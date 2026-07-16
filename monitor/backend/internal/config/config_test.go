package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestLoadConfigWebhookDefaults(t *testing.T) {
	setMonitorConfigEnvironment(t)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.WebhookDispatchInterval != 2*time.Second || config.WebhookHTTPTimeout != 10*time.Second {
		t.Fatalf("webhook intervals = %s, %s", config.WebhookDispatchInterval, config.WebhookHTTPTimeout)
	}
	if config.WebhookLeaseDuration != 30*time.Second || config.WebhookMaxAttempts != 8 {
		t.Fatalf("webhook lease/max = %s/%d", config.WebhookLeaseDuration, config.WebhookMaxAttempts)
	}
	if config.WebhookAllowPrivate {
		t.Fatal("private webhook targets are enabled by default")
	}
	if len(config.EncryptionKey) != 32 {
		t.Fatalf("encryption key length = %d", len(config.EncryptionKey))
	}
}

func TestLoadConfigEmailDefaultsWithoutSMTP(t *testing.T) {
	setMonitorConfigEnvironment(t)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.EmailSMTPConfigured() {
		t.Fatal("email SMTP is configured without a host")
	}
	if config.EmailDispatchInterval != 2*time.Second || config.EmailSMTPTimeout != 15*time.Second ||
		config.EmailLeaseDuration != 30*time.Second || config.EmailMaxAttempts != 8 ||
		config.EmailSMTPPort != 587 || config.EmailSMTPTLSMode != "starttls" {
		t.Fatalf("email defaults = %#v", config)
	}
}

func TestLoadConfigRejectsWebhookLeaseShorterThanHTTPTimeout(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_WEBHOOK_HTTP_TIMEOUT", "30s")
	t.Setenv("MONITOR_WEBHOOK_LEASE_DURATION", "10s")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid webhook lease was accepted")
	}
}

func TestLoadConfigRejectsInvertedWebhookBackoff(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF", "2m")
	t.Setenv("MONITOR_WEBHOOK_RETRY_MAX_BACKOFF", "1m")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("inverted webhook backoff was accepted")
	}
}

func TestLoadConfigValidatesEmailSMTPSettings(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_HOST", "smtp.example.com")
	t.Setenv("MONITOR_EMAIL_FROM_ADDRESS", "monitor@example.com")
	t.Setenv("MONITOR_EMAIL_SMTP_TLS_MODE", "tls")
	t.Setenv("MONITOR_EMAIL_SMTP_USERNAME", "monitor")
	t.Setenv("MONITOR_EMAIL_SMTP_PASSWORD", "secret")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.EmailSMTPConfigured() || config.EmailSMTPTLSMode != "tls" {
		t.Fatalf("email config = %#v", config)
	}
}

func TestLoadConfigRejectsPartialEmailAuthentication(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_USERNAME", "monitor")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("partial SMTP authentication was accepted")
	}
}

func TestLoadConfigRejectsInvalidEmailLeaseAndTLSMode(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_TIMEOUT", "30s")
	t.Setenv("MONITOR_EMAIL_LEASE_DURATION", "10s")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("email lease shorter than SMTP timeout was accepted")
	}

	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_TLS_MODE", "opportunistic")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("unsupported SMTP TLS mode was accepted")
	}
}

func setMonitorConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MONITOR_WEBHOOK_DISPATCH_INTERVAL", "MONITOR_WEBHOOK_HTTP_TIMEOUT", "MONITOR_WEBHOOK_LEASE_DURATION",
		"MONITOR_WEBHOOK_MAX_ATTEMPTS", "MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF", "MONITOR_WEBHOOK_RETRY_MAX_BACKOFF",
		"MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS", "MONITOR_CONSOLE_BASE_URL",
		"MONITOR_EMAIL_DISPATCH_INTERVAL", "MONITOR_EMAIL_SMTP_TIMEOUT", "MONITOR_EMAIL_LEASE_DURATION",
		"MONITOR_EMAIL_MAX_ATTEMPTS", "MONITOR_EMAIL_RETRY_INITIAL_BACKOFF", "MONITOR_EMAIL_RETRY_MAX_BACKOFF",
		"MONITOR_EMAIL_SMTP_HOST", "MONITOR_EMAIL_SMTP_PORT", "MONITOR_EMAIL_SMTP_USERNAME",
		"MONITOR_EMAIL_SMTP_PASSWORD", "MONITOR_EMAIL_SMTP_TLS_MODE", "MONITOR_EMAIL_FROM_ADDRESS",
		"MONITOR_EMAIL_FROM_NAME",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}
