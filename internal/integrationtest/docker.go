// Package integrationtest provides small helpers shared by integration tests
// across internal packages (for example Docker Engine reachability checks).
package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

// SkipIfDockerUnavailable skips the test when the Docker Engine API is not reachable.
// NewClientWithOpts often succeeds even when the daemon pipe/socket is absent (e.g. Windows without Docker Desktop).
func SkipIfDockerUnavailable(t *testing.T, cli *client.Client) {
	t.Helper()
	if cli == nil {
		t.Fatal("nil docker client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
}
