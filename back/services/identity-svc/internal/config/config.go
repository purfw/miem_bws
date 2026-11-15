package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        int
	DatabaseURL string
	RedisURL    string
	NATSURL     string

	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	JWTIssuer         string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration

	ServiceTokenSecret string

	LoginRateLimit  int
	LoginRateWindow time.Duration

	CookieSecure          bool
	PwnedPasswordsURL     string
	PwnedPasswordsTimeout time.Duration

	LogLevel string
	BaseURL  string

	ORCIDClientID     string
	ORCIDClientSecret string
	ORCIDAuthorizeURL string
	ORCIDTokenURL     string
	ORCIDUserInfoURL  string
	ORCIDAPIURL       string
}

func Load() (*Config, error) {
	c := &Config{
		Port:                  envInt("PORT", 8081),
		DatabaseURL:           must("DATABASE_URL"),
		RedisURL:              must("REDIS_URL"),
		NATSURL:               must("NATS_URL"),
		JWTPrivateKeyPath:     must("JWT_PRIVATE_KEY_PATH"),
		JWTPublicKeyPath:      must("JWT_PUBLIC_KEY_PATH"),
		JWTIssuer:             env("JWT_ISSUER", "confhub"),
		AccessTokenTTL:        envDur("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:       envDur("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		ServiceTokenSecret:    must("SERVICE_TOKEN_SECRET"),
		LoginRateLimit:        envInt("LOGIN_RATE_LIMIT", 10),
		LoginRateWindow:       envDur("LOGIN_RATE_WINDOW", time.Minute),
		CookieSecure:          env("COOKIE_SECURE", "false") == "true",
		PwnedPasswordsURL:     env("PWNED_PASSWORDS_URL", "https://api.pwnedpasswords.com"),
		PwnedPasswordsTimeout: envDur("PWNED_PASSWORDS_TIMEOUT", 5*time.Second),
		LogLevel:              env("LOG_LEVEL", "info"),
		BaseURL:               env("BASE_URL", "http://localhost"),
		ORCIDClientID:         env("ORCID_CLIENT_ID", ""),
		ORCIDClientSecret:     env("ORCID_CLIENT_SECRET", ""),
		ORCIDAuthorizeURL:     env("ORCID_AUTHORIZE_URL", "https://orcid.org/oauth/authorize"),
		ORCIDTokenURL:         env("ORCID_TOKEN_URL", "https://orcid.org/oauth/token"),
		ORCIDUserInfoURL:      env("ORCID_USERINFO_URL", "https://orcid.org/oauth/userinfo"),
		ORCIDAPIURL:           env("ORCID_API_URL", "https://pub.orcid.org/v3.0"),
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
