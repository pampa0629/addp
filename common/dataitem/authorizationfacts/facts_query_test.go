package authorizationfacts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAuthorizationFactsBatchBindsEveryRequestedIdentity(t *testing.T) {
	facts := authorizationTestFacts(t)
	missing := strings.Repeat("b", 64)
	query := AuthorizationFactsQuery{Fingerprints: []string{facts.ItemFingerprint, missing}}
	batch := AuthorizationFactsBatch{Data: []AuthorizationFactsResult{{ItemFingerprint: facts.ItemFingerprint, Facts: &facts}, {ItemFingerprint: missing}}}
	raw, _ := json.Marshal(batch)
	var decoded AuthorizationFactsBatch
	if json.Unmarshal(raw, &decoded) != nil || decoded.Validate(query) != nil {
		t.Fatal("exact ordered batch rejected")
	}
	for _, body := range []string{
		strings.Replace(string(raw), `"data":`, `"data":[],"data":`, 1),
		strings.Replace(string(raw), `"facts":null`, `"facts":null,"facts":null`, 1),
		strings.Replace(string(raw), `"facts":null`, `"private":"x"`, 1),
		`{"data":null}`, string(raw) + `{}`,
	} {
		if json.Unmarshal([]byte(body), &decoded) == nil {
			t.Fatal("malformed batch accepted")
		}
	}
	batch.Data[0], batch.Data[1] = batch.Data[1], batch.Data[0]
	if batch.Validate(query) == nil {
		t.Fatal("reordered results accepted")
	}
	batch.Data = batch.Data[:1]
	if batch.Validate(query) == nil {
		t.Fatal("partial batch accepted")
	}
	bounded := AuthorizationFactsQuery{}
	for i := 0; i < MaxAuthorizationFactsQueryItems; i++ {
		bounded.Fingerprints = append(bounded.Fingerprints, fmt.Sprintf("%064x", i+1))
	}
	requestBody, err := json.Marshal(bounded)
	if err != nil || bounded.Validate() != nil || len(requestBody) > MaxAuthorizationFactsQueryBytes {
		t.Fatalf("full exact batch exceeds the request budget: bytes=%d err=%v", len(requestBody), err)
	}
	bounded.Fingerprints = append(bounded.Fingerprints, fmt.Sprintf("%064x", MaxAuthorizationFactsQueryItems+1))
	if bounded.Validate() == nil {
		t.Fatal("owner accepted a batch above the item limit")
	}
	for _, fps := range [][]string{nil, {missing, missing}, {"invalid"}} {
		if (AuthorizationFactsQuery{Fingerprints: fps}).Validate() == nil {
			t.Fatal("invalid query accepted")
		}
	}
}
