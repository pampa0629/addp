package plugin

import (
	"context"
	"errors"
	"testing"
)

type baseProbeFixture struct {
	*MockPlugin
	calls int
}

func (p *baseProbeFixture) Type() string { return "test_connection_probe" }
func (p *baseProbeFixture) TestConnection(context.Context, ConnectionInfo) error {
	p.calls++
	return nil
}

type nativeProbeFixture struct {
	*baseProbeFixture
	probes  int
	failure error
}

func (p *nativeProbeFixture) ProbeConnection(context.Context, ConnectionInfo) (ConnectionProbeResult, error) {
	p.probes++
	return ConnectionProbeResult{ServerVersion: " NativeDB 4.5 "}, p.failure
}

func TestConnectionProbeUsesOneProviderAndDiscardsFailedFacts(t *testing.T) {
	base := &baseProbeFixture{MockPlugin: &MockPlugin{}}
	native := &nativeProbeFixture{baseProbeFixture: base}
	Register(native)
	t.Cleanup(func() { Unregister(base.Type()) })
	engine := &Engine{EngineType: base.Type()}
	result, err := ProbeConnection(t.Context(), engine)
	if err != nil || result.ServerVersion != "NativeDB 4.5" || native.probes != 1 || base.calls != 0 {
		t.Fatalf("probe=%+v err=%v probes=%d tests=%d", result, err, native.probes, base.calls)
	}
	if err := TestConnection(t.Context(), engine); err != nil || native.probes != 2 || base.calls != 0 {
		t.Fatal("connectivity wrapper did not use the same probe", err)
	}
	native.failure = errors.New("authentication failed")
	result, err = ProbeConnection(t.Context(), engine)
	if !errors.Is(err, native.failure) || result.ServerVersion != "" {
		t.Fatalf("failed probe leaked facts: %+v %v", result, err)
	}
	Unregister(base.Type())
	Register(base)
	result, err = ProbeConnection(t.Context(), engine)
	if err != nil || result.ServerVersion != "" || base.calls != 1 {
		t.Fatalf("unimplemented version must remain unknown: %+v %v", result, err)
	}
	for _, input := range []*Engine{nil, {EngineType: "missing_probe_type"}} {
		if _, err := ProbeConnection(t.Context(), input); err == nil {
			t.Fatal("invalid engine accepted")
		}
	}
}
