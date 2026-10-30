package imap

import (
	"errors"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bscott/pm-cli/internal/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type loginResponseSession struct {
	imapserver.Session
	loginError error
}

func (s *loginResponseSession) Login(username, password string) error {
	if s.loginError != nil {
		return s.loginError
	}
	return s.Session.Login(username, password)
}

func TestConnectBridgeLoginDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     []string
	}{
		{"missing user", "no such user", []string{"Bridge may have unloaded", "Bridge host disk space", "no space left on device", "Removing IMAP user", "Bridge mailbox username", "Bridge app password"}},
		{"rate limited", "too many login attempts", []string{"Stop automatic login retries", "Bridge logs", "disk space", "After fixing"}},
		{"case insensitive", "NO SUCH USER", []string{"Bridge may have unloaded"}},
		{"other failure", "authentication failed", nil},
		{"success", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certificateServer := httptest.NewTLSServer(nil)
			tlsConfig := certificateServer.TLS.Clone()
			certificateServer.Close()
			mem := imapmemserver.New()
			mem.AddUser(imapmemserver.NewUser("user@example.com", "example-secret"))
			var loginError error
			if tc.response != "" {
				loginError = &imap.Error{Type: imap.StatusResponseTypeNo, Text: tc.response}
			}
			srv := imapserver.New(&imapserver.Options{
				TLSConfig: tlsConfig,
				NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
					return &loginResponseSession{Session: mem.NewSession(), loginError: loginError}, nil, nil
				},
			})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { srv.Close(); listener.Close() })
			go srv.Serve(listener)
			host, port, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.DefaultConfig()
			cfg.Bridge.IMAPHost = host
			cfg.Bridge.IMAPPort, err = strconv.Atoi(port)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Bridge.Email = "user@example.com"
			t.Setenv(config.EnvBridgePassword, "example-secret")
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			err = client.Connect()
			if tc.response == "" {
				if err != nil {
					t.Fatal(err)
				}
				client.Close()
				return
			}
			if err == nil {
				t.Fatal("expected login failure")
			}
			var protocolError *imap.Error
			if !errors.As(err, &protocolError) || protocolError.Text != tc.response {
				t.Fatalf("original IMAP error lost: %v", err)
			}
			for _, want := range append(tc.want, listener.Addr().String()) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("missing %q in %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "example-secret") {
				t.Fatal("password leaked")
			}
			if tc.name == "other failure" && strings.Contains(err.Error(), "disk space") {
				t.Fatal("unrelated error received storage diagnosis")
			}
		})
	}
}
