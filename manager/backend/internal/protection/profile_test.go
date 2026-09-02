package protection

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/dataprofile"
)

func TestProtectProfileSuppressesWholeSensitiveFieldWithoutMutatingSource(t *testing.T) {
	profile := &dataprofile.Profile{
		FieldCount: 2,
		Fields: []dataprofile.FieldProfile{
			{Name: "name", Type: datatype.FieldTypeString, TopValues: []dataprofile.ValueCount{{Value: "Alice", Count: 1}}},
			{Name: "userInfo.phone", Type: datatype.FieldTypeString, TopValues: []dataprofile.ValueCount{{Value: "13661384499", Count: 1}}},
		},
		Observations: []dataprofile.Observation{
			{Code: dataprofile.ObservationHighCardinality, Field: "userInfo.phone"},
			{Code: dataprofile.ObservationConstant, Field: "name"},
		},
	}
	rules := []dataprotection.Rule{{
		Action: ActionProfile, Component: dataprotection.Component{Key: "userInfo.phone"},
		Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress},
	}}

	protected, err := ProtectProfile(profile, rules)
	if err != nil {
		t.Fatal(err)
	}
	if protected == profile || protected.FieldCount != 1 || len(protected.Fields) != 1 || protected.Fields[0].Name != "name" {
		t.Fatalf("protected profile = %#v", protected)
	}
	if len(protected.Observations) != 1 || protected.Observations[0].Field != "name" {
		t.Fatalf("protected observations = %#v", protected.Observations)
	}
	if len(profile.Fields) != 2 || profile.FieldCount != 2 {
		t.Fatal("ProtectProfile mutated the source profile")
	}
}

func TestProtectProfileFailsClosedForDenyUnknownAndMissingComponent(t *testing.T) {
	profile := &dataprofile.Profile{Fields: []dataprofile.FieldProfile{{Name: "name"}}}
	for _, rule := range []dataprotection.Rule{
		{Action: ActionProfile, Component: dataprotection.Component{Key: "name"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectDeny}},
		{Action: ActionProfile, Component: dataprotection.Component{Key: "name"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask}},
		{Action: ActionProfile, Component: dataprotection.Component{Key: "missing"}, Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress}},
	} {
		if _, err := ProtectProfile(profile, []dataprotection.Rule{rule}); !errors.Is(err, ErrRequired) {
			t.Fatalf("ProtectProfile(%#v) error = %v", rule, err)
		}
	}
}

func TestProtectProfileSuppressesAncestorContainerThatCanAggregateSensitiveLeaf(t *testing.T) {
	profile := &dataprofile.Profile{
		FieldCount: 3,
		Fields: []dataprofile.FieldProfile{
			{Name: "userInfo", Type: datatype.FieldTypeJSON, TopValues: []dataprofile.ValueCount{{Value: map[string]interface{}{"phone": "13661384499", "nickName": "daydayup"}, Count: 1}}},
			{Name: "userInfo.phone", Type: datatype.FieldTypeString, TopValues: []dataprofile.ValueCount{{Value: "13661384499", Count: 1}}},
			{Name: "userInfo.nickName", Type: datatype.FieldTypeString, TopValues: []dataprofile.ValueCount{{Value: "daydayup", Count: 1}}},
		},
	}
	rules := []dataprotection.Rule{{
		Action: ActionProfile,
		Component: dataprotection.Component{
			Key:  "userInfo.phone",
			Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}},
		},
		Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress},
	}}

	protected, err := ProtectProfile(profile, rules)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protected)
	if strings.Contains(string(payload), "13661384499") || protected.FieldCount != 1 || protected.Fields[0].Name != "userInfo.nickName" {
		t.Fatalf("protected profile = %s", payload)
	}
}
