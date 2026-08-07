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

func TestLoadConfigIgnoresMigratedRuntimePolicyEnvironment(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_WEBHOOK_HTTP_TIMEOUT", "30s")
	t.Setenv("MONITOR_WEBHOOK_LEASE_DURATION", "10s")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.WebhookHTTPTimeout != 10*time.Second || config.WebhookLeaseDuration != 30*time.Second {
		t.Fatalf("migrated environment changed config: %s/%s", config.WebhookHTTPTimeout, config.WebhookLeaseDuration)
	}
}

func TestLoadConfigIgnoresMigratedRetryEnvironment(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_WEBHOOK_RETRY_INITIAL_BACKOFF", "2m")
	t.Setenv("MONITOR_WEBHOOK_RETRY_MAX_BACKOFF", "1m")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.WebhookRetryInitial != 5*time.Second || config.WebhookRetryMax != 5*time.Minute {
		t.Fatalf("migrated environment changed retry config: %s/%s", config.WebhookRetryInitial, config.WebhookRetryMax)
	}
}

func TestLoadConfigDoesNotReadSMTPRelayEnvironment(t *testing.T) {
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
	if config.EmailSMTPConfigured() || config.EmailSMTPPort != 587 || config.EmailSMTPTLSMode != "starttls" {
		t.Fatalf("email config = %#v", config)
	}
}

func TestLoadConfigDoesNotReadPartialSMTPEnvironment(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_USERNAME", "monitor")
	config, err := LoadConfig()
	if err != nil || config.EmailSMTPUsername != "" || config.EmailSMTPPassword != "" {
		t.Fatalf("SMTP environment was read: config=%#v err=%v", config, err)
	}
}

func TestLoadConfigDoesNotReadSMTPRelayTLSMode(t *testing.T) {
	setMonitorConfigEnvironment(t)
	t.Setenv("MONITOR_EMAIL_SMTP_TLS_MODE", "opportunistic")
	config, err := LoadConfig()
	if err != nil || config.EmailSMTPTLSMode != "starttls" {
		t.Fatalf("SMTP TLS environment was read: config=%#v err=%v", config, err)
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
