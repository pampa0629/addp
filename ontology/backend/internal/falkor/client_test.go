package falkor

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
)

func fixture(t *testing.T) *semantic.Snapshot {
	t.Helper()
	s, err := semantic.Freeze(semantic.Definition{
		Scope:      semantic.Scope{TenantID: 101, OntologyID: "outdoor", Revision: 1},
		Classes:    []semantic.Class{{ID: "activity", Name: "北京户外活动"}, {ID: "hike", Name: "徒步", Parents: []string{"activity"}}},
		Properties: []semantic.Property{{ID: "enabled", ClassID: "activity", Key: "enabled", Name: "已启用", Kind: semantic.Boolean}},
		Relations:  []semantic.Relation{{ID: "related", Name: "相关活动", From: "activity", To: "activity"}},
		Rules:      []semantic.Rule{{ID: "eligible", ClassID: "activity", Expression: "enabled", Basis: "明确启用", Inputs: []semantic.Input{{Variable: "enabled", PropertyID: "enabled", OnAbsent: semantic.AbsenceUnknown}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParameters(t *testing.T) {
	text := "北京\"\\\n' RETURN 1 //"
	got, err := parameters(map[string]any{"b": int64(9007199254740993), "a": text})
	if err != nil || got != "CYPHER a=\"北京\\\"\\\\\\n' RETURN 1 //\" b=9007199254740993 " {
		t.Fatalf("%q %v", got, err)
	}
	for _, params := range []map[string]any{
		{"x=1 RETURN 1 //": true}, {"x": 1.5}, {"x": uint64(1)}, {"x": nil},
		{"x": strings.Repeat("x", 8193)}, {"x": string([]byte{0xff})}, {"x": map[string]any{"bad-key": true}},
		{"x": []any{[]any{[]any{[]any{[]any{true}}}}}},
	} {
		if _, err := parameters(params); !errors.Is(err, ErrInvalid) {
			t.Fatalf("must reject: %v", err)
		}
	}
}

func TestPlanImmutableAndScoped(t *testing.T) {
	generation := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	p, err := Plan(fixture(t), generation)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := Plan(fixture(t), generation)
	if !reflect.DeepEqual(p, q) || len(p.nodes) != 5 || len(p.edges) != 6 {
		t.Fatalf("plan mismatch: %+v", p)
	}
	r, _ := Plan(fixture(t), uuid.NewString())
	if p.key == r.key {
		t.Fatal("generations share key")
	}
	for _, id := range []string{"", uuid.Nil.String(), "bad", strings.ToUpper(generation)} {
		if _, err := Plan(fixture(t), id); err == nil {
			t.Fatal("accepted invalid generation")
		}
	}
	if _, err := Plan(nil, generation); err == nil {
		t.Fatal("accepted nil snapshot")
	}
}

func TestClientRejectsBeforeNetwork(t *testing.T) {
	c, err := New(Config{Address: "127.0.0.1:1", Password: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.command(ctx, "PING"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		c.slots <- struct{}{}
	}
	if _, err := c.command(context.Background(), "PING"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		<-c.slots
	}
	for _, config := range []Config{{}, {Address: "host:1"}, {Address: "host:1", Password: "p", TLS: &tls.Config{InsecureSkipVerify: true}}} {
		if _, err := New(config); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := c.query(context.Background(), "foreign", "RETURN 1", nil, true, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := scalarRows([]any{[]any{"n"}, []any{[]any{[]any{"entity"}}}, []any{}}); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
}

func TestCancellationClosesRealConnectionDuringHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	c, _ := New(Config{Address: listener.Addr().String(), Password: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.command(ctx, "PING"); done <- err }()
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("not connected")
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 4096)
	if _, err := conn.Read(buffer); err != nil {
		t.Fatal(err)
	} // handshake sent, no response
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not release blocked read")
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("socket not closed: %v", err)
	}
	if len(c.slots) != 0 {
		t.Fatal("capacity leaked")
	}
}
