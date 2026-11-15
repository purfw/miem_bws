package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               int
	DatabaseURL        string
	RedisURL           string
	CacheRedisURL      string
	NATSURL            string
	JWTPublicKeyPath   string
	JWTIssuer          string
	ServiceTokenSecret string
	IdentitySvcURL     string
	CacheTTL           time.Duration
	LogLevel           string
	JoinURLKeys        string
	JoinURLKeyVersion  int
	ElasticsearchURL   string
	ElasticsearchIndex string
	SearchRateLimit    int
}

func Load() (*Config, error) {
	legacyJoinKey := must("JOIN_URL_ENCRYPTION_KEY")
	joinKeyring := env("JOIN_URL_ENCRYPTION_KEYS", "")
	if joinKeyring == "" {
		joinKeyring = "1:" + legacyJoinKey
	}
	c := &Config{
		Port:               envInt("PORT", 8082),
		DatabaseURL:        must("DATABASE_URL"),
		RedisURL:           must("REDIS_URL"),
		CacheRedisURL:      env("CACHE_REDIS_URL", must("REDIS_URL")),
		NATSURL:            must("NATS_URL"),
		JWTPublicKeyPath:   must("JWT_PUBLIC_KEY_PATH"),
		JWTIssuer:          env("JWT_ISSUER", "confhub"),
		ServiceTokenSecret: must("SERVICE_TOKEN_SECRET"),
		IdentitySvcURL:     env("IDENTITY_SVC_URL", "http://identity-svc:8081"),
		CacheTTL:           envDur("CACHE_TTL", 60*time.Second),
		LogLevel:           env("LOG_LEVEL", "info"),
		JoinURLKeys:        joinKeyring,
		JoinURLKeyVersion:  envInt("JOIN_URL_ACTIVE_KEY_VERSION", 1),
		ElasticsearchURL:   must("ELASTICSEARCH_URL"),
		ElasticsearchIndex: env("ELASTICSEARCH_INDEX", "confhub-conferences-v1"),
		SearchRateLimit:    envInt("SEARCH_RATE_LIMIT", 200),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL required")
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("env required: " + k)
	}
	return v
}
func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
func envDur(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
