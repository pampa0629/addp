package duckdb

import "testing"

func TestMountKindForEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engineType string
		want       MountKind
	}{
		{engineType: "minio", want: MountKindObject},
		{engineType: "s3", want: MountKindObject},
		{engineType: "postgresql", want: MountKindPostgres},
		{engineType: "mysql", want: MountKindMySQL},
		{engineType: "mongodb", want: MountKindUnsupported},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.engineType, func(t *testing.T) {
			t.Parallel()
			if got := MountKindForEngine(tt.engineType); got != tt.want {
				t.Fatalf("MountKindForEngine(%q) = %q, want %q", tt.engineType, got, tt.want)
			}
		})
	}
}
