package highgo

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestHighGoCompletedHandshakeReleasesCancellationWatch(t *testing.T) {
	info := newSM3Server(t, "瀚高数据库管理系统V4.5")
	dsn, err := (&Plugin{}).BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("highgo", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	var version string
	if err := conn.QueryRowContext(t.Context(), "SELECT version()").Scan(&version); err != nil {
		t.Fatalf("canceling completed startup closed the leased connection: %v", err)
	}
}

func TestHighGoHandshakeHonorsCancellation(t *testing.T) {
	for _, stage := range []string{"ssl_request", "tls_handshake", "sm3_authentication"} {
		t.Run(stage, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			ready := make(chan error, 1)
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				c, err := listener.Accept()
				if err != nil {
					ready <- err
					return
				}
				defer c.Close()
				if stage != "ssl_request" {
					err = diagnosticHandshakeStage(c, stage)
				}
				ready <- err
				<-release
			}()
			t.Cleanup(func() { close(release); listener.Close(); <-serverDone })
			host, portString, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portString)
			mode := "require"
			if stage == "sm3_authentication" {
				mode = "disable"
			}
			info := plugin.ConnectionInfo{"host": host, "port": port, "database": "test", "user": "user", "password": "fixture-password", "sslmode": mode}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- (&Plugin{}).TestConnection(ctx, info) }()
			select {
			case err := <-ready:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("client did not reach handshake stage")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("handshake cancellation must retain context error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Errorf("canceled query remains blocked in %s", stage)
			}
		})
	}
}

func diagnosticHandshakeStage(c net.Conn, stage string) error {
	if stage == "tls_handshake" {
		request := make([]byte, 8)
		if _, err := io.ReadFull(c, request); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(request[4:]) != 80877103 {
			return fmt.Errorf("expected SSLRequest")
		}
		_, err := c.Write([]byte{'S'})
		return err
	}
	var size uint32
	if err := binary.Read(c, binary.BigEndian, &size); err != nil {
		return err
	}
	if size < 8 || size > 65536 {
		return fmt.Errorf("invalid startup packet length")
	}
	if _, err := io.CopyN(io.Discard, c, int64(size-4)); err != nil {
		return err
	}
	if err := sendMessage(c, 'R', []byte{0, 0, 0, 13, 1, 2, 3, 4}); err != nil {
		return err
	}
	kind, _, err := readMessage(c)
	if err != nil {
		return err
	}
	if kind != 'p' {
		return fmt.Errorf("expected SM3 password response")
	}
	return nil
}
