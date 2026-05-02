//go:build !windows || force_detector_tests

package detector_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"godeploy-platform/internal/detector"
)

func ExampleDetect() {
	dir, err := os.MkdirTemp("", "godeploy-detector-*")
	if err != nil {
		fmt.Println("error")
		return
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		fmt.Println("error")
		return
	}
	res, err := detector.Detect(dir)
	if err != nil {
		fmt.Println("error")
		return
	}
	fmt.Println(res.Runtime)
	// Output:
	// go
}

func TestDetect_goModMarker(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := detector.Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Runtime != detector.RuntimeGo {
		t.Fatalf("runtime: got %q want %q", res.Runtime, detector.RuntimeGo)
	}
}
