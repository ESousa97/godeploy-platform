package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"godeploy-platform/internal/platform/iox"
)

const (
	defaultMemoryLimitMB = int64(256)
	defaultStopTimeout   = 15 * time.Second
	startupTimeout       = 30 * time.Second
	failedLogsTailLines  = "200"
	failedLogsMaxBytes   = 16 * 1024
	labelManagedTrue     = "true"
)

// App describes one deployable workload and its resource envelope.
type App struct {
	// Name is a stable identifier used for container naming and labels.
	Name string
	// Image is the fully qualified image reference to run.
	Image string
	// InternalPort is the container TCP port published to a host port.
	InternalPort int
	// CPULimit caps CPU usage (for example 0.5 for half a core).
	CPULimit float64
	// MemoryLimit is the memory cap in megabytes; zero defaults to 256MB.
	MemoryLimit int64
}

// DeploymentResult captures identifiers and networking after a deploy attempt.
type DeploymentResult struct {
	NewContainerID   string
	NewContainerName string
	OldContainerID   string
	AssignedHostPort string
}

// DeployOptions tweaks rollout behavior for blue-green flows.
type DeployOptions struct {
	// KeepOld, when true, leaves the previous container running for health checks and rollback.
	KeepOld bool
}

// Scheduler coordinates Docker operations for godeploy-managed apps.
type Scheduler struct {
	docker      *client.Client
	networkName string
}

// ResourceConflictError is returned when Docker reports a name or host port collision
// relevant to godeploy scheduling.
type ResourceConflictError struct {
	Resource string
	Value    string
	Details  string
}

// Error implements the [error] interface.
func (e *ResourceConflictError) Error() string {
	return fmt.Sprintf("%s conflict (%s): %s", e.Resource, e.Value, e.Details)
}

// New returns a Scheduler. The networkName network is created on the first [Scheduler.DeployWithOptions]
// call (via [EnsurePaaSNetwork]) so components without access to the Docker socket (e.g. godeployd inside
// a container without /var/run/docker.sock bind-mounted) can still start the HTTP server and pass the health check.
func New(_ context.Context, docker *client.Client, networkName string) (*Scheduler, error) {
	if docker == nil {
		return nil, errors.New("docker client cannot be nil")
	}
	if strings.TrimSpace(networkName) == "" {
		return nil, errors.New("networkName cannot be empty")
	}

	return &Scheduler{
		docker:      docker,
		networkName: networkName,
	}, nil
}

// EnsurePaaSNetwork ensures the dedicated network exists and returns its ID.
func EnsurePaaSNetwork(ctx context.Context, docker *client.Client, networkName string) (string, error) {
	existingID, err := findNetworkByName(ctx, docker, networkName)
	if err != nil {
		return "", err
	}
	if existingID != "" {
		return existingID, nil
	}

	resp, err := docker.NetworkCreate(ctx, networkName, network.CreateOptions{
		Labels: map[string]string{
			"godeploy.managed": labelManagedTrue,
			"godeploy.network": networkName,
		},
	})
	if err != nil {
		// Race between schedulers: another process may have created the network.
		if cerrdefs.IsConflict(err) {
			existingID, lookupErr := findNetworkByName(ctx, docker, networkName)
			if lookupErr == nil && existingID != "" {
				return existingID, nil
			}
		}
		return "", fmt.Errorf("failed to create network %q: %w", networkName, err)
	}

	return resp.ID, nil
}

// Deploy runs a new version with resource limits and removes the old version
// only after the new one is running (basic blue-green).
func (s *Scheduler) Deploy(ctx context.Context, app App) (DeploymentResult, error) {
	return s.DeployWithOptions(ctx, app, DeployOptions{})
}

