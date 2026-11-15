package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/confhub/shared/httputil"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var hashSlots = make(chan struct{}, 2)

func acquireHashSlot(ctx context.Context) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case hashSlots <- struct{}{}:
		return func() { <-hashSlots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("password hashing busy: %w", httputil.ErrUnavailable)
	}
}

const (
	argTime    uint32 = 3
	argMemory  uint32 = 64 * 1024
	argThreads uint8  = 4
	argKeyLen  uint32 = 32
	argSaltLen uint32 = 16
)

// dummyHash keeps unknown-email logins on the same hashing path.
var dummyHash string

func init() {
	salt := make([]byte, argSaltLen)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte("dummy"), salt, argTime, argMemory, argThreads, argKeyLen)
	dummyHash = encode(salt, key)
}

func encode(salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argMemory, argTime, argThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

func HashPasswordContext(ctx context.Context, plain string) (string, error) {
	release, err := acquireHashSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	salt := make([]byte, argSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argTime, argMemory, argThreads, argKeyLen)
	return encode(salt, key), nil
}

func VerifyPassword(plain, stored string) (bool, error) {
	return VerifyPasswordContext(context.Background(), plain, stored)
}

func VerifyPasswordContext(ctx context.Context, plain, stored string) (bool, error) {
	release, err := acquireHashSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("bad hash format")
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}
	if memory < 8*1024 || memory > argMemory || time < 1 || time > 10 || threads < 1 || threads > 16 {
		return false, errors.New("unsupported hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, errors.New("invalid hash salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != int(argKeyLen) {
		return false, errors.New("invalid hash key")
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
