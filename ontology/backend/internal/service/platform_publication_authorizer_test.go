package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
)

type platformPublicationTokenSource struct{}

func (platformPublicationTokenSource) Token(context.Context, uint) (string, error) {
	return "", errors.New("Tenant token forbidden")
}
func (platformPublicationTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

func TestPlatformPublicationAuthorizerChecksEachExactSnapshotWithoutTenantIdentity(t *testing.T) {
	definition, err := platform.TransferContext()
	if err != nil {
		t.Fatal(err)
	}
	definition.Digest = ""
	source, _ := json.Marshal(definition)
	snapshot, err := platform.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	want := authorization.PlatformPublicationCheck{Capability: definition.Capability, Revision: strconv.FormatUint(definition.Revision, 10), Digest: snapshot.Digest()}
	calls, status := 0, 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer platform-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("incorrect publication identity")
		}
		var got authorization.PlatformPublicationCheck
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got != want {
			t.Errorf("binding=%+v %v", got, err)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(authorization.PlatformPublicationObservation{PlatformPublicationCheck: want, ContextType: "platform", ClientID: "addp-ontology", PrincipalID: "41", PrincipalType: "service_principal", AuthorizationVersion: strconv.Itoa(calls)})
	}))
	defer server.Close()
	c := client.NewSystemServiceClient(server.URL, platformPublicationTokenSource{}, server.Client())
	a, err := NewSystemPlatformPublicationAuthorizer(c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		actor, err := a.Check(context.Background(), snapshot)
		if err != nil || actor != (models.PlatformActor{ContextType: "platform", PrincipalID: 41, PrincipalType: "service_principal", AuthorizationVersion: int64(i)}) {
			t.Fatalf("actor=%+v err=%v", actor, err)
		}
	}
	for _, bad := range []*platform.Snapshot{nil, {}} {
		if actor, err := a.Check(context.Background(), bad); err == nil || actor != (models.PlatformActor{}) || calls != 2 {
			t.Fatal("invalid snapshot passed authorization")
		}
	}
	tenant, _ := NewSystemPlatformPublicationAuthorizer(c.WithTenantID(101))
	if actor, err := tenant.Check(context.Background(), snapshot); err == nil || actor != (models.PlatformActor{}) || calls != 2 {
		t.Fatal("Tenant authorizer accepted")
	}
	status = 403
	if actor, err := a.Check(context.Background(), snapshot); err == nil || actor != (models.PlatformActor{}) {
		t.Fatal("denied identity returned actor")
	}
	if _, err := NewSystemPlatformPublicationAuthorizer(nil); err == nil {
		t.Fatal("nil client accepted")
	}
}
