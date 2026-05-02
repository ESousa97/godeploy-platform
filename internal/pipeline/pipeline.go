package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"godeploy-platform/internal/builder"
	"godeploy-platform/internal/detector"
	"godeploy-platform/internal/proxy"
	"godeploy-platform/internal/scheduler"
)

// Config wires database, Docker, network defaults, health probing, and logging for [New].
type Config struct {
	// DB is the shared SQLite handle used by the proxy route store.
	DB *sql.DB

	// Docker is the Docker Engine API client.
	Docker *client.Client

	// NetworkName is the Docker bridge network for managed containers (default "godeploy").
	NetworkName string

	// DefaultImagePrefix is the repository prefix for built images (for example "godeploy").
	DefaultImagePrefix string

	// HealthTimeout bounds HTTP health checks against new containers (default 30s).
	HealthTimeout time.Duration
	// HealthPath is the HTTP path probed on new containers before switching traffic (default "/").
	HealthPath string

	// Logger receives structured pipeline events; if nil, slog.Default is used.
	Logger *slog.Logger
}

// Runner executes end-to-end deploy workflows (clone, build, schedule, route).
type Runner struct {
	cfg       Config
	store     *proxy.Store
	builder   *builder.Builder
	scheduler *scheduler.Scheduler
}

// RunRequest describes one deployment triggered by a webhook or operator.
type RunRequest struct {
	AppName    string
	Domain     string
	CloneURL   string
	Ref        string
	CommitSHA  string
	ImageName  string
	HealthPath string
}

// RunResult summarizes a successful [Runner.Run]: image tag, container IDs, and route target.
type RunResult struct {
	Runtime        detector.Runtime
	ImageTag       string
	NewContainerID string
	OldContainerID string
	RoutedTarget   string // host:port
}

// New constructs a [Runner] from cfg, ensuring proxy schema. The PaaS Docker network
// is created on the first deploy ([Runner.Run] -> scheduler), so godeployd can start
// without a Docker socket until a webhook runs (e.g. distroless self-deploy).
func New(cfg Config) (*Runner, error) {
	if cfg.DB == nil {
		return nil, errors.New("DB cannot be nil")
	}
	if cfg.Docker == nil {
		return nil, errors.New("docker cannot be nil")
	}
	if strings.TrimSpace(cfg.NetworkName) == "" {
		cfg.NetworkName = "godeploy"
	}
	if strings.TrimSpace(cfg.DefaultImagePrefix) == "" {
		cfg.DefaultImagePrefix = "godeploy"
	}
	if cfg.HealthTimeout <= 0 {
		cfg.HealthTimeout = 30 * time.Second
	}
	if strings.TrimSpace(cfg.HealthPath) == "" {
		cfg.HealthPath = "/"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	store, err := proxy.NewStore(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("route store: %w", err)
	}
	if schemaErr := store.EnsureSchema(context.Background()); schemaErr != nil {
		return nil, fmt.Errorf("ensure proxy schema: %w", schemaErr)
	}

	b, err := builder.New(cfg.Docker)
	if err != nil {
		return nil, fmt.Errorf("builder: %w", err)
	}

	s, err := scheduler.New(context.Background(), cfg.Docker, cfg.NetworkName)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}

	return &Runner{
		cfg:       cfg,
		store:     store,
		builder:   b,
		scheduler: s,
	}, nil
}

// Run clones req.CloneURL, builds an image, deploys a new container with health checks,
// updates the SQLite route for req.Domain, and removes the previous container on success.
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	var out RunResult
	trimRunRequest(&req)
	if err := r.validateRunRequest(&req); err != nil {
		return out, err
	}
	healthPath := resolvedHealthPath(req.HealthPath, r.cfg.HealthPath)

	prevTarget, prevOK, err := r.store.GetRoute(ctx, req.Domain)
	if err != nil {
		return out, err
	}

	cleanupCtx := context.WithoutCancel(ctx)

	tmpDir, err := os.MkdirTemp("", "godeploy-*")
	if err != nil {
		return out, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			r.cfg.Logger.Warn("tempdir cleanup", slog.Any("err", rmErr))
		}
	}()

	r.warnIfLocalRepoDirty(ctx, req)

	if cloneErr := gitClone(ctx, tmpDir, req.CloneURL, req.Ref); cloneErr != nil {
		return out, cloneErr
	}

	detected, err := detector.Detect(tmpDir)
	if err != nil {
		return out, err
	}
	out.Runtime = detected.Runtime
	internalPort := defaultPortForRuntime(detected.Runtime)

	bres, err := r.runBuildWithLogDrain(ctx, tmpDir, req)
	if err != nil {
		return out, err
	}
	out.ImageTag = bres.Tag

	deployRes, err := r.scheduler.DeployWithOptions(ctx, scheduler.App{
		Name:         req.AppName,
		Image:        bres.Tag,
		InternalPort: internalPort,
	}, scheduler.DeployOptions{KeepOld: true})
	if err != nil {
		return out, err
	}
	out.NewContainerID = deployRes.NewContainerID
	out.OldContainerID = deployRes.OldContainerID

	newTarget, err := r.resolvePublishedTarget(ctx, deployRes, internalPort, cleanupCtx)
	if err != nil {
		return out, err
	}

	if err := waitHTTP200(ctx, "http://"+newTarget+healthPath, r.cfg.HealthTimeout); err != nil {
		r.warnSafeRemove(cleanupCtx, deployRes.NewContainerID, "cleanup new container")
		return out, fmt.Errorf("healthcheck failed: %w", err)
	}

	if err := r.store.UpsertRoute(ctx, req.Domain, newTarget); err != nil {
		r.warnSafeRemove(cleanupCtx, deployRes.NewContainerID, "cleanup new container")
		r.rollbackRoute(cleanupCtx, req.Domain, prevTarget, prevOK)
		return out, err
	}
	out.RoutedTarget = newTarget

	r.stopAndRemoveOldContainer(ctx, deployRes.OldContainerID)
	return out, nil
}

