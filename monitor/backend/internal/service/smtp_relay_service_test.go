package service

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	secretcipher "github.com/addp/common/secretcipher"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSMTPRelayTestService(t *testing.T) (*SMTPRelayService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:monitor-smtp-relay-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS monitor").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SMTPRelay{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	return NewSMTPRelayService(repository.NewSMTPRelayRepository(db), []byte("0123456789abcdef0123456789abcdef")), db
}

func TestSMTPRelayCredentialIsEncryptedAndMasked(t *testing.T) {
	svc, db := newSMTPRelayTestService(t)
	status, err := svc.SetCredential(context.Background(), "smtp-secret", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Version != 1 {
		t.Fatalf("status = %#v", status)
	}
	var stored models.SMTPRelay
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CredentialCiphertext == "" || stored.CredentialCiphertext == "smtp-secret" {
		t.Fatalf("credential stored in plaintext: %q", stored.CredentialCiphertext)
	}
	plaintext, err := secretcipher.Decrypt(stored.CredentialCiphertext, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil || plaintext != "smtp-secret" {
		t.Fatalf("decrypt credential = %q, %v", plaintext, err)
	}
	value, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) == "" || bytes.Contains(encoded, []byte("smtp-secret")) {
		t.Fatalf("response exposed credential: %s", encoded)
	}
}

func TestSMTPRelayConfigurationVersionAndApply(t *testing.T) {
	svc, _ := newSMTPRelayTestService(t)
	if _, err := svc.SetCredential(context.Background(), "smtp-secret", 7); err != nil {
		t.Fatal(err)
	}
	value, err := svc.Update(context.Background(), UpdateSMTPRelayInput{Version: 0, Enabled: true, Host: "smtp.example.com", Port: 587, TLSMode: EmailTLSModeSTARTTLS, FromAddress: "monitor@example.com", FromName: "Monitor", Username: "monitor"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || !value.Credential.Configured {
		t.Fatalf("value = %#v", value)
	}
	if _, err := svc.Update(context.Background(), UpdateSMTPRelayInput{Version: 0, Enabled: true, Host: "smtp.example.com", Port: 587, TLSMode: EmailTLSModeSTARTTLS, FromAddress: "monitor@example.com"}, 8); err == nil {
		t.Fatal("stale configuration update was accepted")
	}
	cfg := &config.Config{}
	if err := svc.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.EmailSMTPHost != "smtp.example.com" || cfg.EmailSMTPPassword != "smtp-secret" || cfg.EmailFromAddress != "monitor@example.com" {
		t.Fatalf("applied config = %#v", cfg)
	}
}
