// Package storage wraps MinIO (S3) for application file uploads.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	mc         *minio.Client
	presigner  *minio.Client
	bucket     string
	presignTTL time.Duration
}

type Config struct {
	Endpoint       string
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	PublicUseSSL   bool
	Bucket         string
	PresignTTL     time.Duration
}

func New(cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio init: %w", err)
	}
	presigner := mc
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		presigner, err = minio.New(cfg.PublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.PublicUseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("minio public signer init: %w", err)
		}
	}
	return &Client{mc: mc, presigner: presigner, bucket: cfg.Bucket, presignTTL: cfg.PresignTTL}, nil
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	ok, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("bucket exists: %w", err)
	}
	if !ok {
		if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("make bucket: %w", err)
		}
	}
	return nil
}

func (c *Client) Put(ctx context.Context, ext, mime string, size int64, body io.Reader) (string, error) {
	key := NewObjectKey(ext)
	return key, c.PutAt(ctx, key, mime, size, body)
}

func NewObjectKey(ext string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("%04d/%02d/%s%s", now.Year(), now.Month(), uuid.NewString(), ext)
}

func (c *Client) PutAt(ctx context.Context, key, mime string, size int64, body io.Reader) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, body, size, minio.PutObjectOptions{
		ContentType: mime,
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// Presign uses attachment disposition to prevent inline rendering.
func (c *Client) Presign(ctx context.Context, key, downloadName string) (string, error) {
	headers := url.Values{}
	if downloadName != "" {
		headers.Set("response-content-disposition",
			fmt.Sprintf(`attachment; filename="%s"`, downloadName))
	}
	u, err := c.presigner.PresignedGetObject(ctx, c.bucket, key, c.presignTTL, headers)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

func (c *Client) Bucket() string { return c.bucket }
