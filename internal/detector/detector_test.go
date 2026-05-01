package detector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		wantRT   Runtime
		wantErr  bool
	}{
		{
			name: "Go Project",
			files: map[string]string{
				"go.mod": "module test",
			},
			wantRT: RuntimeGo,
		},
		{
			name: "NodeJS Project",
			files: map[string]string{
				"package.json": "{}",
			},
			wantRT: RuntimeNodeJS,
		},
		{
			name: "Python Project (requirements.txt)",
			files: map[string]string{
				"requirements.txt": "",
			},
			wantRT: RuntimePython,
		},
		{
			name: "Python Project (pyproject.toml)",
			files: map[string]string{
				"pyproject.toml": "",
			},
			wantRT: RuntimePython,
		},
		{
			name: "Static Project (index.html)",
			files: map[string]string{
				"index.html": "<html></html>",
			},
			wantRT: RuntimeStatic,
		},
		{
			name: "Static Project (public/index.html)",
			files: map[string]string{
				"public/index.html": "<html></html>",
			},
			wantRT: RuntimeStatic,
		},
		{
			name: "Dockerfile Priority",
			files: map[string]string{
				"Dockerfile": "FROM alpine",
				"go.mod":     "module test",
			},
			wantRT: RuntimeDockerfile,
		},
		{
			name:    "Empty Directory",
			files:   map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			for path, content := range tt.files {
				fullPath := filepath.Join(tmpDir, path)
				err := os.MkdirAll(filepath.Dir(fullPath), 0755)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}
				err = os.WriteFile(fullPath, []byte(content), 0644)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			got, err := Detect(tmpDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("Detect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Runtime != tt.wantRT {
				t.Errorf("Detect() got = %v, want %v", got.Runtime, tt.wantRT)
			}
		})
	}
}
