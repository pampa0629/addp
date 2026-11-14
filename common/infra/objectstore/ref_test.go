package objectstore

import (
	"testing"
)

func TestReferenceRoundTripAndRejection(t *testing.T) {
	for _, key := range []string{"tenant_7/a b/汉字+%.shp", "tenant_7/literal%2F.json"} {
		want := Ref{Manager, key, "object"}
		value, err := want.URI()
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(value)
		if err != nil || got != want {
			t.Fatalf("round trip: %+v %v", got, err)
		}
	}
	for _, value := range []string{"addp-infra://minio/manager/a?type=object", "addp-infra://object/manager/a%2Fb?type=object", "addp-infra://object/manager/a/../b?type=object", "addp-infra://object/manager/a?type=directory", "addp-infra://object/manager/a?type=object&type=prefix", "addp-infra://object/manager//b?type=object"} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}
