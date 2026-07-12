package api

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHost            = "0.0.0.0"
	defaultPort            = 5556
	defaultVolumeDir       = "Volume"
	defaultWebDistDir      = "web/dist"
	defaultRequestTimeout  = 15 * time.Second
	defaultDownloadTimeout = 30 * time.Minute
	defaultDownloadIdle    = 60 * time.Second
	defaultMaxBodyBytes    = int64(1 << 20)
	defaultMaxUpstreamBody = int64(16 << 20)
)

// Config contains the runtime settings for the core API server.
type Config struct {
	Host                string
	Port                int
	VolumeDir           string
	WebDistDir          string
	RequestTimeout      time.Duration
	DownloadTimeout     time.Duration
	DownloadIdleTimeout time.Duration
	AllowPrivateProxy   bool
	MaxBodyBytes        int64
	MaxUpstreamBody     int64
}

// ConfigFromEnv loads server configuration from environment variables.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Host:                envOrDefault("HOST", defaultHost),
		Port:                defaultPort,
		VolumeDir:           envOrDefault("XHS_VOLUME_DIR", defaultVolumeDir),
		WebDistDir:          envOrDefault("WEB_DIST_DIR", defaultWebDistDir),
		RequestTimeout:      defaultRequestTimeout,
		DownloadTimeout:     defaultDownloadTimeout,
		DownloadIdleTimeout: defaultDownloadIdle,
		MaxBodyBytes:        defaultMaxBodyBytes,
		MaxUpstreamBody:     defaultMaxUpstreamBody,
	}

	if value := strings.TrimSpace(os.Getenv("PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid PORT %q", value)
		}
		cfg.Port = port
	}
	if value := strings.TrimSpace(os.Getenv("XHS_REQUEST_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("invalid XHS_REQUEST_TIMEOUT %q", value)
		}
		cfg.RequestTimeout = timeout
	}
	if value := strings.TrimSpace(os.Getenv("XHS_DOWNLOAD_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("invalid XHS_DOWNLOAD_TIMEOUT %q", value)
		}
		cfg.DownloadTimeout = timeout
	}
	if value := strings.TrimSpace(os.Getenv("XHS_DOWNLOAD_IDLE_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("invalid XHS_DOWNLOAD_IDLE_TIMEOUT %q", value)
		}
		cfg.DownloadIdleTimeout = timeout
	}
	if value := strings.TrimSpace(os.Getenv("XHS_ALLOW_PRIVATE_PROXY")); value != "" {
		allow, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid XHS_ALLOW_PRIVATE_PROXY %q", value)
		}
		cfg.AllowPrivateProxy = allow
	}
	if value := strings.TrimSpace(os.Getenv("XHS_MAX_BODY_BYTES")); value != "" {
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil || limit < 1 {
			return Config{}, fmt.Errorf("invalid XHS_MAX_BODY_BYTES %q", value)
		}
		cfg.MaxBodyBytes = limit
	}

	return cfg.withDefaults(), nil
}

func (c Config) withDefaults() Config {
	if strings.TrimSpace(c.Host) == "" {
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if strings.TrimSpace(c.VolumeDir) == "" {
		c.VolumeDir = defaultVolumeDir
	}
	if strings.TrimSpace(c.WebDistDir) == "" {
		c.WebDistDir = defaultWebDistDir
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = defaultRequestTimeout
	}
	if c.DownloadTimeout <= 0 {
		c.DownloadTimeout = defaultDownloadTimeout
	}
	if c.DownloadIdleTimeout <= 0 {
		c.DownloadIdleTimeout = defaultDownloadIdle
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.MaxUpstreamBody <= 0 {
		c.MaxUpstreamBody = defaultMaxUpstreamBody
	}
	return c
}

func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
