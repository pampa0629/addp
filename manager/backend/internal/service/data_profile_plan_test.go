package service

import (
	"testing"
	"time"

	"github.com/addp/manager/internal/dataprofile"
)

func TestDataProfilePagePositionsAreBoundedAndSystematic(t *testing.T) {
	for _, count := range []*int64{nil, profilePageCount(0), profilePageCount(1), profilePageCount(600), profilePageCount(10000), profilePageCount(1000000)} {
		for _, kind := range []string{dataprofile.DataScopeKindAll, dataprofile.DataScopeKindCondition} {
			pages, err := dataProfilePagePositions(count, dataprofile.DataScope{Kind: kind}, DefaultDataProfileBudget)
			if err != nil || len(pages) == 0 {
				t.Fatalf("no prepared pages: %v", err)
			}
			total := 0
			for index, page := range pages {
				if page.Offset < 0 || page.Limit <= 0 || page.Limit > DefaultDataProfileBudget.PageSize ||
					(index > 0 && page.Offset < pages[index-1].Offset+pages[index-1].Limit) {
					t.Fatalf("invalid page position: %#v", pages)
				}
				total += page.Limit
			}
			if total > DefaultDataProfileBudget.MaxRowsScanned {
				t.Fatal("read budget expanded")
			}
			if kind == dataprofile.DataScopeKindCondition || count == nil || *count == 0 {
				if pages[1].Offset != DefaultDataProfileBudget.PageSize {
					t.Fatal("unknown count fabricated a full-range spread")
				}
			} else if *count == 1000000 && pages[len(pages)-1].Offset != 999500 {
				t.Fatal("systematic pages did not span the observed row range")
			}
		}
	}
	budget := DataProfileBudget{SampleSize: 3, MaxRowsScanned: 7, PageSize: 3, Timeout: time.Second}
	pages, err := dataProfilePagePositions(nil, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget)
	if err != nil || len(pages) != 3 || pages[2].Limit != 1 || pages[2].Offset != 6 {
		t.Fatal("final page exceeded remaining budget")
	}
}

func TestDataProfilePagePositionsRejectInvalidBudgetsAndOverflow(t *testing.T) {
	for _, budget := range []DataProfileBudget{
		{}, {SampleSize: 1, MaxRowsScanned: 10001, PageSize: 500, Timeout: time.Second},
		{SampleSize: 1, MaxRowsScanned: 5000, PageSize: 2001, Timeout: time.Second},
		{SampleSize: 1, MaxRowsScanned: 10, PageSize: 5, Timeout: time.Microsecond},
	} {
		if _, err := dataProfilePagePositions(nil, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	for _, count := range []int64{-1, 1<<63 - 1} {
		if _, err := dataProfilePagePositions(&count, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, DefaultDataProfileBudget); err == nil {
			t.Fatal("invalid or overflowing row range accepted")
		}
	}
}

func profilePageCount(count int64) *int64 { return &count }

func TestDataProfileSamplePlanMissingPreparationDoesNotProduceScope(t *testing.T) {
	for _, plan := range []*DataProfileSamplePlan{nil, {}} {
		if scope, err := plan.SourceReadScope([]byte(`{"version":"data-profile-config/v6"}`)); err == nil || scope != nil {
			t.Fatal("unprepared plan produced a source scope")
		}
	}
}
