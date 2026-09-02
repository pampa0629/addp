package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		return "", fmt.Errorf("query token encryption key is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	aead, err := c.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, body, []byte(purpose))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *queryTokenCodec) decode(purpose, token string, target interface{}) error {
	if len(c.key) == 0 {
		return errors.New("query token encryption key is not configured")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return err
	}
	aead, err := c.aead()
	if err != nil {
		return err
	}
	if len(encoded) < aead.NonceSize() {
		return errors.New("invalid token shape")
	}
	nonce, ciphertext := encoded[:aead.NonceSize()], encoded[aead.NonceSize():]
	body, err := aead.Open(nil, nonce, ciphertext, []byte(purpose))
	if err != nil {
		return errors.New("invalid token ciphertext")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func (c *queryTokenCodec) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
