package api

import (
	"testing"

	"github.com/addp/graph/internal/models"
)

func TestValidateExpandRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     models.ExpandRequest
		wantErr bool
	}{
		{name: "entity", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: "4:person-1"}, Depth: 2, NodeLimit: 200, RelationshipLimit: 400}},
		{name: "aggregate", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "aggregate", Labels: []string{"Person"}}, Depth: 1, NodeLimit: 200, RelationshipLimit: 400}},
		{name: "unknown kind", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "legacy", ID: "x"}, Depth: 1}, wantErr: true},
		{name: "entity without id", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity"}, Depth: 1}, wantErr: true},
		{name: "aggregate without labels", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "aggregate"}, Depth: 1}, wantErr: true},
		{name: "depth too large", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: "x"}, Depth: 4}, wantErr: true},
		{name: "node budget too large", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: "x"}, NodeLimit: 501}, wantErr: true},
		{name: "relationship budget too large", req: models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: "x"}, RelationshipLimit: 1001}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpandRequest(&tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("validateExpandRequest() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validateExpandRequest() error = %v", err)
			}
		})
	}
}
