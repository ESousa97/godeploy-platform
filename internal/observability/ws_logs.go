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

// Stream values used in [LogMessage.Stream].
const (
	streamStdout = "stdout"
	streamStderr = "stderr"
	streamMeta   = "meta"
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
		s.serveLogsWS(w, r, upgrader)
	}
}

func (s *LogsStreamer) serveLogsWS(w http.ResponseWriter, r *http.Request, upgrader websocket.Upgrader) {
	containerRef := strings.TrimSpace(r.URL.Query().Get("container"))
	if containerRef == "" {
		http.Error(w, "required query parameter: container", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	inspect, err := s.docker.ContainerInspect(ctx, containerRef)
	cancel()
	if err != nil {
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer iox.Close(conn)

	streamCtx, stop := context.WithCancel(r.Context())
	defer stop()

	s.startWSReadPump(conn, stop)

	sendMu := &sync.Mutex{}
	send := newWSLogSender(conn, sendMu)

	if err := send(LogMessage{Stream: streamMeta, Line: "connected"}); err != nil {
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
		_ = send(LogMessage{Stream: streamMeta, Line: "failed to open logs"}) //nolint:errcheck // client already misconfigured
		return
	}
	defer iox.Close(rc)

	if inspect.Config != nil && inspect.Config.Tty {
		streamTTYLogsToWS(streamCtx, rc, send)
		return
	}
	streamDemuxedLogsToWS(streamCtx, rc, send)
}

func (s *LogsStreamer) startWSReadPump(conn *websocket.Conn, stop context.CancelFunc) {
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
}

func newWSLogSender(conn *websocket.Conn, sendMu *sync.Mutex) func(LogMessage) error {
	return func(msg LogMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return conn.WriteJSON(msg)
	}
}

func streamTTYLogsToWS(streamCtx context.Context, rc io.ReadCloser, send func(LogMessage) error) {
	sc := bufio.NewScanner(rc)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		select {
		case <-streamCtx.Done():
			return
		default:
			if err := send(LogMessage{Stream: streamStdout, Line: sc.Text()}); err != nil {
				return
			}
		}
	}
}

func streamDemuxedLogsToWS(streamCtx context.Context, rc io.ReadCloser, send func(LogMessage) error) {
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

	stdoutCh := logLinesChan(streamCtx, stdoutR)
	stderrCh := logLinesChan(streamCtx, stderrR)

	for {
		select {
		case <-streamCtx.Done():
			return
		case line, ok := <-stdoutCh:
			if ok {
				if err := send(LogMessage{Stream: streamStdout, Line: line}); err != nil {
					return
				}
			} else {
				stdoutCh = nil
			}
		case line, ok := <-stderrCh:
			if ok {
				if err := send(LogMessage{Stream: streamStderr, Line: line}); err != nil {
					return
				}
			} else {
				stderrCh = nil
			}
		case demuxErr := <-demuxErrCh:
			if demuxErr != nil {
				_ = send(LogMessage{Stream: streamMeta, Line: "stream de logs encerrado"}) //nolint:errcheck // shutdown path
			}
			return
		}

		if stdoutCh == nil && stderrCh == nil {
			return
		}
	}
}

func logLinesChan(streamCtx context.Context, rd io.Reader) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(rd)
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
