package mongodb

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestPreparedQueryReadSetIncludesMongoCrossCollectionStages(t *testing.T) {
	request := plugin.QueryRequest{
		EngineID: 11,
		Language: "mql",
		Query: `{"aggregate":"Persons","pipeline":[
			{"$lookup":{"from":"Groups","localField":"group_id","foreignField":"_id","as":"group"}},
			{"$unionWith":{"coll":"ArchivedPersons","pipeline":[
				{"$graphLookup":{"from":"Referrals","startWith":"$_id","connectFromField":"_id","connectToField":"parent_id","as":"links"}}
			]}}
		]}`,
		TargetPath: &plugin.EngineCatalogPath{
			Version: plugin.EngineCatalogPathVersion, EngineID: 11,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
			},
		},
		Options: plugin.QueryOptions{ReadOnly: true},
	}
	plan, err := prepareMongoQueryReadPlan(plugin.ConnectionInfo{}, request)
	if err != nil {
		t.Fatal(err)
	}
	collections := map[string]struct{}{plan.collection: {}}
	if err := collectMongoAggregateReadCollections(plan.document["pipeline"], collections); err != nil {
		t.Fatal(err)
	}
	readSet, err := mongoQueryReadSet(request, plan.database, collections)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(readSet.Paths))
	for index, path := range readSet.Paths {
		got[index] = path.StringPath()
	}
	want := []string{
		"Outdoor/ArchivedPersons", "Outdoor/Groups", "Outdoor/Persons", "Outdoor/Referrals",
	}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
}

func TestPreparedQueryReadSetRejectsUnresolvedMongoExternalCollection(t *testing.T) {
	provider := &MongoDBPlugin{}
	prepared, err := provider.PrepareQuery(t.Context(), plugin.ConnectionInfo{"database": "Outdoor"}, plugin.QueryRequest{
		EngineID: 11, Language: "mql",
		Query:   `{"aggregate":"Persons","pipeline":[{"$lookup":{"pipeline":[{"$documents":[{"id":1}]}],"as":"inline"}}]}`,
		Options: plugin.QueryOptions{ReadOnly: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepared.ReadSet(t.Context())
	if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("error = %v", err)
	}
}

func TestCollectMongoAggregateReadCollectionsSupportsViewPipelineBSON(t *testing.T) {
	collections := map[string]struct{}{}
	pipeline := primitive.A{
		bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: "Members"}, {Key: "as", Value: "members"}}}},
		bson.D{{Key: "$unionWith", Value: bson.D{{Key: "coll", Value: "Archived"}, {Key: "pipeline", Value: primitive.A{
			bson.D{{Key: "$graphLookup", Value: bson.D{{Key: "from", Value: "Referrals"}}}},
		}}}}},
	}
	if err := collectMongoAggregateReadCollections(pipeline, collections); err != nil {
		t.Fatal(err)
	}
	for _, collection := range []string{"Members", "Archived", "Referrals"} {
		if _, exists := collections[collection]; !exists {
			t.Fatalf("collection %q was not resolved: %#v", collection, collections)
		}
	}
}

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

func TestPrepareMongoQueryReadPlanAcceptsFlatteningAggregateWithoutPreviewLimit(t *testing.T) {
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
	plan, err := prepareMongoQueryReadPlan(plugin.ConnectionInfo{}, request)
	if err != nil {
		t.Fatalf("prepareMongoQueryReadPlan() error = %v", err)
	}
	if plan.database != "Outdoor" || plan.collection != "Outdoors" || plan.command != "aggregate" {
		t.Fatalf("prepareMongoQueryReadPlan() = %#v", plan)
	}
	pipeline := plan.document["pipeline"].([]interface{})
	if len(pipeline) != 2 {
		t.Fatalf("pipeline length = %d, want original 2 stages without implicit limit", len(pipeline))
	}
}

func TestMongoQueryReadSessionValidationRejectsUnsafeOrPreviewRequests(t *testing.T) {
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
			plan, err := prepareMongoQueryReadPlan(plugin.ConnectionInfo{"database": "Outdoor"}, tt.request)
			if err == nil {
				err = validateMongoQueryReadSession(tt.request, plan)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MongoDB query read session validation error = %v, want %q", err, tt.want)
			}
		})
	}
}
