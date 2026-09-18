package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
)

func TestListRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	s := NewRevisionService(repository.NewRevisionRepository(nil))
	for _, page := range []models.ListPage{{}, {Page: -1, PageSize: 20}, {Page: 1, PageSize: 101}, {Page: 2147483648, PageSize: 100}} {
		if _, _, err := s.ListOntologies(context.Background(), testActor(101), page); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, _, err := s.ListRevisions(context.Background(), testActor(101), "outdoor", page); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	page := models.ListPage{Page: 1, PageSize: 20}
	for _, actor := range []models.Actor{{}, {TenantID: 101, PrincipalID: 1, MembershipID: 2}, {TenantID: ^uint64(0), PrincipalID: 1, MembershipID: 2, AuthorizationVersion: 1}} {
		if _, _, err := s.ListOntologies(context.Background(), actor, page); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, _, err := s.ListRevisions(context.Background(), actor, "outdoor", page); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, _, err := s.ListRevisions(context.Background(), testActor(101), "", page); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}
