package detector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Marker file names recognised by [Detect].
const (
	markerGoMod       = "go.mod"
	markerPackageJSON = "package.json"
	markerIndexHTML   = "index.html"
	markerDockerfile  = "Dockerfile"
)

// Runtime representa a linguagem/estrategia de build detectada.
type Runtime string

const (
	// RuntimeGo marks a Go module project (go.mod) for templated builds.
	RuntimeGo Runtime = "go"
	// RuntimeNodeJS marks a Node.js project (package.json).
	RuntimeNodeJS Runtime = "nodejs"
	// RuntimePython marks a Python project (pyproject.toml, requirements.txt, or Pipfile).
	RuntimePython Runtime = "python"
	// RuntimeStatic marks a static site (index.html at root or under public/).
	RuntimeStatic Runtime = "static"
	// RuntimeDockerfile means the repository supplies its own Dockerfile.
	RuntimeDockerfile Runtime = "dockerfile"
)

// Result holds the detected [Runtime] and file paths that justified the choice.
type Result struct {
	// Runtime is the deployment strategy selected for the project root.
	Runtime Runtime
	// Evidence lists marker files that motivated the decision.
	Evidence []string
}

// Detect varre o diretório raiz e retorna o runtime baseado na presença de arquivos comuns.
//
// Ordem de precedência:
// - Dockerfile.
// - Go (go.mod).
// - Node.js (package.json).
// - Python (pyproject.toml / requirements.txt / Pipfile).
// - Static (index.html).
func Detect(rootDir string) (Result, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return Result{}, errors.New("rootDir nao pode ser vazio")
	}

	info, err := os.Stat(rootDir)
	if err != nil {
		return Result{}, fmt.Errorf("falha ao acessar rootDir: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("rootDir nao e um diretorio: %s", rootDir)
	}

	rootDir, err = filepath.Abs(rootDir)
	if err != nil {
		return Result{}, fmt.Errorf("falha ao resolver path absoluto: %w", err)
	}

	// Dockerfile (se existe no root, respeita o do usuário).
	if exists(filepath.Join(rootDir, markerDockerfile)) {
		return Result{Runtime: RuntimeDockerfile, Evidence: []string{markerDockerfile}}, nil
	}

	// Go.
	if exists(filepath.Join(rootDir, markerGoMod)) {
		return Result{Runtime: RuntimeGo, Evidence: []string{markerGoMod}}, nil
	}

	// Node.js.
	if exists(filepath.Join(rootDir, markerPackageJSON)) {
		return Result{Runtime: RuntimeNodeJS, Evidence: []string{markerPackageJSON}}, nil
	}

	// Python.
	var pyEvidence []string
	for _, name := range []string{"pyproject.toml", "requirements.txt", "Pipfile"} {
		if exists(filepath.Join(rootDir, name)) {
			pyEvidence = append(pyEvidence, name)
		}
	}
	if len(pyEvidence) > 0 {
		return Result{Runtime: RuntimePython, Evidence: pyEvidence}, nil
	}

	// Static site.
	if exists(filepath.Join(rootDir, markerIndexHTML)) {
		return Result{Runtime: RuntimeStatic, Evidence: []string{markerIndexHTML}}, nil
	}
	if exists(filepath.Join(rootDir, "public", markerIndexHTML)) {
		return Result{Runtime: RuntimeStatic, Evidence: []string{filepath.ToSlash(filepath.Join("public", markerIndexHTML))}}, nil
	}

	return Result{}, errors.New("runtime nao detectado: nenhum marcador conhecido encontrado no diretorio raiz")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
