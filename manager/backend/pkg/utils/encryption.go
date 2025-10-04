package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
)

// Decrypt 使用 AES-256-GCM 解密字符串
// key 必须是 32 字节 (256位)
func Decrypt(ciphertext string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("decryption key must be 32 bytes")
	}

	// Base64 解码
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	// 提取 nonce 和实际密文
	nonce, cipherData := data[:nonceSize], data[nonceSize:]

	// 解密并验证认证标签
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// DecodeKey 从 Base64 字符串解码密钥
func DecodeKey(encodedKey string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encodedKey)
}
