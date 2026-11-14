package upgrade

import "testing"

func TestLegacyReferencesConvertOnceWithoutChangingKeys(t *testing.T) {
	cases := map[string]string{
		"addp-infra://minio/manager/tenant_7/a%20%E4%B8%AD%2B%25.shp?type=object": "addp-infra://object/manager/tenant_7/a%20%E4%B8%AD+%25.shp?type=object",
		"addp-infra://minio/manager/session/?type=directory":                      "addp-infra://object/manager/session?type=prefix",
		"addp-infra://minio/manager/literal%252F.shp?type=object":                 "addp-infra://object/manager/literal%252F.shp?type=object",
	}
	for old, want := range cases {
		got, err := Reference(old)
		if err != nil || got != want {
			t.Fatalf("%s => %s %v", old, got, err)
		}
		if twice, err := Reference(got); err != nil || twice != want {
			t.Fatalf("second conversion %s %v", twice, err)
		}
	}
	for _, bad := range []string{"addp-infra://minio/unknown/a?type=object", "addp-infra://minio/manager/a%2Fb?type=object", "addp-infra://minio/manager/a?type=object&type=prefix"} {
		if _, err := Reference(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
