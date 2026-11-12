package service

import (
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseProductionIdentification(t *testing.T, db *gorm.DB) {
	for i, kind := range []string{"native", "geopackage", "3ds"} {
		t.Run("logical production "+kind, func(t *testing.T) {
			var target plugin.EngineCatalogPath
			// Fixtures establish real Meta parent chains and complete composition.
			var primaryID uint = uint(99100 + i)
			var binding authorizationfacts.Binding
			var createdAt time.Time
			var itemFingerprint string
			if kind == "native" {
				item := seedAuthorizationFacts(t, db, "schema", "table", primaryID)
				target = plugin.TabularItemPath(primaryID, "schema", "Out.door", " active.table ")
				lookup, err := loadMetadataIdentityNodes(db, 7, []models.MetaItem{item})
				if err != nil {
					t.Fatal(err)
				}
				facts, err := projectDataItemAuthorizationFacts(item, time.Now(), lookup)
				if err != nil {
					t.Fatal(err)
				}
				binding, err = facts.ApprovedBinding()
				if err != nil {
					t.Fatal(err)
				}
				createdAt = item.CreatedAt
				itemFingerprint = item.Fingerprint
			} else {
				item := seedFileAuthorizationFacts(t, db, primaryID, kind)
				if kind == "geopackage" {
					item.Name = "panel.gpkg"
					item.FullName = "models/panel.gpkg"
					item.Fingerprint = commonmodels.GenerateItemFingerprint(item.EngineID, item.FullName)

					item.Attributes = models.JSONMap{"item": map[string]any{"layout": "single", "data_type": "container", "format": "geopackage", "refs": []map[string]any{{"path": item.FullName, "role": "container", "required": true, "primary": true}}}, "storage": map[string]any{"physical_path": item.FullName}, "format_info": map[string]any{"geopackage": map[string]any{"composition_complete": true}}}
					if err := db.Save(&item).Error; err != nil {
						t.Fatal(err)
					}
				}

				lookup, err := loadMetadataIdentityNodes(db, 7, []models.MetaItem{item})
				if err != nil {
					t.Fatal(err)
				}
				facts, err := projectDataItemAuthorizationFacts(item, time.Now(), lookup)
				if err != nil {
					t.Fatal(err)
				}
				binding, err = facts.ApprovedBinding()
				if err != nil {
					t.Fatal(err)
				}
				target, err = binding.PrimaryTarget()
				if err != nil {
					t.Fatal(err)
				}
				createdAt = item.CreatedAt
				itemFingerprint = item.Fingerprint
			}
			ref := production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
			receipt := production.Receipt{Reference: ref, TenantID: 7, Attempt: 1, TargetEngineVersion: 1, OperationID: uuid.NewString(), CommittedAt: time.Now(), Primary: target, Origin: &production.UserOrigin{PrincipalID: 31, MembershipID: 41, AuthorizationVersion: 1}, Sources: production.SourceEvidence{Kind: production.SourcesNone}, Objects: []production.CommittedObject{{Target: target, Created: true}}}
			if binding.FileComposition != nil {
				receipt.Objects = nil
				for _, member := range binding.FileComposition.Refs {
					receipt.Objects = append(receipt.Objects, production.CommittedObject{Target: member.Target, Created: true})
				}
			}
			req, err := productionScanRequest(receipt)
			if err != nil {
				t.Fatal(err)
			}
			profile := "nfs"
			if kind == "native" {
				profile = "postgresql"
			}
			run := scantask.NewManualExecution(7, 31, primaryID, 0, profile, req.Targets, nil, req.RefGroups, "basic", true, ref.Owner, createdAt.Add(-time.Second))
			run.ParentExecutionID = &ref.ExecutionID
			run.ActorPrincipalID = &receipt.Origin.PrincipalID
			run.ActorTenantMembershipID = &receipt.Origin.MembershipID
			run.IssuedAuthorizationVersion = &receipt.Origin.AuthorizationVersion
			tenant := uint(7)
			freezeScanEngineSnapshot(run, &commonmodels.Engine{ID: primaryID, TenantID: &tenant, Version: 1, EngineType: profile})
			run.ExecutionConfig["initial_production_receipt"] = receipt
			run.ExecutionConfig["initial_production_new_candidate"] = true
			run.Status = "success"
			svc := &ScanExecutionService{db: db}
			identified, err := svc.productionIdentification(t.Context(), db, run, 7, ref)
			if err != nil || identified.LogicalItemState != "new" || identified.Binding.ItemFingerprint != itemFingerprint {
				t.Fatalf("new item=%+v err=%v", identified, err)
			}
			run.ExecutionConfig["initial_production_new_candidate"] = false
			identified, err = svc.productionIdentification(t.Context(), db, run, 7, ref)
			if err != nil || identified.LogicalItemState != "existing" {
				t.Fatal("known logical item gained a new origin", identified, err)
			}
			run.ExecutionConfig["initial_production_new_candidate"] = true
			run.CreatedAt = createdAt.Add(time.Second)
			identified, err = svc.productionIdentification(t.Context(), db, run, 7, ref)
			if err != nil || identified.LogicalItemState != "existing" {
				t.Fatal("pre-existing item gained a new origin", identified, err)
			}
			run.CreatedAt = createdAt.Add(-time.Second)
			run.Status = "failed"
			identified, err = svc.productionIdentification(t.Context(), db, run, 7, ref)
			if err != nil || identified.Binding != nil || identified.LogicalItemState != "" {
				t.Fatal("failed scan claimed identity", identified, err)
			}
			run.Status = "success"
			receipt.Objects[0].Created = false
			run.ExecutionConfig["initial_production_receipt"] = receipt
			if _, err = svc.productionIdentification(t.Context(), db, run, 7, ref); err == nil {
				t.Fatal("old physical member qualified")
			}
		})
	}
}
func TestProductionIdentification(t *testing.T) {
	exerciseProductionIdentification(t, metatest.OpenMetadataDB(t))
}
