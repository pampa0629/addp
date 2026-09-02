package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	secretcipher "github.com/addp/common/secretcipher"
	monitorModels "github.com/addp/monitor/internal/models"
)

var ErrWebhookTargetRejected = errors.New("webhook target is not allowed")

type WebhookSendResult struct {
	HTTPStatus int
}

type WebhookSender interface {
	Send(ctx context.Context, delivery monitorModels.WebhookDelivery, secret string, now time.Time) (WebhookSendResult, error)
}

type HTTPWebhookSender struct {
	client       *http.Client
	allowPrivate bool
}

func NewHTTPWebhookSender(timeout time.Duration, allowPrivate bool) *HTTPWebhookSender {
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            secureWebhookDialContext(allowPrivate),
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 64 << 10,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
	}
	sender := &HTTPWebhookSender{allowPrivate: allowPrivate}
	sender.client = &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("webhook redirects are not allowed")
		},
	}
	return sender
}

func (s *HTTPWebhookSender) Send(ctx context.Context, delivery monitorModels.WebhookDelivery, secret string, now time.Time) (WebhookSendResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.client.Timeout)
	defer cancel()
	if err := ValidateWebhookURL(ctx, delivery.RequestURL, s.allowPrivate); err != nil {
		return WebhookSendResult{}, err
	}
	body, err := json.Marshal(delivery.Payload)
	if err != nil {
		return WebhookSendResult{}, fmt.Errorf("marshal webhook payload: %w", err)
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := signWebhookPayload(secret, timestamp, body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.RequestURL, bytes.NewReader(body))
	if err != nil {
		return WebhookSendResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ADDP-Monitor-Webhook/1.0")
	request.Header.Set("X-ADDP-Webhook-ID", delivery.DeliveryID)
	request.Header.Set("X-ADDP-Webhook-Timestamp", timestamp)
	request.Header.Set("X-ADDP-Webhook-Signature", "v1="+signature)

	response, err := s.client.Do(request)
	if err != nil {
		return WebhookSendResult{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	result := WebhookSendResult{HTTPStatus: response.StatusCode}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("webhook returned HTTP %d", response.StatusCode)
	}
	return result, nil
}

func DecryptWebhookSecret(ciphertext string, encryptionKey []byte) (string, error) {
	if ciphertext == "" {
		return "", errors.New("webhook secret is unavailable")
	}
	return secretcipher.Decrypt(ciphertext, encryptionKey)
}

func signWebhookPayload(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func ValidateWebhookURL(ctx context.Context, rawURL string, allowPrivate bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrWebhookTargetRejected
	}
	if parsed.Scheme != "https" && !(allowPrivate && parsed.Scheme == "http") {
		return ErrWebhookTargetRejected
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "" || hostname == "localhost" {
		if allowPrivate && hostname == "localhost" {
			return nil
		}
		return ErrWebhookTargetRejected
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil || len(addresses) == 0 {
		return ErrWebhookTargetRejected
	}
	if allowPrivate {
		return nil
	}
	for _, address := range addresses {
		if webhookIPForbidden(address.IP) {
			return ErrWebhookTargetRejected
		}
	}
	return nil
}

func secureWebhookDialContext(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrWebhookTargetRejected
		}
		for _, resolved := range addresses {
			if !allowPrivate && webhookIPForbidden(resolved.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		}
		return nil, ErrWebhookTargetRejected
	}
}

func webhookIPForbidden(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}
