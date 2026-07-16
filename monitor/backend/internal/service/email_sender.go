package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	mail "github.com/wneessen/go-mail"
)

const (
	EmailTLSModeSTARTTLS = "starttls"
	EmailTLSModeTLS      = "tls"
)

type EmailSender interface {
	Send(ctx context.Context, delivery monitorModels.EmailDelivery, now time.Time) error
}

type SMTPEmailSenderConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	TLSMode     string
	FromAddress string
	FromName    string
	Timeout     time.Duration
}

type smtpMailClient interface {
	DialAndSendWithContext(context.Context, ...*mail.Msg) error
}

type SMTPEmailSender struct {
	client      smtpMailClient
	fromAddress string
	fromName    string
}

func NewSMTPEmailSender(config SMTPEmailSenderConfig) (*SMTPEmailSender, error) {
	options := []mail.Option{
		mail.WithPort(config.Port),
		mail.WithTimeout(config.Timeout),
		mail.WithTLSConfig(&tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}),
	}
	switch config.TLSMode {
	case EmailTLSModeSTARTTLS:
		options = append(options, mail.WithTLSPolicy(mail.TLSMandatory))
	case EmailTLSModeTLS:
		options = append(options, mail.WithSSL(), mail.WithTLSPolicy(mail.NoTLS))
	default:
		return nil, fmt.Errorf("unsupported SMTP TLS mode %q", config.TLSMode)
	}
	if config.Username != "" {
		options = append(options,
			mail.WithUsername(config.Username),
			mail.WithPassword(config.Password),
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
		)
	}
	client, err := mail.NewClient(config.Host, options...)
	if err != nil {
		return nil, fmt.Errorf("create SMTP client: %w", err)
	}
	return &SMTPEmailSender{
		client: client, fromAddress: config.FromAddress, fromName: strings.TrimSpace(config.FromName),
	}, nil
}

func (s *SMTPEmailSender) Send(ctx context.Context, delivery monitorModels.EmailDelivery, now time.Time) error {
	message := mail.NewMsg()
	var err error
	if s.fromName == "" {
		err = message.From(s.fromAddress)
	} else {
		err = message.FromFormat(s.fromName, s.fromAddress)
	}
	if err != nil {
		return fmt.Errorf("set email sender: %w", err)
	}
	if err := message.To([]string(delivery.Recipients)...); err != nil {
		return fmt.Errorf("set email recipients: %w", err)
	}
	message.Subject(delivery.Subject)
	message.SetMessageIDWithValue(delivery.DeliveryID + "@monitor.addp.local")
	message.SetDateWithValue(now)
	message.SetBulk()
	message.SetBodyString(mail.TypeTextPlain, delivery.TextBody)
	message.AddAlternativeString(mail.TypeTextHTML, delivery.HTMLBody)
	if err := s.client.DialAndSendWithContext(ctx, message); err != nil {
		return fmt.Errorf("SMTP send failed: %w", err)
	}
	return nil
}
