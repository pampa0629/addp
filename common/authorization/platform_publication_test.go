package authorization

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlatformPublicationBindingAndObservationAreExactAndBounded(t *testing.T) {
	binding := PlatformPublicationCheck{Capability: "transfer.task.create", Revision: "2", Digest: strings.Repeat("a", 64)}
	encoded, _ := json.Marshal(binding)
	var decoded PlatformPublicationCheck
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != binding {
		t.Fatalf("decode=%+v %v", decoded, err)
	}
	for _, invalid := range []string{"null", "{}", `[]`, string(encoded) + "{}", strings.Replace(string(encoded), `"revision":"2"`, `"revision":2`, 1), strings.Replace(string(encoded), `"revision":"2"`, `"revision":null`, 1), strings.Replace(string(encoded), `"revision":"2"`, `"revision":"2","revision":"3"`, 1), strings.Replace(string(encoded), `"revision"`, `"Revision"`, 1), strings.TrimSuffix(string(encoded), "}") + `,"tenant_id":"1"}`, strings.Replace(string(encoded), strings.Repeat("a", 64), strings.Repeat("a", 2048), 1)} {
		if json.Unmarshal([]byte(invalid), &decoded) == nil {
			t.Fatalf("accepted invalid binding %q", invalid)
		}
	}
	for _, revision := range []string{"0", "-1", "01", "+1", " 2", "9223372036854775808", ""} {
		bad := binding
		bad.Revision = revision
		if bad.Validate() == nil {
			t.Fatal("accepted revision", revision)
		}
	}
	for _, cap := range []string{"", "task", "Tenant.Task.Create", "transfer..create", strings.Repeat("a", 129) + ".create"} {
		bad := binding
		bad.Capability = cap
		if bad.Validate() == nil {
			t.Fatal("accepted capability", cap)
		}
	}
	observation := PlatformPublicationObservation{PlatformPublicationCheck: binding, ContextType: "platform", ClientID: PlatformDefinitionPublisherClient, PrincipalID: "9223372036854775807", PrincipalType: "service_principal", AuthorizationVersion: "3"}
	data, _ := json.Marshal(observation)
	var observed PlatformPublicationObservation
	if err := json.Unmarshal(data, &observed); err != nil || observed != observation {
		t.Fatalf("observation=%+v %v", observed, err)
	}
	for _, mutate := range []func(*PlatformPublicationObservation){func(o *PlatformPublicationObservation) { o.Revision = "3" }, func(o *PlatformPublicationObservation) { o.Digest = strings.Repeat("b", 64) }, func(o *PlatformPublicationObservation) { o.ContextType = "tenant" }, func(o *PlatformPublicationObservation) { o.ClientID = "addp-agent" }, func(o *PlatformPublicationObservation) { o.PrincipalType = "user" }, func(o *PlatformPublicationObservation) { o.PrincipalID = "0" }, func(o *PlatformPublicationObservation) { o.AuthorizationVersion = "01" }} {
		bad := observation
		mutate(&bad)
		if bad.Validate(binding) == nil {
			t.Fatalf("accepted observation %+v", bad)
		}
	}
}
