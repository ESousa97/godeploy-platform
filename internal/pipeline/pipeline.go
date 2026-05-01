package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"

	"godeploy-platform/internal/builder"
	"godeploy-platform/internal/detector"
	"godeploy-platform/internal/proxy"
	"godeploy-platform/internal/scheduler"
)

type Config struct {
	DB *sql.DB

	Docker *client.Client

	NetworkName string

	// DefaultImagePrefix ex.: "godeploy".
	DefaultImagePrefix string

	// HealthTimeout default 30s.
	HealthTimeout time.Duration
	// HealthPath default "/".
	HealthPath string

	Logger *log.Logger
}

type Runner struct {
	cfg       Config
	store     *proxy.Store
	builder   *builder.Builder
	scheduler *scheduler.Scheduler
}

type RunRequest struct {
	AppName    string
	Domain     string
	CloneURL   string
	Ref        string
	CommitSHA  string
	ImageName  string
	HealthPath string
}

type RunResult struct {
	Runtime        detector.Runtime
	ImageTag       string
	NewContainerID string
	OldContainerID string
	RoutedTarget   string // host:port
}

func New(cfg Config) (*Runner, error) {
	if cfg.DB == nil {
		return nil, errors.New("DB nao pode ser nil")
	}
	if cfg.Docker == nil {
		return nil, errors.New("Docker nao pode ser nil")
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
		cfg.Logger = log.Default()
	}

	store, err := proxy.NewStore(cfg.DB)
	if err != nil {
		return nil, err
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		return nil, err
	}

	b, err := builder.New(cfg.Docker)
	if err != nil {
		return nil, err
	}

	s, err := scheduler.New(context.Background(), cfg.Docker, cfg.NetworkName)
	if err != nil {
		return nil, err
	}

	return &Runner{
		cfg:       cfg,
		store:     store,
		builder:   b,
		scheduler: s,
	}, nil
}

func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	var out RunResult

	req.AppName = strings.TrimSpace(req.AppName)
	req.Domain = strings.TrimSpace(req.Domain)
	req.CloneURL = strings.TrimSpace(req.CloneURL)
	req.Ref = strings.TrimSpace(req.Ref)
	req.CommitSHA = strings.TrimSpace(req.CommitSHA)
	req.ImageName = strings.TrimSpace(req.ImageName)
	req.HealthPath = strings.TrimSpace(req.HealthPath)

	if req.AppName == "" {
		return out, errors.New("AppName nao pode ser vazio")
	}
	if req.Domain == "" {
		req.Domain = fmt.Sprintf("%s.local", normalizeApp(req.AppName))
	}
	if req.CloneURL == "" {
		return out, errors.New("CloneURL nao pode ser vazio")
	}
	if req.ImageName == "" {
		req.ImageName = fmt.Sprintf("%s/%s", r.cfg.DefaultImagePrefix, normalizeApp(req.AppName))
	}

	healthPath := req.HealthPath
	if healthPath == "" {
		healthPath = r.cfg.HealthPath
	}
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}

	// Snapshot da rota atual (pra rollback).
	prevTarget, prevOK, err := r.store.GetRoute(ctx, req.Domain)
	if err != nil {
		return out, err
	}

	tmpDir, err := os.MkdirTemp("", "godeploy-*")
	if err != nil {
		return out, fmt.Errorf("falha ao criar temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := gitClone(ctx, tmpDir, req.CloneURL, req.Ref); err != nil {
		return out, err
	}

	detected, err := detector.Detect(tmpDir)
	if err != nil {
		return out, err
	}
	out.Runtime = detected.Runtime

	internalPort := defaultPortForRuntime(detected.Runtime)

	buildLogs := make(chan string, 256)
	go func() {
		for line := range buildLogs {
			r.cfg.Logger.Printf("build %s: %s", req.AppName, line)
		}
	}()

	bres, err := r.builder.Build(ctx, builder.Options{
		RootDir:    tmpDir,
		ImageName:  req.ImageName,
		Commit:     shortSHA(req.CommitSHA),
		Logs:       buildLogs,
		// DockerfilePath vazio: respeita Dockerfile do repo, ou template do runtime.
	})
	if err != nil {
		return out, err
	}
	out.ImageTag = bres.Tag

	// Deploy do novo container mantendo o antigo vivo para healthcheck + rollback.
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

	newTarget := fmt.Sprintf("127.0.0.1:%s", strings.TrimSpace(deployRes.AssignedHostPort))
	// Em casos raros AssignedHostPort pode vir vazio (inspect falhou); tentamos fallback.
	if strings.TrimSpace(deployRes.AssignedHostPort) == "" {
		fallback, ferr := inferPublishedPort(ctx, r.cfg.Docker, deployRes.NewContainerID, internalPort)
		if ferr != nil {
			_ = r.safeRemoveContainer(context.Background(), deployRes.NewContainerID)
			return out, fmt.Errorf("falha ao resolver porta publicada do novo container: %w", ferr)
		}
		newTarget = fmt.Sprintf("127.0.0.1:%s", fallback)
	}

	// Faz healthcheck direto no novo target antes de trocar a rota.
	if err := waitHTTP200(ctx, "http://"+newTarget+healthPath, r.cfg.HealthTimeout); err != nil {
		_ = r.safeRemoveContainer(context.Background(), deployRes.NewContainerID)
		// Rota não foi alterada ainda, então rollback é manter como estava.
		return out, fmt.Errorf("healthcheck falhou: %w", err)
	}

	// Troca rota do proxy para o novo container.
	if err := r.store.UpsertRoute(ctx, req.Domain, newTarget); err != nil {
		_ = r.safeRemoveContainer(context.Background(), deployRes.NewContainerID)
		// tenta restaurar rota anterior (se existia)
		if prevOK {
			_ = r.store.UpsertRoute(context.Background(), req.Domain, prevTarget)
		} else {
			_ = r.store.DeleteRoute(context.Background(), req.Domain)
		}
		return out, err
	}
	out.RoutedTarget = newTarget

	// Agora sim removemos o antigo (blue-green com readiness).
	if deployRes.OldContainerID != "" {
		timeout := 15
		_ = r.cfg.Docker.ContainerStop(ctx, deployRes.OldContainerID, container.StopOptions{Timeout: &timeout})
		_ = r.safeRemoveContainer(ctx, deployRes.OldContainerID)
	}

	return out, nil
}

func (r *Runner) safeRemoveContainer(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	err := r.cfg.Docker.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func waitHTTP200(ctx context.Context, url string, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-timeoutCtx.Done():
			if lastErr == nil {
				lastErr = timeoutCtx.Err()
			}
			return fmt.Errorf("timeout aguardando 200 OK em %s: %w", url, lastErr)
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
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
		return fmt.Errorf("falha no git clone: %w: %s", err, strings.TrimSpace(string(out)))
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
		// Melhor chute default: 8080 (pode ser sobrescrito no futuro via config/manifest).
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
		return "", errors.New("container sem port bindings")
	}
	published := bindings[nat.Port(key)]
	if len(published) == 0 {
		return "", fmt.Errorf("porta %s nao publicada", key)
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

