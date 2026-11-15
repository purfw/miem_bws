package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/confhub/shared/telemetry"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func TLSConfig() (*tls.Config, error) {
	ca, cert, key := os.Getenv("MTLS_CA_FILE"), os.Getenv("MTLS_CERT_FILE"), os.Getenv("MTLS_KEY_FILE")
	if ca == "" && cert == "" && key == "" {
		return nil, nil
	}
	if ca == "" || cert == "" || key == "" {
		return nil, errors.New("all three MTLS files are required")
	}
	pem, err := os.ReadFile(ca)
	if err != nil {
		return nil, fmt.Errorf("read mTLS CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("invalid mTLS CA")
	}
	identity, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, fmt.Errorf("load mTLS identity: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ClientCAs: roots,
		Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}

type failedTransport struct{ err error }

func (t failedTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

type internalTransport struct {
	base   http.RoundTripper
	secure bool
}

func (t internalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.secure && r.URL.Scheme != "https" {
		return nil, errors.New("plaintext internal request refused while mTLS is enabled")
	}
	return t.base.RoundTrip(r)
}

func NewClient(timeout time.Duration) *http.Client {
	config, err := TLSConfig()
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: failedTransport{err}}
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = config
	base.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: telemetry.ClientTransport{Base: internalTransport{base: base, secure: config != nil}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

var servers sync.Map

// Internal APIs have a separate listener; the public listener cannot bypass mTLS.
func ListenAndServe(public *http.Server) error {
	config, err := TLSConfig()
	if err != nil {
		return err
	}
	if config == nil {
		return public.ListenAndServe()
	}
	original := public.Handler
	public.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal" || strings.HasPrefix(r.URL.Path, "/internal/") {
			http.NotFound(w, r)
			return
		}
		original.ServeHTTP(w, r)
	})
	addr := os.Getenv("MTLS_LISTEN_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mTLS listen: %w", err)
	}
	secure := &http.Server{Addr: addr, TLSConfig: config, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/internal/") || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
				http.NotFound(w, r)
				return
			}
			original.ServeHTTP(w, r)
		})}
	servers.Store(public, secure)
	tlsErr := make(chan error, 1)
	go func() {
		err := secure.Serve(tls.NewListener(listener, config))
		tlsErr <- err
		if !errors.Is(err, http.ErrServerClosed) {
			_ = public.Close()
		}
	}()
	err = public.ListenAndServe()
	_ = secure.Close()
	servers.Delete(public)
	if e := <-tlsErr; e != nil && !errors.Is(e, http.ErrServerClosed) {
		return e
	}
	return err
}

func Shutdown(ctx context.Context, public *http.Server) error {
	if value, ok := servers.Load(public); ok {
		if err := value.(*http.Server).Shutdown(ctx); err != nil {
			_ = public.Close()
			return err
		}
	}
	return public.Shutdown(ctx)
}
