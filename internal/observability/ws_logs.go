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
)

type LogMessage struct {
	Stream string `json:"stream"` // stdout|stderr|meta
	Line   string `json:"line"`
}

type LogsStreamer struct {
	docker *client.Client
}

func NewLogsStreamer(docker *client.Client) *LogsStreamer {
	return &LogsStreamer{docker: docker}
}

func (s *LogsStreamer) Handler() http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Ambiente local / PaaS simples: aceita qualquer origin.
			return true
		},
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
		defer conn.Close()

		// Cancelamento quando socket fechar.
		streamCtx, stop := context.WithCancel(r.Context())
		defer stop()

		// Leitor de controle (pings/close) para detectar desconexão.
		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
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

		_ = send(LogMessage{Stream: "meta", Line: "connected"})

		rc, err := s.docker.ContainerLogs(streamCtx, containerRef, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Timestamps: false,
			Tail:       "100",
		})
		if err != nil {
			_ = send(LogMessage{Stream: "meta", Line: "erro ao abrir logs: " + err.Error()})
			return
		}
		defer rc.Close()

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
					_ = send(LogMessage{Stream: "stdout", Line: sc.Text()})
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
			_ = stdoutW.Close()
			_ = stderrW.Close()
			if demuxErr != nil && !errors.Is(demuxErr, context.Canceled) {
				demuxErrCh <- demuxErr
				return
			}
			demuxErrCh <- nil
		}()

		readStream := func(stream string, rd io.Reader) <-chan string {
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

		stdoutCh := readStream("stdout", stdoutR)
		stderrCh := readStream("stderr", stderrR)

		for {
			select {
			case <-streamCtx.Done():
				return
			case line, ok := <-stdoutCh:
				if ok {
					_ = send(LogMessage{Stream: "stdout", Line: line})
				} else {
					stdoutCh = nil
				}
			case line, ok := <-stderrCh:
				if ok {
					_ = send(LogMessage{Stream: "stderr", Line: line})
				} else {
					stderrCh = nil
				}
			case demuxErr := <-demuxErrCh:
				if demuxErr != nil {
					_ = send(LogMessage{Stream: "meta", Line: "demux erro: " + demuxErr.Error()})
				}
				return
			}

			if stdoutCh == nil && stderrCh == nil {
				return
			}
		}
	}
}
