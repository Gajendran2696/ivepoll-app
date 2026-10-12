// Package middleware holds cross-cutting HTTP concerns: auth, rate limiting, hardening.
package middleware

import (
	"net/http"
	"strings"
	"time"

	"livepoll/realtime"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const UserIDKey = "userID"

// Auth requires a valid "Authorization: Bearer <jwt>" header.
func Auth(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		raw, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		claims := &jwt.RegisteredClaims{}
		tok, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return secret, nil },
			jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.Subject == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session, please log in again"})
			return
		}
		c.Set(UserIDKey, claims.Subject)
		c.Next()
	}
}

// OptionalAuth identifies the user when a valid token is sent, but never rejects the request.
// Used on public routes that show extra controls to the poll owner.
func OptionalAuth(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok && raw != "" {
			claims := &jwt.RegisteredClaims{}
			tok, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return secret, nil },
				jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
			if err == nil && tok.Valid && claims.Subject != "" {
				c.Set(UserIDKey, claims.Subject)
			}
		}
		c.Next()
	}
}

// RateLimit limits requests per client IP using a Redis counter shared by all instances.
// If Redis is unavailable it fails open (availability over strictness) but the error is not hidden from logs.
func RateLimit(b *realtime.Broker, name string, limit int64, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := b.Allow(c.Request.Context(), name+":"+c.ClientIP(), limit, window)
		if err == nil && !ok {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, slow down"})
			return
		}
		c.Next()
	}
}

// BodyLimit caps JSON request bodies so a client cannot stream megabytes into the server.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}

// SecurityHeaders adds a few cheap, safe defaults.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
