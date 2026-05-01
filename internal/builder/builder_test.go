package builder

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"godeploy-platform/internal/detector"
)

func TestResolveDockerfile(t *testing.T) {
	b := &Builder{}
	tmpDir := t.TempDir()

	t.Run("Template for Go", func(t *testing.T) {
		got, err := b.resolveDockerfile(detector.RuntimeGo, tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "FROM golang") {
			t.Errorf("expected Go template, got: %s", got)
		}
	})

	t.Run("User Dockerfile in Root", func(t *testing.T) {
		dfPath := filepath.Join(tmpDir, "Dockerfile")
		content := "FROM alpine\nRUN echo hello"
		os.WriteFile(dfPath, []byte(content), 0644)
		defer os.Remove(dfPath)

		got, err := b.resolveDockerfile(detector.RuntimeGo, tmpDir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content+"\n" {
			t.Errorf("expected user Dockerfile content, got: %q", got)
		}
	})

	t.Run("Explicit Dockerfile Path", func(t *testing.T) {
		customDF := filepath.Join(tmpDir, "CustomDockerfile")
		content := "FROM scratch"
		os.WriteFile(customDF, []byte(content), 0644)
		defer os.Remove(customDF)

		got, err := b.resolveDockerfile(detector.RuntimeGo, tmpDir, customDF)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != content+"\n" {
			t.Errorf("expected custom Dockerfile content, got: %q", got)
		}
	})
}

func TestCreateBuildContextTar(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create some files
	os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("secret"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "node_modules", "package.json"), []byte("{}"), 0644)

	dfContent := "FROM nginx:alpine"
	reader, err := createBuildContextTar(tmpDir, "Dockerfile", dfContent)
	if err != nil {
		t.Fatalf("failed to create tar: %v", err)
	}

	tr := tar.NewReader(reader)
	foundFiles := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar: %v", err)
		}
		foundFiles[hdr.Name] = true
	}

	if !foundFiles["Dockerfile"] {
		t.Error("Dockerfile missing in tar")
	}
	if !foundFiles["index.html"] {
		t.Error("index.html missing in tar")
	}
	if foundFiles[".git/config"] {
		t.Error(".git/config should be excluded")
	}
	if foundFiles["node_modules/package.json"] {
		t.Error("node_modules should be excluded")
	}
}

func TestBuildIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()

	b, _ := New(cli)
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("<h1>GoDeploy</h1>"), 0644)

	logs := make(chan string, 100)
	opts := Options{
		RootDir:   tmpDir,
		ImageName: "godeploy-test-static",
		Logs:      logs,
	}

	ctx := context.Background()
	res, err := b.Build(ctx, opts)
	if err != nil {
		// Read logs to see what happened if build failed
		for log := range logs {
			t.Logf("Build log: %s", log)
		}
		t.Fatalf("Build failed: %v", err)
	}

	if res.Runtime != detector.RuntimeStatic {
		t.Errorf("expected runtime static, got %v", res.Runtime)
	}

	// Verify tag format
	if !strings.HasPrefix(res.Tag, "godeploy-test-static:") {
		t.Errorf("unexpected tag format: %s", res.Tag)
	}

	// Wait for logs to close and verify we got some
	logCount := 0
	for range logs {
		logCount++
	}
	if logCount == 0 {
		t.Error("expected some logs, got none")
	}

	// Cleanup image
	_, _ = cli.ImageRemove(ctx, res.Tag, image.RemoveOptions{Force: true})
}

func TestGitShortSHA(t *testing.T) {
	tmpDir := t.TempDir()
	
	// No git repo
	if got := gitShortSHA(context.Background(), tmpDir); got != "" {
		t.Errorf("expected empty string for non-git dir, got %q", got)
	}

	// Initialize git repo
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Logf("git %v failed: %v (skipping git test)", args, err)
			t.Skip()
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	os.WriteFile(filepath.Join(tmpDir, "file"), []byte("data"), 0644)
	run("add", "file")
	run("commit", "-m", "initial commit")

	got := gitShortSHA(context.Background(), tmpDir)
	if len(got) < 7 {
		t.Errorf("expected at least 7 chars for git sha, got %q", got)
	}
}
