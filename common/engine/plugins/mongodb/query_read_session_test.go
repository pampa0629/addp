package mongodb

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestMongoDBDeclaresAndImplementsQueryReadSession(t *testing.T) {
	provider := &MongoDBPlugin{}
	if _, ok := interface{}(provider).(plugin.QueryReadSessionProvider); !ok {
		t.Fatal("MongoDB plugin does not implement QueryReadSessionProvider")
	}
	if provider.Capabilities().Compute == nil || provider.Capabilities().Compute.Query == nil || !provider.Capabilities().Compute.Query.ReadSession {
		t.Fatal("MongoDB capabilities do not declare query read_session")
	}
	if err := plugin.ValidatePluginCapabilities(provider); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func TestParseMongoQueryReadPlanAcceptsFlatteningAggregateWithoutPreviewLimit(t *testing.T) {
	request := plugin.QueryRequest{
		Language: "mql",
		Query:    `{"aggregate":"Outdoors","pipeline":[{"$unwind":"$members"},{"$project":{"person_id":"$members.personid","activity_id":"$_id","_id":0}}]}`,
		TargetPath: &plugin.EngineCatalogPath{
			Version:  plugin.EngineCatalogPathVersion,
			EngineID: 11,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
			},
		},
		Options: plugin.QueryOptions{ReadOnly: true},
	}
	plan, err := parseMongoQueryReadPlan(plugin.ConnectionInfo{}, request)
	if err != nil {
		t.Fatalf("parseMongoQueryReadPlan() error = %v", err)
	}
	if plan.database != "Outdoor" || plan.collection != "Outdoors" || plan.command != "aggregate" {
		t.Fatalf("parseMongoQueryReadPlan() = %#v", plan)
	}
	pipeline := plan.document["pipeline"].([]interface{})
	if len(pipeline) != 2 {
		t.Fatalf("pipeline length = %d, want original 2 stages without implicit limit", len(pipeline))
	}
}

func TestParseMongoQueryReadPlanRejectsUnsafeOrPreviewRequests(t *testing.T) {
	tests := []struct {
		name    string
		request plugin.QueryRequest
		want    string
	}{
		{
			name: "write stage",
			request: plugin.QueryRequest{Language: "mql", Query: `{"aggregate":"Outdoors","pipeline":[{"$merge":"target"}]}`,
				Options: plugin.QueryOptions{ReadOnly: true}},
			want: "$out and $merge",
		},
		{
			name: "preview limit",
			request: plugin.QueryRequest{Language: "mql", Query: `{"find":"Outdoors","filter":{}}`,
				Options: plugin.QueryOptions{ReadOnly: true, Limit: 500}},
			want: "does not accept preview limit",
		},
		{
			name:    "not read only",
			request: plugin.QueryRequest{Language: "mql", Query: `{"find":"Outdoors","filter":{}}`},
			want:    "requires read_only=true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMongoQueryReadPlan(plugin.ConnectionInfo{"database": "Outdoor"}, tt.request)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseMongoQueryReadPlan() error = %v, want %q", err, tt.want)
			}
		})
	}
}
