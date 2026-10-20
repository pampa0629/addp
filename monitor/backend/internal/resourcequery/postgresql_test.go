package resourcequery

import (
	"errors"
	"testing"
	"time"
)

func TestPostgreSQLCatalogAndBoundedPlans(t *testing.T) {
	keys := []string{}
	rates := 0
	for _, d := range PostgreSQLCatalog() {
		keys = append(keys, d.Key)
		if d.WindowSeconds != 0 {
			if d.WindowSeconds != 60 {
				t.Fatal("unexpected rate window", d)
			}
			rates++
		}
	}
	if len(keys) != 12 || rates != 3 {
		t.Fatal("PostgreSQL first catalog must fit the existing metric budget", keys, rates)
	}
	copy := PostgreSQLCatalog()
	copy[0].Key = "mutated"
	if PostgreSQLCatalog()[0].Key == "mutated" {
		t.Fatal("caller changed the fixed catalog")
	}
	p, err := NewPostgreSQLPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, DefaultBudget())
	if err != nil || p.Points > 1000 || p.Points*len(keys) > 20000 || p.StepSeconds%15 != 0 {
		t.Fatal("PostgreSQL trend bypassed the existing budget", p, err)
	}
	if _, err := NewPostgreSQLPlan(keys, testTime.Add(-7*24*time.Hour-time.Second), testTime, testTime, true, DefaultBudget()); !errors.Is(err, ErrBudget) {
		t.Fatal("seven-day range limit bypassed", err)
	}
	for _, keys := range [][]string{nil, {"up"}, {"node.memory.total_bytes"}, {"postgresql.connections.total", "postgresql.connections.total"}, {`postgresql.connections.total} or up{`}} {
		if _, err := NewPostgreSQLPlan(keys, testTime, testTime, testTime, false, DefaultBudget()); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid PostgreSQL metric accepted", keys, err)
		}
	}
	if _, err := NewPlan([]string{"postgresql.connections.total"}, testTime, testTime, testTime, false, nil, DefaultBudget()); !errors.Is(err, ErrInvalid) {
		t.Fatal("node metric API accepted PostgreSQL catalog", err)
	}
}

func TestPostgreSQLSummaryReservesAllRequestedRows(t *testing.T) {
	b := DefaultBudget()
	if _, err := NewPostgreSQLSummaryPlan(20, testTime, b); err != nil {
		t.Fatal("normal first page rejected", err)
	}
	for _, count := range []int{0, -1, 101} {
		if _, err := NewPostgreSQLSummaryPlan(count, testTime, b); !errors.Is(err, ErrInvalid) {
			t.Fatal(count, err)
		}
	}
	for _, count := range []int{26, 100} {
		if _, err := NewPostgreSQLSummaryPlan(count, testTime, b); !errors.Is(err, ErrBudget) {
			t.Fatal("summary silently truncated rows", count, err)
		}
	}
	b.MaxMetrics = 3
	if _, err := NewPostgreSQLSummaryPlan(1, testTime, b); !errors.Is(err, ErrBudget) {
		t.Fatal("summary silently omitted a metric", err)
	}
	b = DefaultBudget()
	b.MaxTotalPoints = 7
	if _, err := NewPostgreSQLSummaryPlan(2, testTime, b); !errors.Is(err, ErrBudget) {
		t.Fatal("summary total output budget bypassed", err)
	}
}