// DeployWithOptions is [Scheduler.Deploy] with extra options (e.g. keep the old version).
func (s *Scheduler) DeployWithOptions(ctx context.Context, app App, opts DeployOptions) (DeploymentResult, error) {
	var out DeploymentResult

	if err := app.validate(); err != nil {
		return out, err
	}

	if _, err := EnsurePaaSNetwork(ctx, s.docker, s.networkName); err != nil {
		return out, err
	}

	oldContainer, err := s.findCurrentContainer(ctx, app.Name)
	if err != nil {
		return out, err
	}

	appPort, hostBinding, err := s.resolveHostPortBinding(ctx, app, oldContainer)
	if err != nil {
		return out, err
	}

	newContainerName := fmt.Sprintf("%s-%d", normalizeName(app.Name), time.Now().UnixNano())
	containerConfig, hostConfig, networkConfig := s.deployContainerSpecs(app, appPort, hostBinding)

	createResp, err := s.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, newContainerName)
	if err != nil {
		return out, classifyCreateError(err, newContainerName, app.InternalPort)
	}

	out.NewContainerID = createResp.ID
	out.NewContainerName = newContainerName

	detach := context.WithoutCancel(ctx)
	started := false
	defer func() {
		if started {
			return
		}
		_ = s.docker.ContainerRemove(detach, createResp.ID, container.RemoveOptions{Force: true}) //nolint:errcheck // best-effort teardown
	}()

	if err := s.docker.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		return out, fmt.Errorf("failed to start new container %q: %w", newContainerName, err)
	}

	if err := s.waitContainerRunning(ctx, createResp.ID); err != nil {
		// Capture logs before the defer removes the container to preserve the real failure reason.
		tail := s.fetchContainerLogsTail(detach, createResp.ID)
		if tail != "" {
			return out, fmt.Errorf("new container %q did not reach running: %w; logs:\n%s", newContainerName, err, tail)
		}
		return out, fmt.Errorf("new container %q did not reach running: %w", newContainerName, err)
	}
	started = true

	if inspect, ierr := s.docker.ContainerInspect(ctx, createResp.ID); ierr == nil {
		out.AssignedHostPort = firstPublishedPort(inspect.NetworkSettings.Ports, appPort)
	}

	if err := s.finishOldContainer(ctx, oldContainer, opts, &out); err != nil {
		return out, err
	}

	return out, nil
}

func (s *Scheduler) resolveHostPortBinding(ctx context.Context, app App, oldContainer *container.Summary) (nat.Port, nat.PortBinding, error) {
	appPort := nat.Port(fmt.Sprintf("%d/tcp", app.InternalPort))
	hostBinding := nat.PortBinding{HostIP: "0.0.0.0", HostPort: strconv.Itoa(app.InternalPort)}
	// When an old version exists, use a dynamic host port on the new container
	// to avoid downtime during blue-green overlap.
	if oldContainer != nil {
		hostBinding.HostPort = ""
		return appPort, hostBinding, nil
	}
	if conflictErr := s.detectPortConflict(ctx, app.InternalPort); conflictErr != nil {
		return "", nat.PortBinding{}, conflictErr
	}
	return appPort, hostBinding, nil
}

func (s *Scheduler) deployContainerSpecs(app App, appPort nat.Port, hostBinding nat.PortBinding) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	containerConfig := &container.Config{
		Image: app.Image,
		Labels: map[string]string{
			"godeploy.managed":  labelManagedTrue,
			"godeploy.app.name": app.Name,
		},
		ExposedPorts: nat.PortSet{appPort: {}},
	}
	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:   app.memoryBytes(),
			NanoCPUs: app.nanoCPUs(),
		},
		PortBindings: nat.PortMap{appPort: []nat.PortBinding{hostBinding}},
		NetworkMode:  container.NetworkMode(s.networkName),
	}
	if envTruthy(os.Getenv("GODEPLOY_BIND_DOCKER_SOCK")) {
		// Allow godeployd (or other tools) inside the container to access the host Docker engine.
		hostConfig.Binds = append(hostConfig.Binds, "/var/run/docker.sock:/var/run/docker.sock")
	}
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			s.networkName: {
				Aliases: []string{fmt.Sprintf("%s-next", normalizeName(app.Name))},
			},
		},
	}
	return containerConfig, hostConfig, networkConfig
}

func (s *Scheduler) finishOldContainer(ctx context.Context, oldContainer *container.Summary, opts DeployOptions, out *DeploymentResult) error {
	if oldContainer == nil {
		return nil
	}
	out.OldContainerID = oldContainer.ID
	if opts.KeepOld {
		return nil
	}
	timeout := int(defaultStopTimeout.Seconds())
	if stopErr := s.docker.ContainerStop(ctx, oldContainer.ID, container.StopOptions{Timeout: &timeout}); stopErr != nil && !cerrdefs.IsNotFound(stopErr) {
		return fmt.Errorf("new deployment active, but failed to stop old container %q: %w", oldContainer.ID, stopErr)
	}
	if rmErr := s.docker.ContainerRemove(ctx, oldContainer.ID, container.RemoveOptions{Force: true}); rmErr != nil && !cerrdefs.IsNotFound(rmErr) {
		return fmt.Errorf("new deployment active, but failed to remove old container %q: %w", oldContainer.ID, rmErr)
	}
	return nil
}

func (a App) validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("app name cannot be empty")
	}
	if strings.TrimSpace(a.Image) == "" {
		return errors.New("app image cannot be empty")
	}
	if a.InternalPort < 1 || a.InternalPort > 65535 {
		return fmt.Errorf("invalid internal port: %d", a.InternalPort)
	}
	if a.CPULimit < 0 {
		return fmt.Errorf("invalid CPU limit: %.2f", a.CPULimit)
	}
	if a.MemoryLimit < 0 {
		return fmt.Errorf("invalid RAM limit: %dMB", a.MemoryLimit)
	}
	return nil
}

