// Package server assembles the HTTP/HTTPS listeners for the gateway,
// handling the three supported TLS modes (off, manual, autocert) and
// graceful shutdown. Deliberately identical in shape to freizone-server's
// internal/server package -- small enough (~190 lines total) that a
// shared library across the two repos would cost more in versioning
// overhead than it saves.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/behringer24/freizone-gateway/internal/config"
)

// Options configures how the server terminates connections and serves
// Handler.
type Options struct {
	Domain           string
	HTTPAddr         string
	HTTPSAddr        string
	TLSMode          config.TLSMode
	TLSCertFile      string
	TLSKeyFile       string
	AutocertCacheDir string
	Handler          http.Handler
	Logger           *slog.Logger
}

// Server wraps one or two http.Server instances, depending on TLS mode.
type Server struct {
	opts    Options
	servers []*http.Server
}

// Connection timeouts. freizone-server's equivalent block can only set two
// of net/http's four, because a blob upload may legitimately take minutes
// to read and an SSE stream stays open for hours. Neither exists here:
// this gateway has three routes, the largest body it accepts is a few
// hundred bytes (see maxRequestBodyBytes), and nothing streams. So all
// four are safe, and leaving any of them at zero would only mean a hung
// connection holds a goroutine and a file descriptor indefinitely.
const (
	// readHeaderTimeout is the Slowloris bound: a client that opens a
	// connection and dribbles its headers holds nothing for longer than
	// this.
	readHeaderTimeout = 15 * time.Second

	// readTimeout covers headers and body together.
	readTimeout = 30 * time.Second

	// writeTimeout covers the handler and the response write, so it must
	// stay comfortably above internal/api's sendTimeout -- the bound on
	// how long a push handler may legitimately block waiting on the
	// upstream service. Set it below that and a healthy-but-slow FCM call
	// would have its connection cut while the handler went on believing
	// it had answered.
	writeTimeout = 30 * time.Second

	// idleTimeout closes a kept-alive connection with no request in
	// flight. Comfortably above any client's own reuse interval, so it
	// costs a reconnect only for connections genuinely finished with.
	idleTimeout = 150 * time.Second
)

// withTimeouts applies the connection timeouts above -- see that block for
// why this server can set all four.
func withTimeouts(srv *http.Server) *http.Server {
	srv.ReadHeaderTimeout = readHeaderTimeout
	srv.ReadTimeout = readTimeout
	srv.WriteTimeout = writeTimeout
	srv.IdleTimeout = idleTimeout
	return srv
}

// New builds a Server for opts. It does not start listening.
func New(opts Options) (*Server, error) {
	wrapped := withLogging(withRecover(withMaxBody(opts.Handler), opts.Logger), opts.Logger)

	switch opts.TLSMode {
	case config.TLSModeOff:
		return &Server{opts: opts, servers: []*http.Server{
			withTimeouts(&http.Server{Addr: opts.HTTPAddr, Handler: wrapped}),
		}}, nil

	case config.TLSModeManual:
		return &Server{opts: opts, servers: []*http.Server{
			withTimeouts(&http.Server{Addr: opts.HTTPSAddr, Handler: wrapped}),
		}}, nil

	case config.TLSModeAutocert:
		if opts.Domain == "" {
			return nil, errors.New("server: autocert mode requires a domain")
		}
		mgr := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(opts.Domain),
			Cache:      autocert.DirCache(opts.AutocertCacheDir),
		}
		httpsServer := withTimeouts(&http.Server{
			Addr:      opts.HTTPSAddr,
			Handler:   wrapped,
			TLSConfig: mgr.TLSConfig(),
		})
		httpServer := withTimeouts(&http.Server{
			Addr:    opts.HTTPAddr,
			Handler: mgr.HTTPHandler(nil), // serves ACME HTTP-01 challenges, redirects everything else to https
		})
		return &Server{opts: opts, servers: []*http.Server{httpServer, httpsServer}}, nil

	default:
		return nil, fmt.Errorf("server: unknown TLS mode %q", opts.TLSMode)
	}
}

// ListenAndServe starts all configured listeners and blocks until they have
// all stopped -- either because Shutdown was called (returns nil) or
// because one of them failed to start/run (returns that error, after best-
// effort shutting down the others).
func (s *Server) ListenAndServe() error {
	errCh := make(chan error, len(s.servers))
	for _, srv := range s.servers {
		srv := srv
		go func() {
			var err error
			switch {
			case srv.TLSConfig != nil:
				err = srv.ListenAndServeTLS("", "") // cert/key come from TLSConfig.GetCertificate (autocert)
			case s.opts.TLSMode == config.TLSModeManual:
				err = srv.ListenAndServeTLS(s.opts.TLSCertFile, s.opts.TLSKeyFile)
			default:
				err = srv.ListenAndServe()
			}
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errCh <- err
		}()
	}

	var firstErr error
	shutdownTriggered := false
	for range s.servers {
		if err := <-errCh; err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if !shutdownTriggered {
				shutdownTriggered = true
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				s.Shutdown(ctx) //nolint:errcheck // best-effort; firstErr is what we report
				cancel()
			}
		}
	}
	return firstErr
}

// Shutdown gracefully stops all configured listeners.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error
	for _, srv := range s.servers {
		if err := srv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
