// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrd/certgen"
	"github.com/decred/slog"
)

func init() {
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	log.SetLevel(slog.LevelTrace)
}

var (
	_ SvrCore = (*TCore)(nil)
)

type TCore struct {
}

func (c *TCore) Config() json.RawMessage { return nil }

type tResponseWriter struct {
	b    []byte
	code int
}

func (w *tResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (w *tResponseWriter) Write(msg []byte) (int, error) {
	w.b = msg
	return len(msg), nil
}
func (w *tResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string) error {
	log.Infof("Generating TLS certificates...")

	org := "dcrdex autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, nil)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = ioutil.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	log.Infof("Done generating TLS certificates")
	return nil
}

var tPort = 5555

func newTServer(t *testing.T, start bool, authSHA [32]byte) (*Server, *TCore, func()) {
	c := &TCore{}
	ctx, cancel := context.WithCancel(context.Background())
	tmp, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cert, key := tmp+"/cert.cert", tmp+"/key.key"
	err = genCertPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cert)
	defer os.Remove(key)
	cfg := &SrvConfig{
		Core:    c,
		Addr:    fmt.Sprintf("localhost:%d", tPort),
		Cert:    cert,
		Key:     key,
		AuthSHA: authSHA,
	}
	s, err := NewSrv(cfg)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	if start {
		s.Run(ctx)
	}
	return s, c, cancel
}

func TestAuthMiddleware(t *testing.T) {
	pass := "password123"
	authSHA := sha256.Sum256([]byte(pass))
	s, _, shutdown := newTServer(t, false, authSHA)
	defer shutdown()
	am := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r, _ := http.NewRequest("GET", "", nil)
	wantAuthError := func(name string, want bool) {
		w := &tResponseWriter{}
		am.ServeHTTP(w, r)
		if w.code != http.StatusUnauthorized && w.code != http.StatusOK {
			t.Fatalf("unexpected HTTP error %d for test \"%s\"", w.code, name)
		}
		switch want {
		case true:
			if w.code != http.StatusUnauthorized {
				t.Fatalf("Expected unauthorized HTTP error for test \"%s\"", name)
			}
		case false:
			if w.code != http.StatusOK {
				t.Fatalf("Expected OK HTTP status for test \"%s\"", name)
			}
		}
	}
	tests := []struct {
		name, user, pass string
		wantErr          bool
	}{{
		name: "user and correct password",
		user: "user",
		pass: pass,
	}, {
		name: "only correct password",
		pass: pass,
	}, {
		name:    "only user",
		user:    "user",
		wantErr: true,
	}, {
		name:    "no user or password",
		wantErr: true,
	}, {
		name:    "wrong password",
		user:    "user",
		pass:    pass[1:],
		wantErr: true,
	}}
	for _, test := range tests {
		r.SetBasicAuth(test.user, test.pass)
		wantAuthError(test.name, test.wantErr)
	}
}