func (a App) memoryBytes() int64 {
	mb := a.MemoryLimit
	if mb == 0 {
		mb = defaultMemoryLimitMB
	}
	return mb * 1024 * 1024
}

func (a App) nanoCPUs() int64 {
	if a.CPULimit <= 0 {
		return 0
	}
	return int64(a.CPULimit * 1_000_000_000)
}

func findNetworkByName(ctx context.Context, docker *client.Client, networkName string) (string, error) {
	args := filters.NewArgs()
	args.Add("name", networkName)

	list, err := docker.NetworkList(ctx, network.ListOptions{Filters: args})
	if err != nil {
		return "", fmt.Errorf("failed to list networks: %w", err)
	}

	for _, n := range list {
		if n.Name == networkName {
			return n.ID, nil
		}
	}

	return "", nil
}

func (s *Scheduler) findCurrentContainer(ctx context.Context, appName string) (*container.Summary, error) {
	args := filters.NewArgs()
	args.Add("label", "godeploy.managed=true")
	args.Add("label", fmt.Sprintf("godeploy.app.name=%s", appName))

	containers, err := s.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current app version %q: %w", appName, err)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Created > containers[j].Created
	})

	for _, c := range containers {
		if strings.EqualFold(c.State, "running") {
			chosen := c
			return &chosen, nil
		}
	}

	fallback := containers[0]
	return &fallback, nil
}

func (s *Scheduler) detectPortConflict(ctx context.Context, hostPort int) error {
	containers, err := s.docker.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return fmt.Errorf("failed to validate port conflicts: %w", err)
	}

	for _, c := range containers {
		for _, p := range c.Ports {
			if int(p.PublicPort) != hostPort {
				continue
			}
			return &ResourceConflictError{
				Resource: "port",
				Value:    strconv.Itoa(hostPort),
				Details:  fmt.Sprintf("port already published by container %s (%v)", c.ID[:12], c.Names),
			}
		}
	}
	return nil
}

// fetchContainerLogsTail tries to read up to [failedLogsTailLines] lines from logs of a
// newly created container that failed to become running. Best-effort: never propagates Docker errors; on
// failure returns an empty string so the caller keeps only the original error.
func (s *Scheduler) fetchContainerLogsTail(ctx context.Context, containerID string) string {
	logsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	rc, err := s.docker.ContainerLogs(logsCtx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       failedLogsTailLines,
	})
	if err != nil {
		return ""
	}
	defer iox.Close(rc)

	var stdout, stderr bytes.Buffer
	// stdcopy demultiplexes Docker's multiplexed stream (stdout/stderr) when the container does not use a TTY.
	if _, err := stdcopy.StdCopy(&stdout, &stderr, rc); err != nil && stdout.Len() == 0 && stderr.Len() == 0 {
		return ""
	}

	combined := strings.TrimSpace(strings.TrimSpace(stderr.String()) + "\n" + strings.TrimSpace(stdout.String()))
	if combined == "" {
		return ""
	}
	if len(combined) > failedLogsMaxBytes {
		combined = "...(truncated)...\n" + combined[len(combined)-failedLogsMaxBytes:]
	}
	return combined
}

func (s *Scheduler) waitContainerRunning(ctx context.Context, containerID string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for running state")
		case <-ticker.C:
			inspect, err := s.docker.ContainerInspect(timeoutCtx, containerID)
			if err != nil {
				return fmt.Errorf("failed to inspect container: %w", err)
			}
			if inspect.State != nil && inspect.State.Running {
				return nil
			}
			if inspect.State != nil && inspect.State.Status == "exited" {
				return fmt.Errorf("container exited early with code %d", inspect.State.ExitCode)
			}
		}
	}
}

func classifyCreateError(err error, containerName string, port int) error {
	msg := strings.ToLower(err.Error())

	if cerrdefs.IsConflict(err) && strings.Contains(msg, "already in use") {
		return &ResourceConflictError{
			Resource: "name",
			Value:    containerName,
			Details:  err.Error(),
		}
	}

	if strings.Contains(msg, "port is already allocated") ||
		strings.Contains(msg, "address already in use") {
		return &ResourceConflictError{
			Resource: "port",
			Value:    strconv.Itoa(port),
			Details:  err.Error(),
		}
	}

	return fmt.Errorf("failed to create container: %w", err)
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "_", "-")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(name)))
}

func firstPublishedPort(bindings nat.PortMap, port nat.Port) string {
	published, ok := bindings[port]
	if !ok || len(published) == 0 {
		return ""
	}
	return published[0].HostPort
}
