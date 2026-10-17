package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/catalog/internal/models"
)

func TestSharingHistoryResolvesRecipientNamesWithoutDependingOnCandidates(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, input := seedSharingEntry(t, db)
	labels := &candidateLabelResolver{}
	svc := NewEntryService(db, nil, labels).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	ctx, access, auth := context.Background(), EntryAccess{Inventory: true}, sharingAuth()
	created, _, err := svc.CreateSharingDecision(ctx, 7, access, entry.ID, input, auth)
	if err != nil || created.RecipientName != "Recipient" || created.RecipientCode != "recipient" || created.RecipientLabelStatus != "resolved" {
		t.Fatalf("create result=%+v error=%v", created, err)
	}
	for _, test := range []struct {
		name              string
		missing, mismatch bool
		err               error
		status            string
	}{
		{"resolved", false, false, nil, "resolved"},
		{"deleted recipient", true, false, nil, "not_found"},
		{"wrong recipient", false, true, nil, "unavailable"},
		{"System offline", false, false, errors.New("offline"), "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			labels.missing, labels.mismatch, labels.err = test.missing, test.mismatch, test.err
			rows, total, err := svc.ListSharingDecisions(ctx, 7, access, entry.ID, auth, 1, 20)
			if err != nil || total != 1 || len(rows) != 1 || rows[0].RecipientLabelStatus != test.status {
				t.Fatalf("rows=%+v total=%d error=%v", rows, total, err)
			}
			view, err := svc.GetSharingDecision(ctx, 7, access, entry.ID, created.ID, auth)
			if err != nil || view.RecipientLabelStatus != test.status || view.RecipientID != input.RecipientID {
				t.Fatalf("view=%+v error=%v", view, err)
			}
			if test.status != "resolved" && (view.RecipientName != "" || rows[0].RecipientName != "") {
				t.Fatal("an unavailable or misbound name leaked into history")
			}
		})
	}
	labels.err, labels.mismatch, labels.missing = nil, false, false
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("visibility", models.VisibilityTenant).Error; err != nil {
		t.Fatal(err)
	}
	for _, single := range []bool{false, true} {
		labels.before = func() {
			if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("visibility", models.VisibilityInventory).Error; err != nil {
				t.Fatal(err)
			}
		}
		var err error
		if single {
			_, err = svc.GetSharingDecision(ctx, 7, EntryAccess{}, entry.ID, created.ID, auth)
		} else {
			_, _, err = svc.ListSharingDecisions(ctx, 7, EntryAccess{}, entry.ID, auth, 1, 20)
		}
		if !errors.Is(err, ErrEntryNotFound) {
			t.Fatalf("visibility loss during resolution exposed history: %v", err)
		}
		labels.before = nil
		if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("visibility", models.VisibilityTenant).Error; err != nil {
			t.Fatal(err)
		}
	}
	var persisted models.SharingDecision
	if err := db.First(&persisted, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.RecipientID != input.RecipientID || persisted.Reason != input.Reason {
		t.Fatal("display resolution changed the immutable decision")
	}
}
