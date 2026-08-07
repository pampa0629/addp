package service

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	commonutils "github.com/addp/common/utils"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
)

type SMTPRelayCredentialStatus struct {
	Configured bool   `json:"configured"`
	Version    uint64 `json:"version"`
}

type SMTPRelayResponse struct {
	Version        uint64                    `json:"version"`
	Enabled        bool                      `json:"enabled"`
	Host           string                    `json:"host"`
	Port           int                       `json:"port"`
	TLSMode        string                    `json:"tls_mode"`
	FromAddress    string                    `json:"from_address"`
	FromName       string                    `json:"from_name"`
	Username       string                    `json:"username"`
	Credential     SMTPRelayCredentialStatus `json:"credential"`
	PendingRestart bool                      `json:"pending_restart"`
}

type UpdateSMTPRelayInput struct {
	Version     uint64 `json:"version"`
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port" binding:"required"`
	TLSMode     string `json:"tls_mode" binding:"required"`
	FromAddress string `json:"from_address"`
	FromName    string `json:"from_name"`
	Username    string `json:"username"`
}

type SMTPRelayService struct {
	repo          *repository.SMTPRelayRepository
	encryptionKey []byte
}

func NewSMTPRelayService(repo *repository.SMTPRelayRepository, encryptionKey []byte) *SMTPRelayService {
	return &SMTPRelayService{repo: repo, encryptionKey: append([]byte(nil), encryptionKey...)}
}

func (s *SMTPRelayService) Get(ctx context.Context) (SMTPRelayResponse, error) {
	value, err := s.repo.Get(ctx)
	if err != nil {
		return SMTPRelayResponse{}, err
	}
	if value == nil {
		value = defaultSMTPRelay()
	}
	return smtpRelayResponse(value, value.Version > 0), nil
}

func (s *SMTPRelayService) Update(ctx context.Context, input UpdateSMTPRelayInput, updatedBy uint) (SMTPRelayResponse, error) {
	value := &models.SMTPRelay{Enabled: input.Enabled, Host: strings.TrimSpace(input.Host), Port: input.Port, TLSMode: strings.TrimSpace(input.TLSMode), FromAddress: strings.TrimSpace(input.FromAddress), FromName: strings.TrimSpace(input.FromName), Username: strings.TrimSpace(input.Username), UpdatedBy: updatedBy}
	if err := validateSMTPRelay(value); err != nil {
		return SMTPRelayResponse{}, err
	}
	if err := s.repo.Save(ctx, value, input.Version); err != nil {
		return SMTPRelayResponse{}, err
	}
	return smtpRelayResponse(value, true), nil
}

func (s *SMTPRelayService) SetCredential(ctx context.Context, credential string, updatedBy uint) (SMTPRelayCredentialStatus, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return SMTPRelayCredentialStatus{}, fmt.Errorf("SMTP credential is required")
	}
	ciphertext, err := commonutils.Encrypt(credential, s.encryptionKey)
	if err != nil {
		return SMTPRelayCredentialStatus{}, err
	}
	value, err := s.repo.RotateCredential(ctx, ciphertext, updatedBy)
	if err != nil {
		return SMTPRelayCredentialStatus{}, err
	}
	return SMTPRelayCredentialStatus{Configured: true, Version: value.CredentialVersion}, nil
}

func (s *SMTPRelayService) DeleteCredential(ctx context.Context, updatedBy uint) (SMTPRelayCredentialStatus, error) {
	value, err := s.repo.RotateCredential(ctx, "", updatedBy)
	if err != nil {
		return SMTPRelayCredentialStatus{}, err
	}
	return SMTPRelayCredentialStatus{Configured: false, Version: value.CredentialVersion}, nil
}

func (s *SMTPRelayService) Apply(ctx context.Context, cfg *config.Config) error {
	value, err := s.repo.Get(ctx)
	if err != nil || value == nil || !value.Enabled {
		return err
	}
	if err := validateSMTPRelay(value); err != nil {
		return fmt.Errorf("stored SMTP relay is invalid: %w", err)
	}
	password := ""
	if value.CredentialCiphertext != "" {
		password, err = commonutils.Decrypt(value.CredentialCiphertext, s.encryptionKey)
		if err != nil {
			return fmt.Errorf("decrypt SMTP credential: %w", err)
		}
	}
	if value.Username != "" && password == "" {
		return fmt.Errorf("enabled authenticated SMTP relay has no configured credential")
	}
	cfg.EmailSMTPHost, cfg.EmailSMTPPort = value.Host, value.Port
	cfg.EmailSMTPUsername, cfg.EmailSMTPPassword = value.Username, password
	cfg.EmailSMTPTLSMode, cfg.EmailFromAddress, cfg.EmailFromName = value.TLSMode, value.FromAddress, value.FromName
	return nil
}

func defaultSMTPRelay() *models.SMTPRelay {
	return &models.SMTPRelay{Port: 587, TLSMode: EmailTLSModeSTARTTLS, FromName: "ADDP Monitor"}
}

func validateSMTPRelay(value *models.SMTPRelay) error {
	if value.Port < 1 || value.Port > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	if value.TLSMode != EmailTLSModeSTARTTLS && value.TLSMode != EmailTLSModeTLS {
		return fmt.Errorf("SMTP TLS mode must be starttls or tls")
	}
	if !value.Enabled {
		return nil
	}
	if value.Host == "" {
		return fmt.Errorf("SMTP host is required when relay is enabled")
	}
	address, err := mail.ParseAddress(value.FromAddress)
	if err != nil || address.Name != "" || address.Address != value.FromAddress {
		return fmt.Errorf("valid SMTP from address is required when relay is enabled")
	}
	return nil
}

func smtpRelayResponse(value *models.SMTPRelay, pending bool) SMTPRelayResponse {
	return SMTPRelayResponse{Version: value.Version, Enabled: value.Enabled, Host: value.Host, Port: value.Port, TLSMode: value.TLSMode, FromAddress: value.FromAddress, FromName: value.FromName, Username: value.Username, Credential: SMTPRelayCredentialStatus{Configured: value.CredentialCiphertext != "", Version: value.CredentialVersion}, PendingRestart: pending}
}
