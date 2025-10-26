package utils

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	TenantID uint   `json:"tenant_id"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, username string, tenantID uint, secret string, expireMinutes int) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireMinutes) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析并验证 JWT Token
// P0-2: 验证签名算法，防止算法降级攻击（CVE-2015-9235）
func ParseToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// CRITICAL: 验证签名算法，防止攻击者将算法改为 "none" 绕过签名验证
		// 参考：https://nvd.nist.gov/vuln/detail/CVE-2015-9235

		// 检查是否为 HMAC 签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// 进一步验证必须是 HS256
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing algorithm: %v, expected: HS256", token.Method.Alg())
		}

		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}