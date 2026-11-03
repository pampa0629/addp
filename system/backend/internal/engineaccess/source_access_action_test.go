package engineaccess

import "testing"

func TestMetadataActionRequiresAnEngineType(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "nfs", "ftp", "minio", ""} {
		for _, action := range []string{"read", "metadata_read", "", "write", "READ", "metadata_read "} {
			fulfillmentWant := action == "read" || (action == "metadata_read" && engineType == "postgresql")
			if sourceFulfillmentActionSupported(action, engineType) != fulfillmentWant {
				t.Fatalf("fulfillment expanded: action=%q engine=%q", action, engineType)
			}
			want := action == "read" || (action == "metadata_read" && engineType != "")
			if sourceAccessActionSupported(action, engineType) != want {
				t.Fatalf("action=%q engine=%q", action, engineType)
			}
		}
	}
}
