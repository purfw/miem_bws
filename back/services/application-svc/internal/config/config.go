package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                      int
	DatabaseURL               string
	RedisURL                  string
	NATSURL                   string
	JWTPublicKeyPath          string
	JWTIssuer                 string
	ServiceTokenSecret        string
	ConferenceSvcURL          string
	IdentitySvcURL            string
	MinIOEndpoint             string
	MinIOPublicEndpoint       string
	MinIOAccessKey            string
	MinIOSecretKey            string
	MinIOBucket               string
	MinIOUseSSL               bool
	MinIOPublicUseSSL         bool
	PresignTTL                time.Duration
	MaxFileSizeBytes          int64
	ConferenceCacheTTL        time.Duration
	LogLevel                  string
	BaseURL                   string
	PDFFontPath               string
	ORCIDPublicationThreshold int
}

func Load() (*Config, error) {
	c := &Config{
		Port:                      envInt("PORT", 8083),
		DatabaseURL:               must("DATABASE_URL"),
		RedisURL:                  must("REDIS_URL"),
		NATSURL:                   must("NATS_URL"),
		JWTPublicKeyPath:          must("JWT_PUBLIC_KEY_PATH"),
		JWTIssuer:                 env("JWT_ISSUER", "confhub"),
		ServiceTokenSecret:        must("SERVICE_TOKEN_SECRET"),
		ConferenceSvcURL:          must("CONFERENCE_SVC_URL"),
		IdentitySvcURL:            env("IDENTITY_SVC_URL", "http://identity-svc:8081"),
		MinIOEndpoint:             must("MINIO_ENDPOINT"),
		MinIOPublicEndpoint:       env("MINIO_PUBLIC_ENDPOINT", ""),
		MinIOAccessKey:            must("MINIO_ACCESS_KEY"),
		MinIOSecretKey:            must("MINIO_SECRET_KEY"),
		MinIOBucket:               env("MINIO_BUCKET", "application-files"),
		MinIOUseSSL:               env("MINIO_USE_SSL", "false") == "true",
		MinIOPublicUseSSL:         env("MINIO_PUBLIC_USE_SSL", "false") == "true",
		PresignTTL:                envDur("PRESIGN_TTL", 5*time.Minute),
		MaxFileSizeBytes:          int64(envInt("MAX_FILE_SIZE_BYTES", 20*1024*1024)),
		ConferenceCacheTTL:        envDur("CONFERENCE_CACHE_TTL", time.Minute),
		LogLevel:                  env("LOG_LEVEL", "info"),
		BaseURL:                   must("BASE_URL"),
		PDFFontPath:               env("PDF_FONT_PATH", "/usr/share/fonts/dejavu/DejaVuSans.ttf"),
		ORCIDPublicationThreshold: envInt("ORCID_PUBLICATION_THRESHOLD", 5),
	}
	if c.MinIOPublicEndpoint == "" {
		publicURL, err := url.Parse(c.BaseURL)
		if err != nil || publicURL.Host == "" {
			return nil, fmt.Errorf("BASE_URL must be an absolute URL")
		}
		c.MinIOPublicEndpoint = publicURL.Host
		c.MinIOPublicUseSSL = publicURL.Scheme == "https"
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
