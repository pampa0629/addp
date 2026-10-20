package ftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"testing"
	"time"
)

func TestReadRejectsTruncationAndTransferFailure(t *testing.T) {
	for _, transferFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(transferFailure), func(t *testing.T) {
			data, peer := net.Pipe()
			control, server := net.Pipe()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := &Session{ctx: ctx, cancel: cancel, sockets: []net.Conn{data, control}}
			s.Conn = &wire{session: s, text: textproto.NewConn(control)}
			defer s.Close()
			defer server.Close()
			go func() {
				_, _ = peer.Write([]byte("abc"))
				peer.Close()
				if transferFailure {
					fmt.Fprint(server, "426 Transfer aborted\r\n")
				}
			}()
			expected := int64(5)
			if transferFailure {
				expected = 3
			}
			r := &reader{s: s, response: data, remaining: expected}
			_, err := io.ReadAll(r)
			if transferFailure {
				var pe *protocolError
				if !errors.As(err, &pe) || pe.Code != 426 {
					t.Fatal("transfer failure lost", err)
				}
			} else if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal("truncated content reported complete", err)
			}
		})
	}
}

func TestCancelBlockedDataRead(t *testing.T) {
	data, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &Session{ctx: ctx, cancel: cancel, sockets: []net.Conn{data}}
	s.stop = context.AfterFunc(ctx, s.closeSockets)
	defer s.Close()
	r := &reader{s: s, response: data, remaining: 100}
	done := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 10)); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("lost read cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt blocked read")
	}
}
