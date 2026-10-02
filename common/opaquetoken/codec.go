// Package opaquetoken owns purpose-isolated authenticated JSON token encoding.
// Domain bindings, versioning, authorization and expiry remain with the caller.
package opaquetoken

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
)

type Codec struct{ key []byte }

// New derives a separate key for the owner's versioned namespace.
func New(encryptionKey []byte, namespace string) *Codec {
	if len(encryptionKey) == 0 || namespace == "" {
		return &Codec{}
	}
	mac := hmac.New(sha256.New, encryptionKey)
	_, _ = mac.Write([]byte(namespace))
	return &Codec{key: mac.Sum(nil)}
}

func (c *Codec) Encode(purpose string, payload interface{}) (string, error) {
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

func (c *Codec) Decode(purpose, token string, target interface{}) error {
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
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return errors.New("invalid token payload")
	}
	return nil
}

func (c *Codec) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
