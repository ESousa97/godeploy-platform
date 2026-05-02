// Command govulncheckci runs govulncheck with JSON output and maps exit status so CI
// can stay green when the only findings are known, unreviewed docker/moby module OSVs
// that still list no fixed version for github.com/docker/docker (client API use only).
// Any other finding or a scan/tool failure is propagated unchanged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
)

// daemonModuleOSVsWithoutModuleFix are GO IDs that currently affect the whole
// github.com/docker/docker module in vuln.go.dev while this repo only uses the Engine API client.
// Remove entries when pkg.go.dev lists a fixed module version, or after migrating off the module.
var daemonModuleOSVsWithoutModuleFix = []string{
	"GO-2026-4883",
	"GO-2026-4887",
}

func main() {
	patterns := os.Args[1:]
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	args := append([]string{
		"run", "golang.org/x/vuln/cmd/govulncheck@latest",
		"-format=json",
	}, patterns...)
	//nolint:gosec // argv is fixed govulncheck invocation plus package patterns from this tool only.
	cmd := exec.CommandContext(context.Background(), "go", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()

	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			fmt.Fprintf(os.Stderr, "govulncheckci: %v\n", err)
			os.Exit(1)
		}
		exitCode = ee.ExitCode()
	}

	if exitCode == 0 {
		os.Exit(0)
	}

	osvSeen := collectFindingOSVs(stdout.Bytes())
	if len(osvSeen) == 0 {
		os.Exit(exitCode)
	}

	for id := range osvSeen {
		if !slices.Contains(daemonModuleOSVsWithoutModuleFix, id) {
			fmt.Fprintf(os.Stderr, "govulncheckci: unexpected OSV %q; see govulncheck output above\n", id)
			os.Exit(exitCode)
		}
	}

	fmt.Fprintf(os.Stderr, "govulncheckci: only known docker module OSVs without published module fix (%v); exiting 0. See SECURITY.md.\n", daemonModuleOSVsWithoutModuleFix)
	os.Exit(0)
}

func collectFindingOSVs(jsonStream []byte) map[string]struct{} {
	out := make(map[string]struct{})
	dec := json.NewDecoder(bytes.NewReader(jsonStream))
	for {
		var block struct {
			Finding *struct {
				OSV string `json:"osv"`
			} `json:"finding"`
		}
		if err := dec.Decode(&block); err == io.EOF {
			break
		} else if err != nil {
			return nil
		}
		if block.Finding != nil && block.Finding.OSV != "" {
			out[block.Finding.OSV] = struct{}{}
		}
	}
	return out
}
