package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"godeploy-platform/internal/integrationtest"
)

func TestAppValidate(t *testing.T) {
	tests := []struct {
		name    string
		app     App
		wantErr bool
	}{
		{
			name: "Valid App",
			app: App{
				Name:         "test-app",
				Image:        "nginx:latest",
				InternalPort: 80,
			},
			wantErr: false,
		},
		{
			name: "Empty Name",
			app: App{
				Name:         "",
				Image:        "nginx:latest",
				InternalPort: 80,
			},
			wantErr: true,
		},
		{
			name: "Empty Image",
			app: App{
				Name:         "test-app",
				Image:        "",
				InternalPort: 80,
			},
			wantErr: true,
		},
		{
			name: "Invalid Port Low",
			app: App{
				Name:         "test-app",
				Image:        "nginx:latest",
				InternalPort: 0,
			},
			wantErr: true,
		},
		{
			name: "Invalid Port High",
			app: App{
				Name:         "test-app",
				Image:        "nginx:latest",
				InternalPort: 65536,
			},
			wantErr: true,
		},
		{
			name: "Negative CPU Limit",
			app: App{
				Name:         "test-app",
				Image:        "nginx:latest",
				InternalPort: 80,
				CPULimit:     -1.0,
			},
			wantErr: true,
		},
		{
			name: "Negative Memory Limit",
			app: App{
				Name:         "test-app",
				Image:        "nginx:latest",
				InternalPort: 80,
				MemoryLimit:  -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.app.validate(); (err != nil) != tt.wantErr {
				t.Errorf("App.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAppHelpers(t *testing.T) {
	app := App{
		MemoryLimit: 512,
		CPULimit:    0.5,
	}

	if got := app.memoryBytes(); got != 512*1024*1024 {
		t.Errorf("memoryBytes() = %v, want %v", got, 512*1024*1024)
	}

	if got := app.nanoCPUs(); got != 500_000_000 {
		t.Errorf("nanoCPUs() = %v, want %v", got, 500_000_000)
	}

	appDefault := App{}
	if got := appDefault.memoryBytes(); got != defaultMemoryLimitMB*1024*1024 {
		t.Errorf("memoryBytes() default = %v, want %v", got, defaultMemoryLimitMB*1024*1024)
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Test App", "test-app"},
		{"  Trim  ", "trim"},
		{"UPPERCASE", "uppercase"},
		{"with_underscore", "with-underscore"},
		{"already-kebab", "already-kebab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeName(tt.input); got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	detach := context.WithoutCancel(ctx)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create docker client: %v", err)
	}
	defer cli.Close()
	integrationtest.SkipIfDockerUnavailable(t, cli)

	networkName := "godeploy-test-net"
	s, err := New(ctx, cli, networkName)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	// Clean up network and containers after test
	defer func() {
		_ = cli.NetworkRemove(detach, networkName) //nolint:errcheck // best-effort test teardown
	}()

	appName := "test-integration-app"
	app := App{
		Name:         appName,
		Image:        "nginx:alpine",
		InternalPort: 80,
	}

	var firstContainerID string

	t.Run("Initial Deploy", func(t *testing.T) {
		res, err := s.Deploy(ctx, app)
		if err != nil {
			t.Fatalf("Initial deployment failed: %v", err)
		}
		if res.NewContainerID == "" {
			t.Fatal("Expected NewContainerID to be set")
		}
		firstContainerID = res.NewContainerID

		// Verify container exists and is running
		inspect, err := cli.ContainerInspect(ctx, res.NewContainerID)
		if err != nil {
			t.Fatalf("Failed to inspect container: %v", err)
		}
		if !inspect.State.Running {
			t.Fatal("Expected container to be running")
		}

		defer func() {
			if t.Failed() {
				_ = cli.ContainerRemove(detach, res.NewContainerID, container.RemoveOptions{Force: true}) //nolint:errcheck // best-effort cleanup on failure
			}
		}()
	})

	t.Run("Update Deploy (Blue-Green)", func(t *testing.T) {
		res, err := s.Deploy(ctx, app)
		if err != nil {
			t.Fatalf("Update deployment failed: %v", err)
		}
		if res.NewContainerID == "" {
			t.Fatal("Expected NewContainerID to be set")
		}
		if res.OldContainerID != firstContainerID {
			t.Errorf("Expected OldContainerID to be %s, got %s", firstContainerID, res.OldContainerID)
		}

		// Verify old container is removed
		_, err = cli.ContainerInspect(ctx, firstContainerID)
		if err == nil {
			t.Error("Expected old container to be removed, but it still exists")
		}

		// Verify new container exists and is running
		inspect, err := cli.ContainerInspect(ctx, res.NewContainerID)
		if err != nil {
			t.Fatalf("Failed to inspect new container: %v", err)
		}
		if !inspect.State.Running {
			t.Fatal("Expected new container to be running")
		}

		// Clean up
		_ = cli.ContainerRemove(detach, res.NewContainerID, container.RemoveOptions{Force: true}) //nolint:errcheck // best-effort cleanup after subtest
	})
}
