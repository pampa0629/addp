package iam

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strconv"
)

const currentMFAKeyVersion = 1

type MFACredentialCipher struct {
	key []byte
}

func NewMFACredentialCipher(key []byte) (*MFACredentialCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("MFA credential encryption key must be 32 bytes")
	}
	return &MFACredentialCipher{key: append([]byte(nil), key...)}, nil
}

func (c *MFACredentialCipher) EncryptTOTPSecret(userID int64, secret string) ([]byte, []byte, int, error) {
	if c == nil || len(c.key) != 32 || userID <= 0 || secret == "" {
		return nil, nil, 0, fmt.Errorf("invalid MFA credential encryption input")
	}
	gcm, err := c.gcm()
	if err != nil {
		return nil, nil, 0, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, 0, fmt.Errorf("generate MFA credential nonce: %w", err)
	}
	version := currentMFAKeyVersion
	ciphertext := gcm.Seal(nil, nonce, []byte(secret), mfaCredentialAAD(userID, version))
	return ciphertext, nonce, version, nil
}

func (c *MFACredentialCipher) DecryptTOTPSecret(credential *MFACredential) (string, error) {
	if c == nil || len(c.key) != 32 || credential == nil || credential.UserID <= 0 ||
		credential.KeyVersion != currentMFAKeyVersion || len(credential.SecretNonce) != 12 ||
		len(credential.SecretCiphertext) < 16 {
		return "", fmt.Errorf("invalid encrypted MFA credential")
	}
	gcm, err := c.gcm()
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(
		nil,
		credential.SecretNonce,
		credential.SecretCiphertext,
		mfaCredentialAAD(credential.UserID, credential.KeyVersion),
	)
	if err != nil {
		return "", fmt.Errorf("decrypt MFA credential: %w", err)
	}
	if len(plaintext) == 0 {
		return "", fmt.Errorf("decrypted MFA credential is empty")
	}
	return string(plaintext), nil
}

func (c *MFACredentialCipher) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("create MFA credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create MFA credential GCM: %w", err)
	}
	return gcm, nil
}

func mfaCredentialAAD(userID int64, keyVersion int) []byte {
	return []byte("addp:iam:mfa:totp:" + strconv.FormatInt(userID, 10) + ":" + strconv.Itoa(keyVersion))
}
