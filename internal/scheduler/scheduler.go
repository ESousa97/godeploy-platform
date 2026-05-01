package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
)

const (
	defaultMemoryLimitMB = int64(256)
	defaultStopTimeout   = 15 * time.Second
	startupTimeout       = 30 * time.Second
)

// App descreve uma aplicacao gerenciada pela PaaS.
type App struct {
	Name         string
	Image        string
	InternalPort int
	CPULimit     float64 // Ex.: 0.5 = meio core.
	MemoryLimit  int64   // Em MB. Se zero, usa 256MB.
}

type DeploymentResult struct {
	NewContainerID   string
	NewContainerName string
	OldContainerID   string
	AssignedHostPort string
}

type Scheduler struct {
	docker      *client.Client
	networkName string
}

// ResourceConflictError representa conflitos comuns de deploy.
type ResourceConflictError struct {
	Resource string
	Value    string
	Details  string
}

func (e *ResourceConflictError) Error() string {
	return fmt.Sprintf("conflito de %s (%s): %s", e.Resource, e.Value, e.Details)
}

func New(ctx context.Context, docker *client.Client, networkName string) (*Scheduler, error) {
	if docker == nil {
		return nil, errors.New("docker client nao pode ser nil")
	}
	if strings.TrimSpace(networkName) == "" {
		return nil, errors.New("networkName nao pode ser vazio")
	}

	if _, err := EnsurePaaSNetwork(ctx, docker, networkName); err != nil {
		return nil, err
	}

	return &Scheduler{
		docker:      docker,
		networkName: networkName,
	}, nil
}

// EnsurePaaSNetwork garante que a rede dedicada existe e retorna seu ID.
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
			"godeploy.managed": "true",
			"godeploy.network": networkName,
		},
	})
	if err != nil {
		// Corrida entre schedulers: outro processo pode ter criado a rede.
		if errdefs.IsConflict(err) {
			existingID, lookupErr := findNetworkByName(ctx, docker, networkName)
			if lookupErr == nil && existingID != "" {
				return existingID, nil
			}
		}
		return "", fmt.Errorf("falha ao criar network %q: %w", networkName, err)
	}

	return resp.ID, nil
}

// Deploy sobe uma nova versao com recursos limitados e remove a versao antiga
// somente apos a nova estar em execucao (blue-green basico).
func (s *Scheduler) Deploy(ctx context.Context, app App) (DeploymentResult, error) {
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

	appPort := nat.Port(fmt.Sprintf("%d/tcp", app.InternalPort))
	hostBinding := nat.PortBinding{HostIP: "0.0.0.0", HostPort: strconv.Itoa(app.InternalPort)}

	// Quando existe versao antiga, usamos porta dinamica no novo container para
	// evitar downtime durante o overlap do blue-green.
	if oldContainer != nil {
		hostBinding.HostPort = ""
	} else {
		if conflictErr := s.detectPortConflict(ctx, app.InternalPort); conflictErr != nil {
			return out, conflictErr
		}
	}

	newContainerName := fmt.Sprintf("%s-%d", normalizeName(app.Name), time.Now().UnixNano())
	containerConfig := &container.Config{
		Image: app.Image,
		Labels: map[string]string{
			"godeploy.managed":  "true",
			"godeploy.app.name": app.Name,
		},
		ExposedPorts: nat.PortSet{
			appPort: {},
		},
	}

	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:   app.memoryBytes(),
			NanoCPUs: app.nanoCPUs(),
		},
		PortBindings: nat.PortMap{
			appPort: []nat.PortBinding{hostBinding},
		},
		NetworkMode: container.NetworkMode(s.networkName),
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			s.networkName: {
				Aliases: []string{
					fmt.Sprintf("%s-next", normalizeName(app.Name)),
				},
			},
		},
	}

	createResp, err := s.docker.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, newContainerName)
	if err != nil {
		return out, classifyCreateError(err, newContainerName, app.InternalPort)
	}

	out.NewContainerID = createResp.ID
	out.NewContainerName = newContainerName

	started := false
	defer func() {
		if started {
			return
		}
		_ = s.docker.ContainerRemove(context.Background(), createResp.ID, container.RemoveOptions{Force: true})
	}()

	if err := s.docker.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		return out, fmt.Errorf("falha ao iniciar novo container %q: %w", newContainerName, err)
	}

	if err := s.waitContainerRunning(ctx, createResp.ID); err != nil {
		return out, fmt.Errorf("novo container %q nao ficou running: %w", newContainerName, err)
	}
	started = true

	inspect, err := s.docker.ContainerInspect(ctx, createResp.ID)
	if err == nil {
		out.AssignedHostPort = firstPublishedPort(inspect.NetworkSettings.Ports, appPort)
	}

	if oldContainer != nil {
		out.OldContainerID = oldContainer.ID

		timeout := int(defaultStopTimeout.Seconds())
		if stopErr := s.docker.ContainerStop(ctx, oldContainer.ID, container.StopOptions{Timeout: &timeout}); stopErr != nil && !errdefs.IsNotFound(stopErr) {
			return out, fmt.Errorf("novo container ativo, mas falha ao parar antigo %q: %w", oldContainer.ID, stopErr)
		}

		if rmErr := s.docker.ContainerRemove(ctx, oldContainer.ID, container.RemoveOptions{Force: true}); rmErr != nil && !errdefs.IsNotFound(rmErr) {
			return out, fmt.Errorf("novo container ativo, mas falha ao remover antigo %q: %w", oldContainer.ID, rmErr)
		}
	}

	return out, nil
}

