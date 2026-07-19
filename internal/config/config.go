// Package config loads and validates gateway configuration from
// environment variables -- same style as freizone-server: no config
// file, everything explicit and env-driven so the gateway is trivial to
// run in a container.
package config

import (
	"fmt"
	"path/filepath"
)

// TLSMode selects how the gateway terminates TLS. Same three modes as
// freizone-server, for the same reason: a lone operator can run this with
// TLSMode=off behind their own reverse proxy, or let it get its own
// Let's Encrypt certificate directly.
type TLSMode string

const (
	TLSModeOff      TLSMode = "off"
	TLSModeManual   TLSMode = "manual"
	TLSModeAutocert TLSMode = "autocert"
)

// APNSEnvironment selects which Apple Push Notification service endpoint
// a (future) APNs sender would use.
type APNSEnvironment string

const (
	APNSEnvironmentSandbox    APNSEnvironment = "sandbox"
	APNSEnvironmentProduction APNSEnvironment = "production"
)

// Config holds all gateway configuration.
type Config struct {
	Domain      string
	HTTPAddr    string
	HTTPSAddr   string
	TLSMode     TLSMode
	TLSCertFile string
	TLSKeyFile  string
	DataDir     string

	// FCMCredentialsFile is a path to a Firebase service-account JSON
	// key. Empty disables FCM sending entirely -- reported as such via
	// GET /v1/capabilities rather than failing at startup, so a gateway
	// can be stood up Apple-only, FCM-only, or (for now) neither.
	FCMCredentialsFile string

	// APNs config is parsed and validated now so the operator-facing
	// surface exists, even though no sender consumes it yet (see
	// internal/push/apns.go) -- deliberately no architectural gap to
	// fill in later.
	APNSKeyFile     string
	APNSKeyID       string
	APNSTeamID      string
	APNSBundleID    string
	APNSEnvironment APNSEnvironment
}

const (
	envDomain             = "GATEWAY_DOMAIN"
	envHTTPAddr           = "GATEWAY_HTTP_ADDR"
	envHTTPSAddr          = "GATEWAY_HTTPS_ADDR"
	envTLSMode            = "GATEWAY_TLS_MODE"
	envTLSCertFile        = "GATEWAY_TLS_CERT_FILE"
	envTLSKeyFile         = "GATEWAY_TLS_KEY_FILE"
	envDataDir            = "GATEWAY_DATA_DIR"
	envFCMCredentialsFile = "GATEWAY_FCM_CREDENTIALS_FILE"
	envAPNSKeyFile        = "GATEWAY_APNS_KEY_FILE"
	envAPNSKeyID          = "GATEWAY_APNS_KEY_ID"
	envAPNSTeamID         = "GATEWAY_APNS_TEAM_ID"
	envAPNSBundleID       = "GATEWAY_APNS_BUNDLE_ID"
	envAPNSEnvironment    = "GATEWAY_APNS_ENVIRONMENT"
)

// Load reads configuration from the process environment.
func Load(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		Domain:             getenv(envDomain),
		HTTPAddr:           orDefault(getenv(envHTTPAddr), ":8080"),
		HTTPSAddr:          orDefault(getenv(envHTTPSAddr), ":8443"),
		TLSMode:            TLSMode(orDefault(getenv(envTLSMode), string(TLSModeOff))),
		TLSCertFile:        getenv(envTLSCertFile),
		TLSKeyFile:         getenv(envTLSKeyFile),
		DataDir:            orDefault(getenv(envDataDir), "./data"),
		FCMCredentialsFile: getenv(envFCMCredentialsFile),
		APNSKeyFile:        getenv(envAPNSKeyFile),
		APNSKeyID:          getenv(envAPNSKeyID),
		APNSTeamID:         getenv(envAPNSTeamID),
		APNSBundleID:       getenv(envAPNSBundleID),
		APNSEnvironment:    APNSEnvironment(orDefault(getenv(envAPNSEnvironment), string(APNSEnvironmentProduction))),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.TLSMode {
	case TLSModeOff, TLSModeManual, TLSModeAutocert:
	default:
		return fmt.Errorf("%s: invalid value %q (must be one of off, manual, autocert)", envTLSMode, c.TLSMode)
	}

	if c.TLSMode == TLSModeAutocert && c.Domain == "" {
		return fmt.Errorf("%s is required when %s=%s", envDomain, envTLSMode, TLSModeAutocert)
	}

	if c.TLSMode == TLSModeManual && (c.TLSCertFile == "" || c.TLSKeyFile == "") {
		return fmt.Errorf("%s and %s are required when %s=%s", envTLSCertFile, envTLSKeyFile, envTLSMode, TLSModeManual)
	}

	switch c.APNSEnvironment {
	case APNSEnvironmentSandbox, APNSEnvironmentProduction:
	default:
		return fmt.Errorf("%s: invalid value %q (must be one of sandbox, production)", envAPNSEnvironment, c.APNSEnvironment)
	}

	return nil
}

// RevokedKeysFile is where the gateway's revocation list lives -- see
// internal/revoke.
func (c *Config) RevokedKeysFile() string {
	return filepath.Join(c.DataDir, "revoked_keys.txt")
}

// AutocertCacheDir is where the autocert certificate cache lives, when
// TLSMode is autocert.
func (c *Config) AutocertCacheDir() string {
	return filepath.Join(c.DataDir, "autocert-cache")
}

// FCMConfigured reports whether FCM sending is available on this
// instance.
func (c *Config) FCMConfigured() bool {
	return c.FCMCredentialsFile != ""
}

// APNSConfigured reports whether APNs sending is (eventually) available
// on this instance. Always false today -- internal/push's APNs sender
// isn't implemented yet -- but the config check is written now so
// wiring it in later doesn't require touching this function.
func (c *Config) APNSConfigured() bool {
	return c.APNSKeyFile != "" && c.APNSKeyID != "" && c.APNSTeamID != "" && c.APNSBundleID != ""
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
