package builder

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"

	"godeploy-platform/internal/detector"
)

type Builder struct {
	docker *client.Client
}

type Options struct {
	// RootDir é o diretório raiz do projeto para montar o contexto de build.
	RootDir string
	// ImageName é o nome do repositório da imagem (sem tag), ex.: "godeploy/myapp".
	ImageName string

	// DockerfilePath opcional: se fornecido, usa este arquivo como Dockerfile.
	// Caso vazio, usa um template embutido baseado no runtime detectado.
	DockerfilePath string

	// Commit opcional: se vazio, tenta obter via git.
	Commit string

	// Logs é um canal para streaming de logs do build.
	// Se não for nil, o Builder enviará linhas de log e fechará o canal ao final.
	Logs chan<- string
}

type Result struct {
	Runtime detector.Runtime
	Tag     string
}

func New(docker *client.Client) (*Builder, error) {
	if docker == nil {
		return nil, errors.New("docker client nao pode ser nil")
	}
	return &Builder{docker: docker}, nil
}

func (b *Builder) Build(ctx context.Context, opts Options) (Result, error) {
	var out Result

	opts.RootDir = strings.TrimSpace(opts.RootDir)
	opts.ImageName = strings.TrimSpace(opts.ImageName)
	opts.DockerfilePath = strings.TrimSpace(opts.DockerfilePath)
	opts.Commit = strings.TrimSpace(opts.Commit)

	if opts.RootDir == "" {
		return out, errors.New("RootDir nao pode ser vazio")
	}
	if opts.ImageName == "" {
		return out, errors.New("ImageName nao pode ser vazio")
	}

	rootInfo, err := os.Stat(opts.RootDir)
	if err != nil {
		return out, fmt.Errorf("falha ao acessar RootDir: %w", err)
	}
	if !rootInfo.IsDir() {
		return out, fmt.Errorf("RootDir nao e diretorio: %s", opts.RootDir)
	}

	rootAbs, err := filepath.Abs(opts.RootDir)
	if err != nil {
		return out, fmt.Errorf("falha ao resolver RootDir absoluto: %w", err)
	}
	opts.RootDir = rootAbs

	var rt detector.Runtime
	if opts.DockerfilePath != "" || fileExists(filepath.Join(opts.RootDir, "Dockerfile")) {
		rt = detector.RuntimeDockerfile
	} else {
		detected, derr := detector.Detect(opts.RootDir)
		if derr != nil {
			return out, derr
		}
		rt = detected.Runtime
	}
	out.Runtime = rt

	commit := opts.Commit
	if commit == "" {
		commit = gitShortSHA(ctx, opts.RootDir)
		if commit == "" {
			commit = "nogit"
		}
	}

	ts := time.Now().UTC().Format("20060102-150405")
	out.Tag = fmt.Sprintf("%s:%s-%s", opts.ImageName, ts, commit)

	logs := opts.Logs
	if logs != nil {
		defer close(logs)
	}

	dfNameInContext := "Dockerfile"
	dfContent, err := b.resolveDockerfile(rt, opts.RootDir, opts.DockerfilePath)
	if err != nil {
		return out, err
	}

	if logs != nil {
		logs <- fmt.Sprintf("runtime=%s tag=%s", rt, out.Tag)
	}

	ctxReader, err := createBuildContextTar(opts.RootDir, dfNameInContext, dfContent)
	if err != nil {
		return out, err
	}

	buildResp, err := b.docker.ImageBuild(ctx, ctxReader, types.ImageBuildOptions{
		Tags:       []string{out.Tag},
		Dockerfile: dfNameInContext,
		Remove:     true,
	})
	if err != nil {
		return out, fmt.Errorf("falha no ImageBuild: %w", err)
	}
	defer buildResp.Body.Close()

	if logs == nil {
		_, _ = io.Copy(io.Discard, buildResp.Body)
		return out, nil
	}

	if err := streamDockerBuildLogs(buildResp.Body, logs); err != nil {
		return out, err
	}

	return out, nil
}

func (b *Builder) resolveDockerfile(rt detector.Runtime, rootDir, dockerfilePath string) (string, error) {
	if dockerfilePath != "" {
		content, err := os.ReadFile(dockerfilePath)
		if err != nil {
			return "", fmt.Errorf("falha ao ler DockerfilePath: %w", err)
		}
		return ensureTrailingNewline(string(content)), nil
	}

	userDockerfile := filepath.Join(rootDir, "Dockerfile")
	if fileExists(userDockerfile) {
		content, err := os.ReadFile(userDockerfile)
		if err != nil {
			return "", fmt.Errorf("falha ao ler Dockerfile do root: %w", err)
		}
		return ensureTrailingNewline(string(content)), nil
	}

	tpl, err := dockerfileTemplate(rt)
	if err != nil {
		return "", err
	}
	return tpl, nil
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gitShortSHA(ctx context.Context, rootDir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD")
	cmd.Dir = rootDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func streamDockerBuildLogs(r io.Reader, logs chan<- string) error {
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		var msg jsonmessage.JSONMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("falha ao decodificar log do build: %w", err)
		}

		if msg.Error != nil {
			text := msg.Error.Message
			if strings.TrimSpace(text) == "" {
				text = msg.Error.Error()
			}
			logs <- strings.TrimRight(text, "\n")
			return fmt.Errorf("build falhou: %s", strings.TrimSpace(text))
		}

		if s := strings.TrimSpace(msg.Stream); s != "" {
			for _, line := range splitLinesPreserveMeaning(s) {
				logs <- line
			}
		}
		if s := strings.TrimSpace(msg.Status); s != "" {
			logs <- s
		}
	}
}

func splitLinesPreserveMeaning(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func createBuildContextTar(rootDir, dockerfileName, dockerfileContents string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	// Injeta Dockerfile (template ou fornecido).
	if err := writeTarFile(tw, dockerfileName, []byte(dockerfileContents), 0o644); err != nil {
		_ = tw.Close()
		return nil, err
	}

	excludes := []string{
		".git",
		".idea",
		".vscode",
		"node_modules",
	}

	shouldSkip := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		for _, ex := range excludes {
			if rel == ex || strings.HasPrefix(rel, ex+"/") {
				return true
			}
		}
		// Evita sobrescrever o Dockerfile injetado.
		if strings.EqualFold(filepath.Base(rel), dockerfileName) {
			return true
		}
		return false
	}

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Mantém simples/portável: ignora symlinks no contexto.
			return nil
		}
		if d.IsDir() {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		return writeTarFile(tw, rel, b, info.Mode())
	})
	if err != nil {
		_ = tw.Close()
		return nil, fmt.Errorf("falha ao montar contexto: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func writeTarFile(tw *tar.Writer, name string, content []byte, mode fs.FileMode) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    int64(mode.Perm()),
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := io.Copy(tw, bytes.NewReader(content))
	return err
}

