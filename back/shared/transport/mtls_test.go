package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testIdentity(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{"ca.pem": {Type: "CERTIFICATE", Bytes: caDER}, "cert.pem": {Type: "CERTIFICATE", Bytes: der}, "key.pem": {Type: "PRIVATE KEY", Bytes: keyDER}} {
		if err := os.WriteFile(filepath.Join(root, name), pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MTLS_CA_FILE", filepath.Join(root, "ca.pem"))
	t.Setenv("MTLS_CERT_FILE", filepath.Join(root, "cert.pem"))
	t.Setenv("MTLS_KEY_FILE", filepath.Join(root, "key.pem"))
}

func TestMutualTLSAndPlaintextRefusal(t *testing.T) {
	testIdentity(t)
	config, err := TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	response, err := NewClient(time.Second).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("mTLS client rejected")
	}
	anonymous := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: config.RootCAs, MinVersion: tls.VersionTLS13}}}
	if response, err := anonymous.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("client certificate was not required")
	}
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("plaintext was sent") }))
	defer plain.Close()
	if _, err := NewClient(time.Second).Get(plain.URL); err == nil {
		t.Fatal("mTLS silently downgraded")
	}
	t.Setenv("MTLS_CA_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := NewClient(time.Second).Get(plain.URL); err == nil {
		t.Fatal("invalid credentials silently bypassed")
	}
}

func TestPublicListenerCannotReachInternalRoutes(t *testing.T) {
	testIdentity(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	publicAddress := listener.Addr().String()
	listener.Close()
	listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	privateAddress := listener.Addr().String()
	listener.Close()
	t.Setenv("MTLS_LISTEN_ADDR", privateAddress)
	srv := &http.Server{Addr: publicAddress, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	finished := make(chan error, 1)
	go func() { finished <- ListenAndServe(srv) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = Shutdown(ctx, srv)
		<-finished
	}()
	for i := 0; i < 100; i++ {
		response, err := http.Get("http://" + publicAddress + "/healthz")
		if err == nil {
			response.Body.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for path, status := range map[string]int{"/internal/test": 404, "/api/v1/test": 204} {
		response, err := http.Get("http://" + publicAddress + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("public %s: %d", path, response.StatusCode)
		}
	}
	for path, status := range map[string]int{"/internal/test": 204, "/api/v1/test": 404} {
		response, err := NewClient(time.Second).Get("https://" + privateAddress + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("private %s: %d", path, response.StatusCode)
		}
	}
}
