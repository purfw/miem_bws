package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type LinkCipher struct {
	active int16
	keys   map[int16]cipher.AEAD
}

// NewLinkCipher accepts a comma-separated keyring in version:base64 form.
// A legacy bare base64 key is treated as version 1.
func NewLinkCipher(encodedKeys string, activeVersion int) (*LinkCipher, error) {
	if activeVersion < 1 || activeVersion > 32767 {
		return nil, errors.New("JOIN_URL_ACTIVE_KEY_VERSION must be between 1 and 32767")
	}
	result := &LinkCipher{active: int16(activeVersion), keys: make(map[int16]cipher.AEAD)}
	for _, item := range strings.Split(encodedKeys, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		version := 1
		encodedKey := item
		if before, after, ok := strings.Cut(item, ":"); ok {
			parsed, err := strconv.Atoi(before)
			if err != nil || parsed < 1 || parsed > 32767 {
				return nil, fmt.Errorf("invalid join URL key version %q", before)
			}
			version, encodedKey = parsed, after
		}
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("join URL key version %d must be base64 for exactly 32 bytes", version)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		if _, exists := result.keys[int16(version)]; exists {
			return nil, fmt.Errorf("duplicate join URL key version %d", version)
		}
		result.keys[int16(version)] = aead
	}
	if _, ok := result.keys[result.active]; !ok {
		return nil, fmt.Errorf("active join URL key version %d is absent from keyring", result.active)
	}
	return result, nil
}

func (c *LinkCipher) Encrypt(plain string, associatedData []byte) (ciphertext, nonce []byte, version int16, err error) {
	aead := c.keys[c.active]
	nonce = make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, err
	}
	return aead.Seal(nil, nonce, []byte(plain), associatedData), nonce, c.active, nil
}

func (c *LinkCipher) Decrypt(ciphertext, nonce, associatedData []byte, version int16) (string, error) {
	aead, ok := c.keys[version]
	if !ok {
		return "", fmt.Errorf("decrypt join URL: key version %d unavailable", version)
	}
	plain, err := aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return "", fmt.Errorf("decrypt join URL: %w", err)
	}
	return string(plain), nil
}
