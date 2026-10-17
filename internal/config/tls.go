package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
)

// BridgeTLSConfig verifies remote Bridge connections against an explicit CA.
// The legacy self-signed mode is restricted to literal loopback destinations.
func (c BridgeConfig) BridgeTLSConfig(host string) (*tls.Config, error) {
	name := c.TLSServerName
	if name == "" {
		name = strings.TrimSpace(host)
	}
	if c.TLSCAFile != "" {
		pem, err := os.ReadFile(c.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("read Bridge TLS CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("Bridge TLS CA file contains no certificates")
		}
		return &tls.Config{RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS12}, nil
	}
	if !IsLoopbackHost(host) {
		return nil, fmt.Errorf("refusing to connect: Bridge host %q is not a loopback address; configure bridge.tls_ca_file for verified remote TLS", host)
	}
	if c.TLSServerName != "" {
		return nil, fmt.Errorf("bridge.tls_server_name requires bridge.tls_ca_file")
	}
	return &tls.Config{InsecureSkipVerify: true, ServerName: name, MinVersion: tls.VersionTLS12}, nil
}

// IsLoopbackHost deliberately does not resolve DNS aliases to local addresses.
func IsLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
