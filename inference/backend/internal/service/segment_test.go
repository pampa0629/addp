package service

import (
	"context"
	"encoding/json"
	ci "github.com/addp/common/inference"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSegmentEndpointsStayOnHost(t *testing.T) {
	for _, s := range []string{"http://127.0.0.1:11369", "http://[::1]:11369"} {
		if !localSegmentEndpoint(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"http://localhost.example", "https://example.com", "http://127.0.0.1@remote.example", "http://127.0.0.1/path", "http://10.1.2.3:11369"} {
		if localSegmentEndpoint(s) {
			t.Fatal(s)
		}
	}
}

func TestSegmentBindsTenantAndRejectsRedirect(t *testing.T) {
	tenant := uint(8)
	var calls int
	var scope string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/segment" || r.Header.Get("Authorization") != "Bearer segment-test-key" {
			t.Error("wrong endpoint or credential")
		}
		var body struct {
			Scope string
			Model string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Scope) != 64 || body.Model != "sam3" {
			t.Error("missing scoped model")
		}
		scope = body.Scope
		if calls == 2 {
			http.Redirect(w, r, "https://example.invalid/", http.StatusTemporaryRedirect)
			return
		}
		json.NewEncoder(w).Encode(ci.SegmentResponse{SessionID: strings.Repeat("a", 43), Width: 1, Height: 1})
	}))
	defer upstream.Close()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	actor := Actor{ContextType: "tenant", PrincipalID: 1, TenantID: tenant}
	ctx := context.Background()
	p, err := control.CreateProvider(ctx, actor, ProviderInput{Name: "local", ScopeType: "tenant", AdapterType: AdapterLocalSegment, Endpoint: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = control.SetCredential(ctx, actor, p.ID, "segment-test-key"); err != nil {
		t.Fatal(err)
	}
	d, err := control.CreateDeployment(ctx, actor, DeploymentInput{ProviderConnectionID: p.ID, Name: "SAM", UpstreamModel: "sam3", Operations: []string{"segmentation"}, Modalities: []string{"image"}})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := control.CreateProfile(ctx, actor, ProfileInput{Name: "local", Code: "local-seg", ScopeType: "tenant", ModelDeploymentID: d.ID})
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(store, testEncryptionKey)
	req := ci.SegmentRequest{SchemaVersion: ci.SchemaVersion, TenantID: tenant, Scope: strings.Repeat("0", 64), ModelProfileID: profile.ID, Action: "prepare", Image: "YWJj"}
	if _, err = rt.Segment(ctx, req); err != nil {
		t.Fatal(err)
	}
	if scope == req.Scope {
		t.Fatal("unbound upstream scope")
	}
	req.TenantID++
	if _, err = rt.Segment(ctx, req); err == nil || calls != 1 {
		t.Fatal("other tenant reached model")
	}
	req.TenantID = tenant
	if _, err = rt.Segment(ctx, req); err == nil || calls != 2 {
		t.Fatal("redirect accepted")
	}
}