func (a App) validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("nome da app nao pode ser vazio")
	}
	if strings.TrimSpace(a.Image) == "" {
		return errors.New("imagem da app nao pode ser vazia")
	}
	if a.InternalPort < 1 || a.InternalPort > 65535 {
		return fmt.Errorf("porta interna invalida: %d", a.InternalPort)
	}
	if a.CPULimit < 0 {
		return fmt.Errorf("limite de CPU invalido: %.2f", a.CPULimit)
	}
	if a.MemoryLimit < 0 {
		return fmt.Errorf("limite de RAM invalido: %dMB", a.MemoryLimit)
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
		return "", fmt.Errorf("falha ao listar networks: %w", err)
	}

	for _, n := range list {
		if n.Name == networkName {
			return n.ID, nil
		}
	}

	return "", nil
}

func (s *Scheduler) findCurrentContainer(ctx context.Context, appName string) (*types.Container, error) {
	args := filters.NewArgs()
	args.Add("label", "godeploy.managed=true")
	args.Add("label", fmt.Sprintf("godeploy.app.name=%s", appName))

	containers, err := s.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao buscar versao atual da app %q: %w", appName, err)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Created > containers[j].Created
	})

	for _, c := range containers {
		if strings.EqualFold(c.State, "running") {
			copy := c
			return &copy, nil
		}
	}

	copy := containers[0]
	return &copy, nil
}

func (s *Scheduler) detectPortConflict(ctx context.Context, hostPort int) error {
	containers, err := s.docker.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return fmt.Errorf("falha ao validar conflitos de porta: %w", err)
	}

	for _, c := range containers {
		for _, p := range c.Ports {
			if int(p.PublicPort) != hostPort {
				continue
			}
			return &ResourceConflictError{
				Resource: "porta",
				Value:    strconv.Itoa(hostPort),
				Details:  fmt.Sprintf("porta ja publicada pelo container %s (%v)", c.ID[:12], c.Names),
			}
		}
	}
	return nil
}

func (s *Scheduler) waitContainerRunning(ctx context.Context, containerID string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout aguardando estado running")
		case <-ticker.C:
			inspect, err := s.docker.ContainerInspect(timeoutCtx, containerID)
			if err != nil {
				return fmt.Errorf("falha ao inspecionar container: %w", err)
			}
			if inspect.State != nil && inspect.State.Running {
				return nil
			}
			if inspect.State != nil && inspect.State.Status == "exited" {
				return fmt.Errorf("container finalizou prematuramente com codigo %d", inspect.State.ExitCode)
			}
		}
	}
}

func classifyCreateError(err error, containerName string, port int) error {
	msg := strings.ToLower(err.Error())

	if errdefs.IsConflict(err) && strings.Contains(msg, "already in use") {
		return &ResourceConflictError{
			Resource: "nome",
			Value:    containerName,
			Details:  err.Error(),
		}
	}

	if strings.Contains(msg, "port is already allocated") ||
		strings.Contains(msg, "address already in use") {
		return &ResourceConflictError{
			Resource: "porta",
			Value:    strconv.Itoa(port),
			Details:  err.Error(),
		}
	}

	return fmt.Errorf("falha ao criar container: %w", err)
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
