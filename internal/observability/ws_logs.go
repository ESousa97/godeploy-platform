package observability

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gorilla/websocket"

	"godeploy-platform/internal/middleware"
	"godeploy-platform/internal/platform/iox"
)

// LogMessage is one line streamed to WebSocket clients.
type LogMessage struct {
	Stream string `json:"stream"` // stdout|stderr|meta
	Line   string `json:"line"`
}

// LogsStreamer upgrades HTTP to WebSocket and tails container logs via Docker.
type LogsStreamer struct {
	docker         *client.Client
	allowedOrigins []string
}

// NewLogsStreamer creates a log WebSocket handler. allowedOrigins lists extra
// browser Origins permitted in addition to same-host requests (see middleware.WSCheckOrigin).
func NewLogsStreamer(docker *client.Client, allowedOrigins []string) *LogsStreamer {
	return &LogsStreamer{docker: docker, allowedOrigins: allowedOrigins}
}

// Handler returns an [http.HandlerFunc] for GET /api/ws/logs?container=...
func (s *LogsStreamer) Handler() http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     middleware.WSCheckOrigin(s.allowedOrigins),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		containerRef := strings.TrimSpace(r.URL.Query().Get("container"))
		if containerRef == "" {
			http.Error(w, "parametro 'container' obrigatorio", http.StatusBadRequest)
			return
		}

		// Fail-fast se container não existe.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		inspect, err := s.docker.ContainerInspect(ctx, containerRef)
		cancel()
		if err != nil {
			http.Error(w, "container nao encontrado", http.StatusNotFound)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer iox.Close(conn)

		// Cancelamento quando socket fechar.
		streamCtx, stop := context.WithCancel(r.Context())
		defer stop()

		// Leitor de controle (pings/close) para detectar desconexão.
		conn.SetReadLimit(1024)
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return
		}
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck // pong: best-effort
			return nil
		})
		go func() {
			defer stop()
			for {
				if _, _, readErr := conn.ReadMessage(); readErr != nil {
					return
				}
			}
		}()

		sendMu := &sync.Mutex{}
		send := func(msg LogMessage) error {
			sendMu.Lock()
			defer sendMu.Unlock()
			return conn.WriteJSON(msg)
		}

		if err := send(LogMessage{Stream: "meta", Line: "connected"}); err != nil {
			return
		}

		rc, err := s.docker.ContainerLogs(streamCtx, containerRef, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Timestamps: false,
			Tail:       "100",
		})
		if err != nil {
			_ = send(LogMessage{Stream: "meta", Line: "erro ao abrir logs"}) //nolint:errcheck // client already misconfigured
			return
		}
		defer iox.Close(rc)

		// Se o container foi criado com TTY, o stream não é multiplexado.
		if inspect.Config != nil && inspect.Config.Tty {
			sc := bufio.NewScanner(rc)
			buf := make([]byte, 0, 64*1024)
			sc.Buffer(buf, 1024*1024)
			for sc.Scan() {
				select {
				case <-streamCtx.Done():
					return
				default:
					if err := send(LogMessage{Stream: "stdout", Line: sc.Text()}); err != nil {
						return
					}
				}
			}
			return
		}

		// Demultiplex stdout/stderr (containers sem TTY).
		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		demuxErrCh := make(chan error, 1)
		go func() {
			_, demuxErr := stdcopy.StdCopy(stdoutW, stderrW, rc)
			iox.Close(stdoutW)
			iox.Close(stderrW)
			if demuxErr != nil && !errors.Is(demuxErr, context.Canceled) {
				demuxErrCh <- demuxErr
				return
			}
			demuxErrCh <- nil
		}()

		readStream := func(rd io.Reader) <-chan string {
			ch := make(chan string, 64)
			go func() {
				defer close(ch)
				sc := bufio.NewScanner(rd)
				// logs podem ter linhas longas
				buf := make([]byte, 0, 64*1024)
				sc.Buffer(buf, 1024*1024)
				for sc.Scan() {
					select {
					case <-streamCtx.Done():
						return
					case ch <- sc.Text():
					}
				}
			}()
			return ch
		}

		stdoutCh := readStream(stdoutR)
		stderrCh := readStream(stderrR)

		for {
			select {
			case <-streamCtx.Done():
				return
			case line, ok := <-stdoutCh:
				if ok {
					if err := send(LogMessage{Stream: "stdout", Line: line}); err != nil {
						return
					}
				} else {
					stdoutCh = nil
				}
			case line, ok := <-stderrCh:
				if ok {
					if err := send(LogMessage{Stream: "stderr", Line: line}); err != nil {
						return
					}
				} else {
					stderrCh = nil
				}
			case demuxErr := <-demuxErrCh:
				if demuxErr != nil {
					_ = send(LogMessage{Stream: "meta", Line: "stream de logs encerrado"}) //nolint:errcheck // shutdown path
				}
				return
			}

			if stdoutCh == nil && stderrCh == nil {
				return
			}
		}
	}
}
