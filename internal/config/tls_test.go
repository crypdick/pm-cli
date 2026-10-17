package config

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedRemoteBridgeTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	path := filepath.Join(t.TempDir(), "bridge-ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := BridgeConfig{TLSCAFile: path, TLSServerName: "127.0.0.1"}
	verified, err := cfg.BridgeTLSConfig("bridge.internal")
	if err != nil {
		t.Fatal(err)
	}
	if verified.InsecureSkipVerify {
		t.Fatal("remote TLS must verify certificates")
	}
	transport := &http.Transport{TLSClientConfig: verified}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.Status)
	}
	for _, test := range []string{"wrong-hostname", "untrusted-certificate"} {
		t.Run(test, func(t *testing.T) {
			tlsConfig := verified.Clone()
			if test == "wrong-hostname" {
				tlsConfig.ServerName = "wrong.example"
			} else {
				tlsConfig.RootCAs = x509.NewCertPool()
			}
			transport := &http.Transport{TLSClientConfig: tlsConfig}
			defer transport.CloseIdleConnections()
			if response, err := (&http.Client{Transport: transport}).Get(server.URL); err == nil {
				response.Body.Close()
				t.Fatal("untrusted TLS connection succeeded")
			}
		})
	}
}

func TestBridgeTLSRequiresTrustBeforeRemoteConnection(t *testing.T) {
	for _, host := range []string{"bridge.internal", "10.0.0.2", "localhost.evil.example"} {
		if _, err := (BridgeConfig{}).BridgeTLSConfig(host); err == nil {
			t.Fatalf("accepted untrusted remote %s", host)
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		cfg, err := (BridgeConfig{}).BridgeTLSConfig(host)
		if err != nil || !cfg.InsecureSkipVerify {
			t.Fatalf("loopback compatibility failed: %s %v", host, err)
		}
	}
	if _, err := (BridgeConfig{TLSServerName: "127.0.0.1"}).BridgeTLSConfig("localhost"); err == nil {
		t.Fatal("server name alone must not imply verified TLS")
	}
	missing := filepath.Join(t.TempDir(), "missing.crt")
	if _, err := (BridgeConfig{TLSCAFile: missing}).BridgeTLSConfig("bridge.internal"); err == nil {
		t.Fatal("missing CA accepted")
	}
	if err := os.WriteFile(missing, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (BridgeConfig{TLSCAFile: missing}).BridgeTLSConfig("bridge.internal"); err == nil {
		t.Fatal("invalid CA accepted")
	}
}
