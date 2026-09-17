package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

func testActor(tenant uint64) models.Actor {
	return models.Actor{TenantID: tenant, PrincipalID: 11, MembershipID: 12, AuthorizationVersion: 1}
}

func testDefinition(id string) semantic.Definition {
	return semantic.Definition{Scope: semantic.Scope{TenantID: 101, OntologyID: id, Revision: 1},
		Classes:    []semantic.Class{{ID: "activity", Name: "北京户外活动观察"}},
		Properties: []semantic.Property{{ID: "has_date", ClassID: "activity", Key: "has_date", Name: "有日期", Kind: semantic.Boolean}},
		Rules: []semantic.Rule{{ID: "valid_date", ClassID: "activity", Expression: "has_date", Basis: "合成夹具的显式日期口径",
			Inputs: []semantic.Input{{Variable: "has_date", PropertyID: "has_date", OnAbsent: semantic.AbsenceFalse}}}},
	}
}

func TestRejectCommandsBeforeDatabaseAccess(t *testing.T) {
	s := NewRevisionService(repository.NewRevisionRepository(nil))
	d := testDefinition("beijing_outdoor")
	for _, a := range []models.Actor{{}, testActor(102), {TenantID: 101, PrincipalID: 11, MembershipID: 12}} {
		if _, err := s.CreateDraft(context.Background(), a, d); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CreateDraft(ctx, testActor(101), d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, testActor(101), 1, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Transition(context.Background(), testActor(101), d.Scope, 1, "save"); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	d.Rules[0].Expression = "has_date + 1"
	if _, err := s.CreateDraft(context.Background(), testActor(101), d); err == nil {
		t.Fatal("invalid definition accepted")
	}
}
