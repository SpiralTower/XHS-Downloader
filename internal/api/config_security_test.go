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
}

func TestConfigDownloadSecurityEnvironment(t *testing.T) {
	clearRuntimeConfigEnv(t)
	t.Setenv("XHS_DOWNLOAD_TIMEOUT", "45m")
	t.Setenv("XHS_DOWNLOAD_IDLE_TIMEOUT", "75s")
	t.Setenv("XHS_ALLOW_PRIVATE_PROXY", "true")

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
}
