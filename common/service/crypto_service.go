package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// CryptoService 加密服务
// 统一管理敏感字段的加密/解密
type CryptoService struct {
	encryptionKey []byte
	// 敏感字段列表
	sensitiveFields map[string]bool
}

// NewCryptoService 创建加密服务
func NewCryptoService(encryptionKey []byte) *CryptoService {
	return &CryptoService{
		encryptionKey: encryptionKey,
		sensitiveFields: map[string]bool{
			"password":    true,
			"access_key":  true,
			"secret_key":  true,
			"token":       true,
			"api_key":     true,
			"private_key": true,
		},
	}
}

// IsSensitiveField 判断是否为敏感字段
func (s *CryptoService) IsSensitiveField(fieldName string) bool {
	return s.sensitiveFields[fieldName]
}

// EncryptSensitiveFields 加密敏感字段
// 遍历 map，加密所有敏感字段
func (s *CryptoService) EncryptSensitiveFields(data map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		if s.IsSensitiveField(key) {
			// 加密敏感字段
			strVal, ok := value.(string)
			if !ok || strVal == "" {
				result[key] = value
				continue
			}

			encrypted, err := s.Encrypt(strVal)
			if err != nil {
				return nil, err
			}
			result[key] = encrypted
		} else {
			// 非敏感字段直接复制
			result[key] = value
		}
	}

	return result, nil
}

// DecryptSensitiveFields 解密敏感字段
// 遍历 map，解密所有敏感字段
func (s *CryptoService) DecryptSensitiveFields(data map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		if s.IsSensitiveField(key) {
			// 解密敏感字段
			strVal, ok := value.(string)
			if !ok || strVal == "" {
				result[key] = value
				continue
			}

			decrypted, err := s.Decrypt(strVal)
			if err != nil {
				// 解密失败可能是因为字段未加密，保持原值
				result[key] = value
				continue
			}
			result[key] = decrypted
		} else {
			// 非敏感字段直接复制
			result[key] = value
		}
	}

	return result, nil
}

// Encrypt 使用 AES-256-GCM 加密数据
func (s *CryptoService) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 使用 AES-256-GCM 解密数据
func (s *CryptoService) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("密文长度不足")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// MaskSensitiveData 脱敏处理（用于日志）
// 只显示前2位和后2位，中间用 **** 代替
func (s *CryptoService) MaskSensitiveData(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + "****" + value[len(value)-2:]
}
