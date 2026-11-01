package engineaccess

import "testing"

func TestMetadataActionsRemainPGOnly(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "nfs", "ftp", "minio", ""} {
		for _, action := range []string{"read", "metadata_read", "", "write", "READ", "metadata_read "} {
			want := action == "read" || (action == "metadata_read" && engineType == "postgresql")
			if sourceAccessActionSupported(action, engineType) != want {
				t.Fatalf("action=%q engine=%q", action, engineType)
			}
		}
	}
}