func trimRunRequest(req *RunRequest) {
	req.AppName = strings.TrimSpace(req.AppName)
	req.Domain = strings.TrimSpace(req.Domain)
	req.CloneURL = strings.TrimSpace(req.CloneURL)
	req.Ref = strings.TrimSpace(req.Ref)
	req.CommitSHA = strings.TrimSpace(req.CommitSHA)
	req.ImageName = strings.TrimSpace(req.ImageName)
	req.HealthPath = strings.TrimSpace(req.HealthPath)
}

func (r *Runner) validateRunRequest(req *RunRequest) error {
	if req.AppName == "" {
		return errors.New("AppName cannot be empty")
	}
	if req.Domain == "" {
		req.Domain = fmt.Sprintf("%s.local", normalizeApp(req.AppName))
	}
	if req.CloneURL == "" {
		return errors.New("CloneURL cannot be empty")
	}
	if req.ImageName == "" {
		req.ImageName = fmt.Sprintf("%s/%s", r.cfg.DefaultImagePrefix, normalizeApp(req.AppName))
	}
	return nil
}

func resolvedHealthPath(reqPath, cfgDefault string) string {
	healthPath := reqPath
	if healthPath == "" {
		healthPath = cfgDefault
	}
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}
	return healthPath
}

func (r *Runner) runBuildWithLogDrain(ctx context.Context, tmpDir string, req RunRequest) (builder.Result, error) {
	buildLogs := make(chan string, 256)
	go func() {
		for line := range buildLogs {
			r.cfg.Logger.Info("build", slog.String("app", req.AppName), slog.String("line", line))
		}
	}()
	return r.builder.Build(ctx, builder.Options{
		RootDir:   tmpDir,
		ImageName: req.ImageName,
		Commit:    shortSHA(req.CommitSHA),
		Logs:      buildLogs,
	})
}

func (r *Runner) resolvePublishedTarget(ctx context.Context, deployRes scheduler.DeploymentResult, internalPort int, cleanupCtx context.Context) (string, error) {
	newTarget := fmt.Sprintf("127.0.0.1:%s", strings.TrimSpace(deployRes.AssignedHostPort))
	if strings.TrimSpace(deployRes.AssignedHostPort) != "" {
		return newTarget, nil
	}
	fallback, ferr := inferPublishedPort(ctx, r.cfg.Docker, deployRes.NewContainerID, internalPort)
	if ferr != nil {
		r.warnSafeRemove(cleanupCtx, deployRes.NewContainerID, "cleanup new container")
		return "", fmt.Errorf("failed to resolve published port of new container: %w", ferr)
	}
	return fmt.Sprintf("127.0.0.1:%s", fallback), nil
}

func (r *Runner) warnSafeRemove(ctx context.Context, id, logKey string) {
	if rmErr := r.safeRemoveContainer(ctx, id); rmErr != nil {
		r.cfg.Logger.Warn(logKey, slog.Any("err", rmErr))
	}
}

func (r *Runner) rollbackRoute(cleanupCtx context.Context, domain, prevTarget string, prevOK bool) {
	if prevOK {
		if upErr := r.store.UpsertRoute(cleanupCtx, domain, prevTarget); upErr != nil {
			r.cfg.Logger.Warn("rollback route", slog.Any("err", upErr))
		}
		return
	}
	if delErr := r.store.DeleteRoute(cleanupCtx, domain); delErr != nil {
		r.cfg.Logger.Warn("rollback delete route", slog.Any("err", delErr))
	}
}

