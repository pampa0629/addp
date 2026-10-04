package engineaccess

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestSourceDenyCommandContract(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	future := now.Add(time.Hour)
	input := CreateDenyInput{Actor: Actor{TenantID: 1, PrincipalID: 2, MembershipID: 3}, EngineID: 9,
		DenyID: uuid.New(), CatalogPath: engineplugin.TabularItemPath(9, "schema", "public", "events"), RecipientType: "user", RecipientID: 4,
		Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "  Restrict reading\n"}
	original, err := prepareSourceDeny(input)
	if err != nil || original.Reason != "Restrict reading" || original.ExpiresAt != nil {
		t.Fatalf("long-term command=%+v %v", original, err)
	}
	for name, mutate := range map[string]func(*CreateDenyInput){
		"missing mode":        func(v *CreateDenyInput) { v.ExpiryMode = "" },
		"unknown mode":        func(v *CreateDenyInput) { v.ExpiryMode = "permanent" },
		"date missing":        func(v *CreateDenyInput) { v.ExpiryMode = shared.SharingExpiryAtTime },
		"long-term with date": func(v *CreateDenyInput) { v.ExpiresAt = &future },
		"wrong engine":        func(v *CreateDenyInput) { v.EngineID++ },
		"zero command":        func(v *CreateDenyInput) { v.DenyID = uuid.Nil },
		"zero subject":        func(v *CreateDenyInput) { v.RecipientID = 0 },
		"all tenant":          func(v *CreateDenyInput) { v.RecipientType = "tenant" },
		"write action":        func(v *CreateDenyInput) { v.Action = "write" },
		"empty reason":        func(v *CreateDenyInput) { v.Reason = "\n " },
		"non leaf": func(v *CreateDenyInput) {
			v.CatalogPath.Segments = v.CatalogPath.Segments[:len(v.CatalogPath.Segments)-1]
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := input
			mutate(&bad)
			if _, err := prepareSourceDeny(bad); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
	for _, kind := range []string{"user", "department", "project_group"} {
		v := input
		v.RecipientType = kind
		v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryAtTime, &future
		row, err := prepareSourceDeny(v)
		if err != nil || !row.ExpiresAt.Equal(future) {
			t.Fatalf("finite %s=%+v %v", kind, row, err)
		}
		past := now.Add(-time.Hour)
		v.ExpiresAt = &past
		if _, err := prepareSourceDeny(v); err != nil {
			t.Fatalf("historical expiry cannot be normalized: %v", err)
		}
	}
	for name, mutate := range map[string]func(*SourceDeny){
		"command": func(v *SourceDeny) { v.DenyID = uuid.New() }, "tenant": func(v *SourceDeny) { v.TenantID++ },
		"engine": func(v *SourceDeny) { v.EngineID++ }, "path": func(v *SourceDeny) { v.CatalogPath = []byte(`{}`) },
		"kind": func(v *SourceDeny) { v.RecipientType = "department" }, "subject": func(v *SourceDeny) { v.RecipientID++ },
		"action": func(v *SourceDeny) { v.Action = "write" }, "reason": func(v *SourceDeny) { v.Reason += "!" },
		"operator": func(v *SourceDeny) { v.EstablishedByPrincipalID++ }, "membership": func(v *SourceDeny) { v.EstablishedByMembershipID++ },
		"expiry": func(v *SourceDeny) { v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryAtTime, &future },
	} {
		t.Run("retry "+name, func(t *testing.T) {
			changed := *original
			mutate(&changed)
			if sameSourceDeny(original, &changed) {
				t.Fatal("changed command matched history")
			}
		})
	}
	changed := *original
	changed.EstablishedAt = now
	if !sameSourceDeny(original, &changed) {
		t.Fatal("storage timestamp changed command identity")
	}
}
