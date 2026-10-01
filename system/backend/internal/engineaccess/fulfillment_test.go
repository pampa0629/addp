package engineaccess

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func testFulfillmentRequest() fulfillmentRequest {
	expiresAt := time.Now().Add(time.Hour)
	return fulfillmentRequest{RequestID: uuid.New(), TenantID: 7, CallerPrincipalID: 9,
		Operator: userProvenance{PrincipalID: 17, MembershipID: 18, AuthorizationVersion: 2},
		Path:     engineplugin.TabularItemPath(12, "schema", "业务.域", " 表/名 "), DecisionID: uuid.New(),
		RequirementVersion: 3, RecipientType: "user", RecipientID: 21, Action: "read", ExpiryMode: authorization.SharingExpiryAtTime, ExpiresAt: &expiresAt}
}

func testFulfillmentExpiry(value time.Time) *time.Time {
	value = value.UTC().Truncate(time.Microsecond)
	return &value
}

func TestFulfillmentBindingPreservesExactIdentity(t *testing.T) {
	request := testFulfillmentRequest()
	path, binding, err := request.encode()
	if err != nil {
		t.Fatal(err)
	}
	var decoded engineplugin.EngineCatalogPath
	if err := json.Unmarshal(path, &decoded); err != nil || decoded.Segments[2].Name != " 表/名 " {
		t.Fatalf("exact name lost: %s, %v", path, err)
	}
	for _, mutate := range []func(*fulfillmentRequest){
		func(r *fulfillmentRequest) { r.Operator.PrincipalID++ },
		func(r *fulfillmentRequest) { r.Operator.MembershipID++ },
		func(r *fulfillmentRequest) { r.Operator.AuthorizationVersion++ },
		func(r *fulfillmentRequest) { r.DecisionID = uuid.New() },
		func(r *fulfillmentRequest) { r.RequirementVersion++ },
		func(r *fulfillmentRequest) { r.RecipientID++ },
		func(r *fulfillmentRequest) { r.ExpiresAt = testFulfillmentExpiry(r.ExpiresAt.Add(time.Second)) },
		func(r *fulfillmentRequest) { r.ExpiryMode, r.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil },
	} {
		changed := request
		mutate(&changed)
		_, other, err := changed.encode()
		if err != nil || equalJSON(binding, other) {
			t.Fatalf("changed parameters accepted as same binding: %v", err)
		}
	}
	if !equalJSON(json.RawMessage(`{"id":9007199254740993,"a":1}`), json.RawMessage(`{"a":1,"id":9007199254740993}`)) ||
		equalJSON(json.RawMessage(`{"id":9007199254740993}`), json.RawMessage(`{"id":9007199254740992}`)) {
		t.Fatal("JSONB comparison lost exact integer identity")
	}
}

func TestFulfillmentBindingRejectsIncompleteInputs(t *testing.T) {
	for _, mutate := range []func(*fulfillmentRequest){
		func(r *fulfillmentRequest) { r.Operator = userProvenance{} },
		func(r *fulfillmentRequest) { r.Operator.PrincipalID = 0 },
		func(r *fulfillmentRequest) { r.Operator.MembershipID = -1 },
		func(r *fulfillmentRequest) { r.Operator.AuthorizationVersion = 0 },
		func(r *fulfillmentRequest) { r.RequestID = uuid.Nil },
		func(r *fulfillmentRequest) { r.TenantID = 0 },
		func(r *fulfillmentRequest) { r.CallerPrincipalID = 0 },
		func(r *fulfillmentRequest) { r.DecisionID = uuid.Nil },
		func(r *fulfillmentRequest) { r.RequirementVersion = 0 },
		func(r *fulfillmentRequest) { r.RecipientID = 0 },
		func(r *fulfillmentRequest) { r.RecipientType = "role" },
		func(r *fulfillmentRequest) { r.Action = "write" },
		func(r *fulfillmentRequest) { r.ExpiresAt = nil },
		func(r *fulfillmentRequest) { r.ExpiryMode = "" },
		func(r *fulfillmentRequest) { r.ExpiryMode = "forever" },
		func(r *fulfillmentRequest) { r.ExpiryMode = authorization.SharingExpiryUntilRevoked },
		func(r *fulfillmentRequest) { r.Path = engineplugin.TabularNamespacePath(12, "schema", "public") },
		func(r *fulfillmentRequest) { r.Path.Version = "unknown" },
		func(r *fulfillmentRequest) { r.Path.EngineID = 0 },
	} {
		r := testFulfillmentRequest()
		mutate(&r)
		if _, _, err := r.encode(); err == nil {
			t.Fatalf("invalid request accepted: %#v", r)
		}
	}
}
