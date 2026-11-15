package service

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testJoinKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func TestLinkCipherKeyRotationKeepsOldLinksReadable(t *testing.T) {
	keyring := "1:" + testJoinKey(1) + ",2:" + testJoinKey(2)
	oldCipher, err := NewLinkCipher(keyring, 1)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, version, err := oldCipher.Encrypt("https://meet.example/old", []byte("session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}

	rotated, err := NewLinkCipher(keyring, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := rotated.Decrypt(ciphertext, nonce, []byte("session-1"), version)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://meet.example/old" {
		t.Fatalf("decrypted %q", got)
	}

	_, _, newVersion, err := rotated.Encrypt("https://meet.example/new", []byte("session-2"))
	if err != nil {
		t.Fatal(err)
	}
	if newVersion != 2 {
		t.Fatalf("version = %d, want 2", newVersion)
	}
}

func TestLinkCipherRejectsMissingActiveKey(t *testing.T) {
	if _, err := NewLinkCipher("1:"+testJoinKey(1), 2); err == nil {
		t.Fatal("expected missing active key error")
	}
}
