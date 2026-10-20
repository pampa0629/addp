package ftpclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

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
