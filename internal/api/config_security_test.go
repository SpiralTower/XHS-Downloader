package api

import (
	"testing"
	"time"
)

func clearRuntimeConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"HOST",
		"PORT",
		"XHS_VOLUME_DIR",
		"WEB_DIST_DIR",
		"XHS_REQUEST_TIMEOUT",
		"XHS_DOWNLOAD_TIMEOUT",
		"XHS_DOWNLOAD_IDLE_TIMEOUT",
		"XHS_ALLOW_PRIVATE_PROXY",
		"XHS_MAX_BODY_BYTES",
		"XHS_MAX_MEDIA_BYTES",
		"XHS_PUBLIC_RATE_LIMIT_PER_MINUTE",
		"XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE",
		"XHS_PUBLIC_MAX_CONCURRENCY",
	} {
		t.Setenv(name, "")
	}
}

func TestConfigDownloadSecurityDefaults(t *testing.T) {
	clearRuntimeConfigEnv(t)
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.DownloadTimeout != 30*time.Minute {
		t.Fatalf("DownloadTimeout = %s", config.DownloadTimeout)
	}
	if config.DownloadIdleTimeout != 60*time.Second {
		t.Fatalf("DownloadIdleTimeout = %s", config.DownloadIdleTimeout)
	}
	if config.AllowPrivateProxy {
		t.Fatal("AllowPrivateProxy defaults to true")
	}
	if config.MaxMediaBytes != defaultMaxMediaBytes {
		t.Fatalf("MaxMediaBytes = %d", config.MaxMediaBytes)
	}
	if config.PublicRateLimitPerMinute != defaultPublicRateLimit {
		t.Fatalf("PublicRateLimitPerMinute = %d", config.PublicRateLimitPerMinute)
	}
	if config.PublicGlobalRateLimitPerMinute != defaultPublicGlobalRateLimit {
		t.Fatalf("PublicGlobalRateLimitPerMinute = %d", config.PublicGlobalRateLimitPerMinute)
	}
	if config.PublicMaxConcurrency != defaultPublicMaxConcurrency {
		t.Fatalf("PublicMaxConcurrency = %d", config.PublicMaxConcurrency)
	}
}

func TestConfigDownloadSecurityEnvironment(t *testing.T) {
	clearRuntimeConfigEnv(t)
	t.Setenv("XHS_DOWNLOAD_TIMEOUT", "45m")
	t.Setenv("XHS_DOWNLOAD_IDLE_TIMEOUT", "75s")
	t.Setenv("XHS_ALLOW_PRIVATE_PROXY", "true")
	t.Setenv("XHS_MAX_MEDIA_BYTES", "12345")
	t.Setenv("XHS_PUBLIC_RATE_LIMIT_PER_MINUTE", "7")
	t.Setenv("XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE", "70")
	t.Setenv("XHS_PUBLIC_MAX_CONCURRENCY", "3")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.DownloadTimeout != 45*time.Minute {
		t.Fatalf("DownloadTimeout = %s", config.DownloadTimeout)
	}
	if config.DownloadIdleTimeout != 75*time.Second {
		t.Fatalf("DownloadIdleTimeout = %s", config.DownloadIdleTimeout)
	}
	if !config.AllowPrivateProxy {
		t.Fatal("AllowPrivateProxy opt-in was ignored")
	}
	if config.MaxMediaBytes != 12345 {
		t.Fatalf("MaxMediaBytes = %d", config.MaxMediaBytes)
	}
	if config.PublicRateLimitPerMinute != 7 {
		t.Fatalf("PublicRateLimitPerMinute = %d", config.PublicRateLimitPerMinute)
	}
	if config.PublicGlobalRateLimitPerMinute != 70 {
		t.Fatalf("PublicGlobalRateLimitPerMinute = %d", config.PublicGlobalRateLimitPerMinute)
	}
	if config.PublicMaxConcurrency != 3 {
		t.Fatalf("PublicMaxConcurrency = %d", config.PublicMaxConcurrency)
	}
}

func TestConfigRejectsInvalidDownloadSecurityEnvironment(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"XHS_DOWNLOAD_TIMEOUT", "0s"},
		{"XHS_DOWNLOAD_TIMEOUT", "invalid"},
		{"XHS_DOWNLOAD_IDLE_TIMEOUT", "-1s"},
		{"XHS_DOWNLOAD_IDLE_TIMEOUT", "invalid"},
		{"XHS_ALLOW_PRIVATE_PROXY", "sometimes"},
		{"XHS_MAX_MEDIA_BYTES", "0"},
		{"XHS_MAX_MEDIA_BYTES", "invalid"},
		{"XHS_PUBLIC_RATE_LIMIT_PER_MINUTE", "0"},
		{"XHS_PUBLIC_RATE_LIMIT_PER_MINUTE", "invalid"},
		{"XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE", "-1"},
		{"XHS_PUBLIC_GLOBAL_RATE_LIMIT_PER_MINUTE", "invalid"},
		{"XHS_PUBLIC_MAX_CONCURRENCY", "0"},
		{"XHS_PUBLIC_MAX_CONCURRENCY", "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name+"="+test.value, func(t *testing.T) {
			clearRuntimeConfigEnv(t)
			t.Setenv(test.name, test.value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("invalid environment value was accepted")
			}
		})
	}
}

func TestHTTPServerDisablesWholeHandlerWriteTimeout(t *testing.T) {
	app := newTestApp(t)
	server := app.HTTPServer()
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want stage-specific contexts", server.WriteTimeout)
	}
	if app.downloads.totalTimeout != defaultDownloadTimeout {
		t.Fatalf("download total timeout = %s", app.downloads.totalTimeout)
	}
	if app.downloads.idleTimeout != defaultDownloadIdle {
		t.Fatalf("download idle timeout = %s", app.downloads.idleTimeout)
	}
	if app.downloads.maxMediaBytes != defaultMaxMediaBytes {
		t.Fatalf("max media bytes = %d", app.downloads.maxMediaBytes)
	}
}
