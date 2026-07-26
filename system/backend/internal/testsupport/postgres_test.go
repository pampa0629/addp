package testsupport

import "testing"

func TestValidateDisposablePostgresDSN(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "reject development database",
			dsn:     "host=localhost port=15432 user=addp password=secret dbname=addp sslmode=disable",
			wantErr: true,
		},
		{
			name: "accept keyword test database",
			dsn:  "host=localhost port=15432 user=addp password=secret dbname=addp_iam_test sslmode=disable",
		},
		{
			name: "accept URL disposable database",
			dsn:  "postgres://addp:secret@localhost:15432/addp-iam-disposable?sslmode=disable",
		},
		{
			name:    "reject ambiguous test substring",
			dsn:     "postgres://addp:secret@localhost:15432/addp_latest?sslmode=disable",
			wantErr: true,
		},
		{
			name:    "reject missing database",
			dsn:     "host=localhost port=15432 user=addp password=secret sslmode=disable",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDisposablePostgresDSN(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDisposablePostgresDSN() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
