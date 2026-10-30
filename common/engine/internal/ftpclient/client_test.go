package ftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnectFailureKeepsSafeStageAndStatus(t *testing.T) {
	for _, row := range []struct{ reply, category string }{
		{"421 private-user /private-path private-password\r\n", "welcome: FTP server returned status 421"},
		{"", "welcome: connection closed (EOF)"},
		{"private-user /private-path private-password\r\n", "welcome: remote operation rejected or invalid response"},
	} {
		t.Run(row.category, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = fmt.Fprint(conn, row.reply)
			}()
			config := Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "private-user", Password: "private-password", Security: "plain", Timeout: time.Second}
			session, err := Dial(t.Context(), config)
			if session != nil {
				session.Close()
				t.Fatal("rejected connection returned a session")
			}
			if err == nil || !strings.Contains(err.Error(), row.category) {
				t.Fatalf("safe failure category missing: %v", err)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("server reply or credentials escaped redaction")
			}
			if errors.Is(err, io.EOF) {
				t.Fatal("failed connection must not become successful end of content")
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("fixture did not close")
			}
		})
	}
}

func TestConfigPathsAndSecurity(t *testing.T) {
	c := Config{Host: "localhost", Port: 21, User: "reader", Password: "secret", Security: "explicit_tls", Timeout: time.Second}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../x", "a/../x", "a//x", "/absolute", "a\\b", "a\x00b", "a\nb", "a\rb", "a/.", string([]byte{255})} {
		if _, err := Physical(p); err == nil {
			t.Fatalf("accepted invalid path %q", p)
		}
	}
	for _, p := range []string{"中文 空格%样例.csv", "a/b.txt", ".hidden", ""} {
		if physical, err := Physical(p); err != nil || physical != "/"+p {
			t.Fatal("account root or exact path spelling lost", physical, err)
		}
	}
	c.Security = "implicit_tls"
	if c.Validate() == nil {
		t.Fatal("implicit TLS accepted")
	}
}

func TestCancelAndDeadlineDuringGreeting(t *testing.T) {
	for _, cancelNow := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel", false: "deadline"}[cancelNow], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				c, e := listener.Accept()
				if e == nil {
					accepted <- c
				}
			}()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "reader", Password: "secret", Security: "plain", Timeout: 150 * time.Millisecond}
			done := make(chan error, 1)
			go func() { _, err := Dial(ctx, c); done <- err }()
			conn := <-accepted
			defer conn.Close()
			if cancelNow {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("lost context failure", err)
				}
			case <-time.After(time.Second):
				t.Fatal("greeting cancellation blocked")
			}
		})
	}
}
