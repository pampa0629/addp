// Package ftp certifies the pinned FTP transport against an owned real server.
package ftp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/addp/common/engine/internal/ftpclient"
)

func config(t *testing.T, security string) ftpclient.Config {
	t.Helper()
	if os.Getenv("ADDP_FTP_INTEGRATION") != "1" {
		t.Skip("requires make test-common-ftp")
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_FTP_ADMISSION_DIR"), "connection.json"))
	if err != nil {
		t.Fatal("private connection missing")
	}
	var c struct {
		Host, User, Password, CA string
		Port                     int
	}
	if json.Unmarshal(b, &c) != nil {
		t.Fatal("invalid connection")
	}
	ca, err := os.ReadFile(c.CA)
	if err != nil {
		t.Fatal("private CA unavailable")
	}
	result := ftpclient.Config{Host: c.Host, Port: c.Port, User: c.User, Password: c.Password, Root: "/", Security: security, Timeout: 10 * time.Second}
	if security == "explicit_tls" {
		result.CAPEM = string(ca)
	}
	return result
}

func TestAdmissionCatalogTLSAndReadOnly(t *testing.T) {
	for _, mode := range []string{"plain", "explicit_tls"} {
		t.Run(mode, func(t *testing.T) {
			c := config(t, mode)
			s, err := ftpclient.Dial(t.Context(), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			entries, err := s.List("/examples")
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{"中文 空格%样例.csv": true, "demo.json": true, "range.bin": true, ".hidden.txt": true, "客户 中文%.csv": true, "订单 示例.json": true, "客户 示例.parquet": true}
			for _, entry := range entries {
				if !want[entry.Name] {
					t.Fatal("unexpected or normalized name")
				}
				delete(want, entry.Name)
			}
			if len(want) != 0 {
				t.Fatal("incomplete directory")
			}
		})
	}
	c := config(t, "explicit_tls")
	c.CAPEM = ""
	if s, err := ftpclient.Dial(t.Context(), c); err == nil {
		s.Close()
		t.Fatal("untrusted certificate accepted")
	}
	c = config(t, "explicit_tls")
	c.Host = "localhost"
	if s, err := ftpclient.Dial(t.Context(), c); err == nil {
		s.Close()
		t.Fatal("wrong hostname accepted")
	}
}

func TestAdmissionRESTRangeAndConcurrentIsolation(t *testing.T) {
	c := config(t, "explicit_tls")
	for _, offset := range []int64{0, 193, 5 << 20} {
		r, err := ftpclient.Open(t.Context(), c, "examples/range.bin", offset, 513)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := make([]byte, 513)
		for i := range want {
			want[i] = byte((offset + int64(i)) % 256)
		}
		if !bytes.Equal(got, want) {
			t.Fatal("REST range differs")
		}
	}
	t.Run("parallel", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			t.Run(string(rune('a'+i)), func(t *testing.T) {
				t.Parallel()
				r, err := ftpclient.Open(t.Context(), c, "examples/demo.json", 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				got, err := io.ReadAll(r)
				if err != nil || !bytes.Contains(got, []byte("长沙")) {
					t.Fatal("isolated read failed", err)
				}
			})
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	r, err := ftpclient.Open(ctx, c, "examples/range.bin", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	defer r.Close()
	deadline := time.After(2 * time.Second)
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, r); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel did not interrupt transfer")
		}
	case <-deadline:
		t.Fatal("cancelled reader blocked")
	}
	for i := 0; i < 12; i++ {
		r, err := ftpclient.Open(t.Context(), c, "examples/range.bin", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
	}
	r, err = ftpclient.Open(t.Context(), c, "examples/demo.json", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = io.ReadAll(r); err != nil {
		t.Fatal("early close poisoned a later session", err)
	}
}
