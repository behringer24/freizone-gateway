// Command gateway runs the Freizone push gateway: a small, mostly
// stateless relay that holds FCM/APNs credentials so individual
// freizone-server operators never have to. See ../../README.md for the
// full picture -- any freizone-server can call in without prior
// registration (see internal/auth), so the only administrative action
// this binary supports is revoking an abusive caller's key.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/behringer24/freizone-gateway/internal/api"
	"github.com/behringer24/freizone-gateway/internal/auth"
	"github.com/behringer24/freizone-gateway/internal/config"
	"github.com/behringer24/freizone-gateway/internal/push"
	"github.com/behringer24/freizone-gateway/internal/revoke"
	"github.com/behringer24/freizone-gateway/internal/server"
)

const (
	nonceSweepInterval   = 1 * time.Minute
	revokeReloadInterval = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	revokeKey := flag.String("revoke-key", "", "append the given base64-encoded Ed25519 public key to the revocation list, then exit -- takes effect for running instances within revokeReloadInterval, no restart needed")
	flag.Parse()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	if *revokeKey != "" {
		if err := revoke.Revoke(cfg.RevokedKeysFile(), *revokeKey); err != nil {
			return fmt.Errorf("revoking key: %w", err)
		}
		fmt.Printf("revoked: %s\n", *revokeKey)
		return nil
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	revoked, err := revoke.Load(cfg.RevokedKeysFile())
	if err != nil {
		return fmt.Errorf("loading revocation list: %w", err)
	}

	senders := map[string]push.Sender{
		push.PlatformAPNS: push.NewAPNSSender(),
	}
	capabilities := map[string]bool{
		push.PlatformFCM:  cfg.FCMConfigured(),
		push.PlatformAPNS: cfg.APNSConfigured(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.FCMConfigured() {
		fcmSender, err := push.NewFCMSender(ctx, cfg.FCMCredentialsFile)
		if err != nil {
			return fmt.Errorf("initializing fcm sender: %w", err)
		}
		senders[push.PlatformFCM] = fcmSender
	}
	if capabilities[push.PlatformAPNS] {
		logger.Warn("GATEWAY_APNS_* is configured, but apns sending isn't implemented yet -- requests for platform=apns will fail")
	}

	authMW := auth.NewMiddleware(revoked, logger)
	a := api.New(authMW, senders, capabilities, logger)
	handler := a.Router()

	srv, err := server.New(server.Options{
		Domain:           cfg.Domain,
		HTTPAddr:         cfg.HTTPAddr,
		HTTPSAddr:        cfg.HTTPSAddr,
		TLSMode:          cfg.TLSMode,
		TLSCertFile:      cfg.TLSCertFile,
		TLSKeyFile:       cfg.TLSKeyFile,
		AutocertCacheDir: cfg.AutocertCacheDir(),
		Handler:          handler,
		Logger:           logger,
	})
	if err != nil {
		return fmt.Errorf("configuring server: %w", err)
	}

	nonceSweepDone := runNonceSweep(ctx, authMW)
	revokeReloadDone := runRevokeReload(ctx, revoked, logger)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	logger.Info("gateway started",
		"tls_mode", string(cfg.TLSMode), "http_addr", cfg.HTTPAddr, "https_addr", cfg.HTTPSAddr,
		"fcm_configured", capabilities[push.PlatformFCM], "apns_configured", capabilities[push.PlatformAPNS],
	)

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	<-nonceSweepDone
	<-revokeReloadDone
	return nil
}

// runNonceSweep periodically prunes expired replay-guard entries until
// ctx is cancelled. The returned channel is closed once the goroutine
// has exited.
func runNonceSweep(ctx context.Context, authMW *auth.Middleware) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(nonceSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				authMW.SweepNonces()
			}
		}
	}()
	return done
}

// runRevokeReload periodically re-reads the revocation list from disk,
// so an operator's `-revoke-key` (or a direct file edit) takes effect
// without restarting the running gateway. The returned channel is
// closed once the goroutine has exited.
func runRevokeReload(ctx context.Context, revoked *revoke.List, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(revokeReloadInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := revoked.Reload(); err != nil {
					logger.Warn("reloading revocation list failed", "error", err)
				}
			}
		}
	}()
	return done
}
