package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port               int
	DatabaseURL        string
	NATSURL            string
	RedisURL           string
	JWTPublicKey       string
	JWTIssuer          string
	SMTPHost           string
	SMTPPort           int
	SMTPUser           string
	SMTPPassword       string
	SMTPFrom           string
	SMTPTLSMode        string
	SupportEmail       string
	BaseURL            string
	IdentitySvcURL     string
	ConferenceSvcURL   string
	ServiceTokenSecret string
	LogLevel           string
}

func Load() *Config {
	return &Config{
		Port:               envInt("PORT", 8084),
		DatabaseURL:        must("DATABASE_URL"),
		NATSURL:            must("NATS_URL"),
		RedisURL:           must("REDIS_URL"),
		JWTPublicKey:       must("JWT_PUBLIC_KEY_PATH"),
		JWTIssuer:          env("JWT_ISSUER", "confhub"),
		SMTPHost:           env("SMTP_HOST", "mailpit"),
		SMTPPort:           envInt("SMTP_PORT", 1025),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:           env("SMTP_FROM", "noreply@confhub.example.com"),
		SMTPTLSMode:        os.Getenv("SMTP_TLS_MODE"),
		SupportEmail:       env("SUPPORT_EMAIL", "support@confhub.example.com"),
		BaseURL:            env("BASE_URL", "http://localhost"),
		IdentitySvcURL:     env("IDENTITY_SVC_URL", "http://identity-svc:8081"),
		ConferenceSvcURL:   env("CONFERENCE_SVC_URL", "http://conference-svc:8082"),
		ServiceTokenSecret: must("SERVICE_TOKEN_SECRET"),
		LogLevel:           env("LOG_LEVEL", "info"),
	}
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
