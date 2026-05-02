package proxy

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Config controls bind address, database access, reload polling, and logging for [New].
type Config struct {
	// Addr is the listen address for the reverse proxy (default ":80").
	Addr string
	// DB is the SQLite database connection shared with the route store.
	DB *sql.DB
	// PollInterval is the polling interval when checking for route version changes (default 1s).
	PollInterval time.Duration
	// Logger receives internal warnings; if nil, slog.Default is used.
	Logger *slog.Logger
}

// Proxy é um reverse proxy dinâmico roteado por Host.
// Ele mantém um cache em memória e faz hot reload quando as rotas mudam no SQLite.
type Proxy struct {
	cfg   Config
	store *Store

	// routes guarda snapshot imutável: map[domain]target
	routes atomic.Value

	// lastVersion é atualizado após cada reload.
	lastVersion atomic.Int64

	// notify força um reload imediato (ex.: após deploy).
	notify chan struct{}

	// upstream reutiliza um pool de ligações; um Transport por pedido esgotava sockets
	// em testes com muitos ServeHTTP (ex. hot-reload em loop).
	upstream *http.Transport
}

// New builds a [Proxy] backed by cfg.DB. Schema creation happens on [Proxy.Run].
func New(cfg Config) (*Proxy, error) {
	if cfg.DB == nil {
		return nil, errors.New("DB nao pode ser nil")
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = ":80"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	store, err := NewStore(cfg.DB)
	if err != nil {
		return nil, err
	}

	p := &Proxy{
		cfg:    cfg,
		store:  store,
		notify: make(chan struct{}, 1),
		upstream: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}
	p.routes.Store(map[string]string{})
	return p, nil
}

// NotifyReload acorda o loop de hot reload imediatamente.
// Use isto ao final de um deploy (após persistir a rota no SQLite) para refletir a mudança sem delay.
func (p *Proxy) NotifyReload() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// Run inicia o servidor HTTP e o loop de hot reload. Bloqueia até ctx cancelar.
func (p *Proxy) Run(ctx context.Context) error {
	if err := p.store.EnsureSchema(ctx); err != nil {
		return err
	}
	if err := p.reloadIfChanged(ctx, true); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              p.cfg.Addr,
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // streaming responses / long downloads — leave unlimited
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		// http.Server.ListenAndServe retorna http.ErrServerClosed em shutdown normal.
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go p.reloadLoop(ctx)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.cfg.Logger.Warn("proxy shutdown", slog.Any("err", err))
		}
		p.upstream.CloseIdleConnections()
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) || err == nil {
			return nil
		}
		return err
	case err := <-errCh:
		p.upstream.CloseIdleConnections()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (p *Proxy) reloadLoop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.reloadIfChanged(ctx, false); err != nil {
				p.cfg.Logger.Warn("proxy hot reload", slog.Any("err", err))
			}
		case <-p.notify:
			if err := p.reloadIfChanged(ctx, false); err != nil {
				p.cfg.Logger.Warn("proxy hot reload notify", slog.Any("err", err))
			}
		}
	}
}

func (p *Proxy) reloadIfChanged(ctx context.Context, force bool) error {
	routes, v, err := p.store.LoadAll(ctx)
	if err != nil {
		return err
	}
	if !force && v == p.lastVersion.Load() {
		return nil
	}

	// snapshot imutável
	p.routes.Store(routes)
	p.lastVersion.Store(v)
	return nil
}

func clientIP(r *http.Request) string {
	// r.RemoteAddr é "ip:port"
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func appendXForwardedFor(h http.Header, ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	const key = "X-Forwarded-For"
	if prior := strings.TrimSpace(h.Get(key)); prior != "" {
		h.Set(key, prior+", "+ip)
		return
	}
	h.Set(key, ip)
}

// ServeHTTP implements [http.Handler], routing by request Host to the configured upstream.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := normalizeDomain(r.Host)
	if host == "" {
		http.Error(w, "host obrigatorio", http.StatusBadRequest)
		return
	}

	raw := p.routes.Load()
	snap, snapOK := raw.(map[string]string)
	if !snapOK || snap == nil {
		http.Error(w, "rotas indisponiveis", http.StatusInternalServerError)
		return
	}
	target, hasRoute := snap[host]
	if !hasRoute {
		http.Error(w, "rota nao encontrada", http.StatusNotFound)
		return
	}

	targetURL := &url.URL{Scheme: "http", Host: strings.TrimSpace(target)}
	proxy := &httputil.ReverseProxy{
		Transport: p.upstream,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Define upstream URL (host+scheme) e mantém path/query.
			pr.SetURL(targetURL)

			// Encaminhamento de headers.
			out := pr.Out
			out.Host = targetURL.Host

			out.Header.Set("X-Forwarded-Host", r.Host)
			if r.TLS != nil {
				out.Header.Set("X-Forwarded-Proto", "https")
			} else {
				out.Header.Set("X-Forwarded-Proto", "http")
			}
			appendXForwardedFor(out.Header, clientIP(r))
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			p.cfg.Logger.Warn("proxy upstream",
				slog.String("host", host),
				slog.String("target", target),
				slog.Any("err", err),
			)
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		},
		ModifyResponse: func(resp *http.Response) error {
			// Garante que caches intermediários não “congelem” rotas durante rollout.
			// (especialmente útil em ambientes simples de estudo).
			resp.Header.Del("Server")
			return nil
		},
	}

	proxy.ServeHTTP(w, r)
}
