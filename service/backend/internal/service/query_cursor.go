package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/service/internal/models"
)

const queryTokenVersion = 1

var (
	ErrInvalidQueryCursor = errors.New("invalid query cursor")
	ErrInvalidFeatureID   = errors.New("invalid feature id")
)

type queryCursorPayload struct {
	Version        int                 `json:"v"`
	ServiceID      uint                `json:"service_id"`
	ServiceVersion string              `json:"service_version"`
	QueryHash      string              `json:"query_hash"`
	OrderBy        []models.QueryOrder `json:"order_by"`
	Values         []interface{}       `json:"values"`
}

type featureIDPayload struct {
	Version        int           `json:"v"`
	ServiceID      uint          `json:"service_id"`
	ServiceVersion string        `json:"service_version"`
	Fields         []string      `json:"fields"`
	Values         []interface{} `json:"values"`
}

type queryTokenCodec struct {
	key []byte
}

func newQueryTokenCodec(encryptionKey []byte) *queryTokenCodec {
	if len(encryptionKey) == 0 {
		return &queryTokenCodec{}
	}
	mac := hmac.New(sha256.New, encryptionKey)
	_, _ = mac.Write([]byte("addp/service/query-token/v1"))
	return &queryTokenCodec{key: mac.Sum(nil)}
}

func (c *queryTokenCodec) encodeCursor(payload queryCursorPayload) (string, error) {
	payload.Version = queryTokenVersion
	return c.encode("cursor", payload)
}

func (c *queryTokenCodec) decodeCursor(token string) (*queryCursorPayload, error) {
	var payload queryCursorPayload
	if err := c.decode("cursor", token, &payload); err != nil || payload.Version != queryTokenVersion {
		return nil, ErrInvalidQueryCursor
	}
	return &payload, nil
}

func (c *queryTokenCodec) encodeFeatureID(payload featureIDPayload) (string, error) {
	payload.Version = queryTokenVersion
	return c.encode("feature", payload)
}

func (c *queryTokenCodec) decodeFeatureID(token string) (*featureIDPayload, error) {
	var payload featureIDPayload
	if err := c.decode("feature", token, &payload); err != nil || payload.Version != queryTokenVersion {
		return nil, ErrInvalidFeatureID
	}
	return &payload, nil
}

func (c *queryTokenCodec) encode(purpose string, payload interface{}) (string, error) {
	if len(c.key) == 0 {
		return "", fmt.Errorf("query token signing key is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := c.signature(purpose, body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (c *queryTokenCodec) decode(purpose, token string, target interface{}) error {
	if len(c.key) == 0 {
		return errors.New("query token signing key is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("invalid token shape")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, c.signature(purpose, body)) {
		return errors.New("invalid token signature")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func (c *queryTokenCodec) signature(purpose string, body []byte) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte(purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}
