// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port           string
	MongoURI       string
	MongoDB        string
	RedisURL       string
	JWTSecret      []byte
	TokenTTL       time.Duration
	AllowedOrigins []string // CORS + WebSocket origins. Empty = same-origin only.
	TrustedProxies []string // Proxies whose X-Forwarded-For header we believe.
	StaticDir      string   // Optional: serve the built React app from this folder.
}

func Load() (*Config, error) {
	c := &Config{
		Port:      get("PORT", "8080"),
		MongoURI:  os.Getenv("MONGODB_URI"),
		MongoDB:   get("MONGODB_DB", "livepoll"),
		RedisURL:  os.Getenv("REDIS_URL"),
		StaticDir: os.Getenv("STATIC_DIR"),
		TokenTTL:  72 * time.Hour,
	}
	if c.MongoURI == "" {
		return nil, errors.New("MONGODB_URI is required")
	}
	if c.RedisURL == "" {
		return nil, errors.New("REDIS_URL is required (e.g. redis://localhost:6379)")
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET is required and must be at least 32 characters")
	}
	c.JWTSecret = []byte(secret)
	c.AllowedOrigins = split(os.Getenv("ALLOWED_ORIGINS"))
	c.TrustedProxies = split(get("TRUSTED_PROXIES",
		"10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1,::1"))
	return c, nil
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
