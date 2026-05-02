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

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"

	"godeploy-platform/internal/detector"
	"godeploy-platform/internal/platform/iox"
)

// Builder wraps a Docker API client and performs image builds for godeploy.
type Builder struct {
	docker *client.Client
}

// Options configures a single [Builder.Build] invocation.
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

// Result reports the runtime strategy used for the Dockerfile and the final image tag.
type Result struct {
	Runtime detector.Runtime
	Tag     string
}

// New returns a Builder that uses the given non-nil Docker API client.
func New(docker *client.Client) (*Builder, error) {
	if docker == nil {
		return nil, errors.New("docker client nao pode ser nil")
	}
	return &Builder{docker: docker}, nil
}

// Build creates a tar context from opts.RootDir, resolves or generates a Dockerfile,
// and runs a Docker image build. The returned [Result].Tag includes a UTC timestamp
// and a short commit suffix when git metadata is available.
func (b *Builder) Build(ctx context.Context, opts Options) (Result, error) {
	var out Result
	normalizeBuildOptions(&opts)
	if err := validateAndAbsolutizeRoot(&opts); err != nil {
		return out, err
	}

	rt, err := detectBuildRuntime(opts)
	if err != nil {
		return out, err
	}
	out.Runtime = rt

	out.Tag = imageTagForBuild(ctx, opts)

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

	return b.runDockerImageBuild(ctx, out, dfNameInContext, opts.Logs, ctxReader)
}

func normalizeBuildOptions(opts *Options) {
	opts.RootDir = strings.TrimSpace(opts.RootDir)
	opts.ImageName = strings.TrimSpace(opts.ImageName)
	opts.DockerfilePath = strings.TrimSpace(opts.DockerfilePath)
	opts.Commit = strings.TrimSpace(opts.Commit)
}

func validateAndAbsolutizeRoot(opts *Options) error {
	if opts.RootDir == "" {
		return errors.New("RootDir nao pode ser vazio")
	}
	if opts.ImageName == "" {
		return errors.New("ImageName nao pode ser vazio")
	}
	rootInfo, err := os.Stat(opts.RootDir)
	if err != nil {
		return fmt.Errorf("falha ao acessar RootDir: %w", err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("RootDir nao e diretorio: %s", opts.RootDir)
	}
	rootAbs, err := filepath.Abs(opts.RootDir)
	if err != nil {
		return fmt.Errorf("falha ao resolver RootDir absoluto: %w", err)
	}
	opts.RootDir = rootAbs
	return nil
}

func detectBuildRuntime(opts Options) (detector.Runtime, error) {
	if opts.DockerfilePath != "" || fileExists(filepath.Join(opts.RootDir, "Dockerfile")) {
		return detector.RuntimeDockerfile, nil
	}
	detected, err := detector.Detect(opts.RootDir)
	if err != nil {
		return "", err
	}
	return detected.Runtime, nil
}

func imageTagForBuild(ctx context.Context, opts Options) string {
	commit := opts.Commit
	if commit == "" {
		commit = gitShortSHA(ctx, opts.RootDir)
		if commit == "" {
			commit = "nogit"
		}
	}
	ts := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("%s:%s-%s", opts.ImageName, ts, commit)
}

func (b *Builder) runDockerImageBuild(ctx context.Context, out Result, dfName string, logs chan<- string, ctxReader io.Reader) (Result, error) {
	buildResp, err := b.docker.ImageBuild(ctx, ctxReader, build.ImageBuildOptions{
		Tags:       []string{out.Tag},
		Dockerfile: dfName,
		Remove:     true,
	})
	if err != nil {
		return out, fmt.Errorf("falha no ImageBuild: %w", err)
	}
	defer iox.Close(buildResp.Body)

	if logs == nil {
		if _, err := io.Copy(io.Discard, buildResp.Body); err != nil {
			return out, fmt.Errorf("drain image build body: %w", err)
		}
		return out, nil
	}
	if err := streamDockerBuildLogs(buildResp.Body, logs); err != nil {
		return out, err
	}
	return out, nil
}

func (b *Builder) resolveDockerfile(rt detector.Runtime, rootDir, dockerfilePath string) (string, error) {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return "", fmt.Errorf("falha ao abrir rootDir: %w", err)
	}
	defer iox.Close(root)

	if dockerfilePath != "" {
		content, err := readFileUnderRoot(rootDir, root, dockerfilePath)
		if err != nil {
			return "", fmt.Errorf("falha ao ler DockerfilePath: %w", err)
		}
		return ensureTrailingNewline(string(content)), nil
	}

	if fileExists(filepath.Join(rootDir, "Dockerfile")) {
		content, err := readFileUnderRoot(rootDir, root, "Dockerfile")
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
		return nil, closeTarWriterCombiningErr(tw, err)
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, closeTarWriterCombiningErr(tw, fmt.Errorf("falha ao abrir rootDir: %w", err))
	}
	defer iox.Close(root)

	if err := walkDirIntoTar(tw, root, rootDir, dockerfileName); err != nil {
		return nil, closeTarWriterCombiningErr(tw, fmt.Errorf("falha ao montar contexto: %w", err))
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func closeTarWriterCombiningErr(tw *tar.Writer, err error) error {
	if cerr := tw.Close(); cerr != nil {
		return fmt.Errorf("%w: %w", err, cerr)
	}
	return err
}

var defaultTarExcludes = []string{".git", ".idea", ".vscode", "node_modules"}

func tarContextSkipRel(rel, dockerfileName string, excludes []string) bool {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	for _, ex := range excludes {
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return strings.EqualFold(filepath.Base(rel), dockerfileName)
}

func walkDirIntoTar(tw *tar.Writer, root *os.Root, rootDir, dockerfileName string) error {
	shouldSkip := func(rel string) bool {
		return tarContextSkipRel(rel, dockerfileName, defaultTarExcludes)
	}
	return filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		return tarWalkEntry(tw, root, rootDir, path, d, walkErr, shouldSkip)
	})
}

func tarWalkEntry(tw *tar.Writer, root *os.Root, rootDir, path string, d fs.DirEntry, walkErr error, shouldSkip func(string) bool) error {
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
	if d.IsDir() {
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	rel = strings.TrimPrefix(rel, "./")
	f, err := root.Open(rel)
	if err != nil {
		return err
	}
	body, rerr := io.ReadAll(f)
	iox.Close(f)
	if rerr != nil {
		return rerr
	}
	return writeTarFile(tw, rel, body, info.Mode())
}

func readFileUnderRoot(rootDir string, root *os.Root, path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("path vazio")
	}

	// Accept both absolute and relative paths, but require the final resolved
	// path to be under rootDir.
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.Clean(rel)
		if rel == "." || rel == "" {
			return nil, errors.New("path aponta para o root (esperado arquivo)")
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return nil, fmt.Errorf("path fora do RootDir: %s", path)
		}
		path = rel
	}

	path = filepath.ToSlash(filepath.Clean(path))
	path = strings.TrimPrefix(path, "./")

	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer iox.Close(f)
	return io.ReadAll(f)
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
