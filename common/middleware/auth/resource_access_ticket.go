package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	BrowserResourceAccessTicketCookieName = "addp_resource_access_ticket"
	AuthTypeResourceAccessTicket          = "resource_access_ticket"
	BrowserResourceAccessScope            = "resource:read"
)

type ResourceAccessRequestMatcher func(*gin.Context) bool

// BrowserResourceAccessMiddleware authenticates explicitly allowed GET/HEAD resource routes
// with the owner-scoped HttpOnly browser resource ticket cookie.
func BrowserResourceAccessMiddleware(
	systemURL string,
	redisClient *redis.Client,
	owner string,
	allowed ResourceAccessRequestMatcher,
) gin.HandlerFunc {
	baseURL := strings.TrimSuffix(systemURL, "/")
	httpClient := &http.Client{Timeout: 10 * time.Second}

	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader("Authorization")) != "" || !isAllowedResourceTicketRequest(c, allowed) {
			c.Next()
			return
		}

		ticket, err := c.Cookie(BrowserResourceAccessTicketCookieName)
		if err != nil || strings.TrimSpace(ticket) == "" {
			c.Next()
			return
		}

		authorizationContext, statusCode, err := resolveResourceAccessTicketContext(
			c.Request.Context(),
			baseURL+"/api/v1/system/auth/context",
			httpClient,
			redisClient,
			ticket,
		)
		if err != nil {
			if statusCode == 0 {
				statusCode = http.StatusUnauthorized
			}
			c.JSON(statusCode, gin.H{"error": "invalid browser resource access ticket"})
			c.Abort()
			return
		}
		if !validResourceAccessContext(authorizationContext, owner) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid browser resource access ticket"})
			c.Abort()
			return
		}

		setAuthorizationContext(c, authorizationContext)
		c.Next()
	}
}

func isAllowedResourceTicketRequest(c *gin.Context, allowed ResourceAccessRequestMatcher) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	return allowed != nil && allowed(c)
}

func resolveResourceAccessTicketContext(
	ctx context.Context,
	endpoint string,
	httpClient *http.Client,
	redisClient *redis.Client,
	ticket string,
) (AuthorizationContext, int, error) {
	cacheKey := generateTokenCacheKey(ticket)
	if redisClient != nil {
		cachedData, err := redisClient.Get(ctx, cacheKey).Result()
		if err == nil && cachedData != "" {
			var cached AuthorizationContext
			if json.Unmarshal([]byte(cachedData), &cached) == nil && cached.ExpiresAt.After(time.Now()) {
				return cached, http.StatusOK, nil
			}
			_ = redisClient.Del(ctx, cacheKey).Err()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return AuthorizationContext{}, http.StatusInternalServerError, err
	}
	req.Header.Set("Authorization", "Bearer "+ticket)
	resp, err := httpClient.Do(req)
	if err != nil {
		return AuthorizationContext{}, http.StatusServiceUnavailable, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		if resp.StatusCode >= http.StatusInternalServerError {
			return AuthorizationContext{}, http.StatusServiceUnavailable, ErrAuthorizationContextUnavailable
		}
		return AuthorizationContext{}, http.StatusUnauthorized, ErrAuthorizationContextUnavailable
	}

	var authorizationContext AuthorizationContext
	if err := json.NewDecoder(resp.Body).Decode(&authorizationContext); err != nil {
		return AuthorizationContext{}, http.StatusServiceUnavailable, err
	}
	if redisClient != nil {
		remainingTTL := time.Until(authorizationContext.ExpiresAt)
		if remainingTTL > 0 {
			if encoded, err := json.Marshal(authorizationContext); err == nil {
				_ = redisClient.Set(ctx, cacheKey, encoded, remainingTTL).Err()
			}
		}
	}
	return authorizationContext, http.StatusOK, nil
}

func validResourceAccessContext(authorizationContext AuthorizationContext, owner string) bool {
	return authorizationContext.SubjectType == "user" &&
		authorizationContext.UserID != 0 &&
		authorizationContext.AuthType == AuthTypeResourceAccessTicket &&
		containsString(authorizationContext.Audiences, owner) &&
		containsString(authorizationContext.Scopes, BrowserResourceAccessScope) &&
		authorizationContext.ExpiresAt.After(time.Now())
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
