package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHost                  = "0.0.0.0"
	defaultPort                  = 5556
	defaultVolumeDir             = "Volume"
	defaultWebDistDir            = "web/dist"
	defaultRequestTimeout        = 15 * time.Second
	defaultDownloadTimeout       = 30 * time.Minute
	defaultDownloadIdle          = 60 * time.Second
	defaultMaxBodyBytes          = int64(1 << 20)
	defaultMaxUpstreamBody       = int64(16 << 20)
	defaultMaxMediaBytes         = int64(2 << 30)
	defaultPublicRateLimit       = 12
	defaultPublicGlobalRateLimit = 120
	defaultPublicMaxConcurrency  = 4
	defaultAdminUsername         = "admin"
	defaultAdminSessionTTL       = 7 * 24 * time.Hour
)

// Config contains the runtime settings for the core API server.
type Config struct {
	Host                           string
	Port                           int
	VolumeDir                      string
	WebDistDir                     string
	RequestTimeout                 time.Duration
	DownloadTimeout                time.Duration
	DownloadIdleTimeout            time.Duration
	AllowPrivateProxy              bool
	MaxBodyBytes                   int64
	MaxUpstreamBody                int64
	MaxMediaBytes                  int64
	PublicRateLimitPerMinute       int
	PublicGlobalRateLimitPerMinute int
	PublicMaxConcurrency           int
	DatabasePath                   string
	SecretKeyPath                  string
	SecretKeyManaged               bool
	AdminUsername                  string
	AdminPassword                  string
	AdminPasswordRequired          bool
	AdminSessionTTL                time.Duration
	SessionCookieSecure            bool
}

// ConfigFromEnv loads server configuration from environment variables.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Host:                           envOrDefault("HOST", defaultHost),
		Port:                           defaultPort,
		VolumeDir:                      envOrDefault("XHS_VOLUME_DIR", defaultVolumeDir),
		WebDistDir:                     envOrDefault("WEB_DIST_DIR", defaultWebDistDir),
		RequestTimeout:                 defaultRequestTimeout,
		DownloadTimeout:                defaultDownloadTimeout,
		DownloadIdleTimeout:            defaultDownloadIdle,
		MaxBodyBytes:                   defaultMaxBodyBytes,
		MaxUpstreamBody:                defaultMaxUpstreamBody,
		MaxMediaBytes:                  defaultMaxMediaBytes,
		PublicRateLimitPerMinute:       defaultPublicRateLimit,
		PublicGlobalRateLimitPerMinute: defaultPublicGlobalRateLimit,
		PublicMaxConcurrency:           defaultPublicMaxConcurrency,
		AdminUsername:                  envOrDefault("XHS_ADMIN_USERNAME", defaultAdminUsername),
		AdminSessionTTL:                defaultAdminSessionTTL,
	}
	cfg.DatabasePath = strings.TrimSpace(os.Getenv("XHS_DATABASE_PATH"))
	cfg.SecretKeyPath = strings.TrimSpace(os.Getenv("XHS_SECRET_KEY_PATH"))
	cfg.SecretKeyManaged = cfg.SecretKeyPath == ""

	password, err := adminPasswordFromEnv()
	if err != nil {
		return Config{}, err
	}
	cfg.AdminPassword = password
	cfg.AdminPasswordRequired = true

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
	if value := strings.TrimSpace(os.Getenv("XHS_MAX_MEDIA_BYTES")); value != "" {
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil || limit < 1 {
			return Config{}, fmt.Errorf("invalid XHS_MAX_MEDIA_BYTES %q", value)
		}
		cfg.MaxMediaBytes = limit
	}
	for _, setting := range []struct {
		name   string
		target *int
	}{
		{name: "XHS_PUBLIC_RATE_LIMIT_PER_MINUTE", target: &cfg.PublicRateLimitPerMinute},
		{name: "XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE", target: &cfg.PublicGlobalRateLimitPerMinute},
		{name: "XHS_PUBLIC_MAX_CONCURRENCY", target: &cfg.PublicMaxConcurrency},
	} {
		if value := strings.TrimSpace(os.Getenv(setting.name)); value != "" {
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 1 {
				return Config{}, fmt.Errorf("invalid %s %q", setting.name, value)
			}
			*setting.target = limit
		}
	}
	if value := strings.TrimSpace(os.Getenv("XHS_ADMIN_SESSION_TTL")); value != "" {
		ttl, err := time.ParseDuration(value)
		if err != nil || ttl <= 0 {
			return Config{}, fmt.Errorf("invalid XHS_ADMIN_SESSION_TTL %q", value)
		}
		cfg.AdminSessionTTL = ttl
	}
	if value := strings.TrimSpace(os.Getenv("XHS_SESSION_COOKIE_SECURE")); value != "" {
		secure, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid XHS_SESSION_COOKIE_SECURE %q", value)
		}
		cfg.SessionCookieSecure = secure
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
	if c.MaxMediaBytes <= 0 {
		c.MaxMediaBytes = defaultMaxMediaBytes
	}
	if c.PublicRateLimitPerMinute <= 0 {
		c.PublicRateLimitPerMinute = defaultPublicRateLimit
	}
	if c.PublicGlobalRateLimitPerMinute <= 0 {
		c.PublicGlobalRateLimitPerMinute = defaultPublicGlobalRateLimit
	}
	if c.PublicMaxConcurrency <= 0 {
		c.PublicMaxConcurrency = defaultPublicMaxConcurrency
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		c.DatabasePath = filepath.Join(c.VolumeDir, "Data", "xhs.sqlite3")
	}
	if strings.TrimSpace(c.SecretKeyPath) == "" {
		c.SecretKeyPath = filepath.Join(filepath.Dir(c.DatabasePath), "secrets.key")
		c.SecretKeyManaged = true
	}
	if strings.TrimSpace(c.AdminUsername) == "" {
		c.AdminUsername = defaultAdminUsername
	}
	if c.AdminSessionTTL <= 0 {
		c.AdminSessionTTL = defaultAdminSessionTTL
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

func adminPasswordFromEnv() (string, error) {
	if path := strings.TrimSpace(os.Getenv("XHS_ADMIN_PASSWORD_FILE")); path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read XHS_ADMIN_PASSWORD_FILE: %w", err)
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r"), nil
	}
	return os.Getenv("XHS_ADMIN_PASSWORD"), nil
}