func (r *Runner) stopAndRemoveOldContainer(ctx context.Context, oldID string) {
	if oldID == "" {
		return
	}
	timeout := 15
	if stopErr := r.cfg.Docker.ContainerStop(ctx, oldID, container.StopOptions{Timeout: &timeout}); stopErr != nil {
		r.cfg.Logger.Warn("stop old container", slog.Any("err", stopErr))
	}
	if rmErr := r.safeRemoveContainer(ctx, oldID); rmErr != nil {
		r.cfg.Logger.Warn("remove old container", slog.Any("err", rmErr))
	}
}

func (r *Runner) safeRemoveContainer(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	err := r.cfg.Docker.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func waitHTTP200(ctx context.Context, target string, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConnsPerHost:   4,
		},
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-timeoutCtx.Done():
			if lastErr == nil {
				lastErr = timeoutCtx.Err()
			}
			return fmt.Errorf("timeout waiting for 200 OK on %s: %w", target, lastErr)
		case <-ticker.C:
			req, reqErr := http.NewRequestWithContext(timeoutCtx, http.MethodGet, target, http.NoBody)
			if reqErr != nil {
				lastErr = reqErr
				continue
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			_, copyErr := io.Copy(io.Discard, resp.Body)
			closeErr := resp.Body.Close()
			if copyErr != nil {
				lastErr = copyErr
				if closeErr != nil {
					lastErr = fmt.Errorf("%w: %w", copyErr, closeErr)
				}
				continue
			}
			if closeErr != nil {
				lastErr = closeErr
				continue
			}
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		}
	}
}

func gitClone(ctx context.Context, dstDir, cloneURL, ref string) error {
	args := []string{"clone", "--depth", "1"}
	if strings.TrimSpace(ref) != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", cloneURL, dstDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultPortForRuntime(rt detector.Runtime) int {
	switch rt {
	case detector.RuntimeNodeJS:
		return 3000
	case detector.RuntimePython:
		return 8000
	case detector.RuntimeStatic:
		return 80
	case detector.RuntimeGo:
		return 8080
	case detector.RuntimeDockerfile:
		// Sensible default: 8080 (may be overridden later via config/manifest).
		return 8080
	default:
		return 8080
	}
}

func inferPublishedPort(ctx context.Context, docker *client.Client, containerID string, internalPort int) (string, error) {
	inspect, err := docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%d/tcp", internalPort)
	bindings := inspect.NetworkSettings.Ports
	if bindings == nil {
		return "", errors.New("container has no port bindings")
	}
	published := bindings[nat.Port(key)]
	if len(published) == 0 {
		return "", fmt.Errorf("port %s not published", key)
	}
	return strings.TrimSpace(published[0].HostPort), nil
}

func shortSHA(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func normalizeApp(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.NewReplacer(" ", "-", "_", "-").Replace(name)
	name = strings.Trim(name, "-")
	if name == "" {
		return "app"
	}
	return name
}

// warnIfLocalRepoDirty logs when the pipeline clones a local repository
// (file://) whose working tree has uncommitted changes. git clone always
// copies the remote HEAD, so uncommitted changes never reach the built image —
// a common source of "why did the container run the wrong code?" when testing self-deploy.
func (r *Runner) warnIfLocalRepoDirty(ctx context.Context, req RunRequest) {
	root, ok := localFileRepoRoot(req.CloneURL)
	if !ok {
		return
	}
	dirty, files, err := gitWorkingTreeDirty(ctx, root)
	if err != nil || !dirty {
		return
	}
	r.cfg.Logger.Warn(
		"local repository working tree has uncommitted changes; build will use HEAD only",
		slog.String("app", req.AppName),
		slog.String("repo", root),
		slog.String("ref", req.Ref),
		slog.Int("dirty_files", files),
	)
}

// localFileRepoRoot returns the local directory pointed to by a `file://...`
// cloneURL. Returns ok=false for other schemes or invalid paths.
func localFileRepoRoot(cloneURL string) (string, bool) {
	cloneURL = strings.TrimSpace(cloneURL)
	if cloneURL == "" {
		return "", false
	}
	u, err := url.Parse(cloneURL)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	p := u.Path
	// On Windows file:///D:/foo, url.Path is "/D:/foo"; normalize to "D:/foo".
	if runtime.GOOS == "windows" && len(p) > 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	if p == "" {
		return "", false
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return p, true
}

// gitWorkingTreeDirty runs `git status --porcelain` in root and reports whether
// there are modified/staged/untracked files, with a line count.
func gitWorkingTreeDirty(ctx context.Context, root string) (dirty bool, files int, err error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = root
	out, runErr := cmd.Output()
	if runErr != nil {
		return false, 0, runErr
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return false, 0, nil
	}
	return true, strings.Count(trimmed, "\n") + 1, nil
}
