package service

import (
	"testing"

	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
)

func TestPlatformAlertEvaluationRequiresExplicitSourceAndVerdict(t *testing.T) {
	valid := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryLog, SubjectKind: models.PlatformAlertSubjectLogObserver, SubjectID: "observer-1", Signal: "delivery_probe", Severity: "critical", Verdict: PlatformAlertRisk}
	for _, tc := range []struct {
		name   string
		mutate func(*PlatformAlertEvaluation)
		want   bool
	}{
		{"log observer", func(e *PlatformAlertEvaluation) {}, true},
		{"host resource", func(e *PlatformAlertEvaluation) {
			e.Category, e.SubjectKind, e.SubjectID = models.PlatformAlertCategoryResource, models.PlatformAlertSubjectHostNode, uuid.NewString()
		}, true},
		{"unknown cannot restore", func(e *PlatformAlertEvaluation) { e.Verdict, e.Severity = PlatformAlertUnknown, "" }, true},
		{"paused cannot restore", func(e *PlatformAlertEvaluation) { e.Verdict, e.Severity = PlatformAlertPaused, "" }, true},
		{"missing category", func(e *PlatformAlertEvaluation) { e.Category = "" }, false},
		{"unknown source", func(e *PlatformAlertEvaluation) { e.Category = "tenant" }, false},
		{"crossed subject kind", func(e *PlatformAlertEvaluation) { e.SubjectKind = models.PlatformAlertSubjectHostNode }, false},
		{"name is not host UUID", func(e *PlatformAlertEvaluation) {
			e.Category, e.SubjectKind = models.PlatformAlertCategoryResource, models.PlatformAlertSubjectHostNode
		}, false},
		{"empty subject", func(e *PlatformAlertEvaluation) { e.SubjectID = "" }, false},
		{"empty signal", func(e *PlatformAlertEvaluation) { e.Signal = "" }, false},
		{"missing verdict", func(e *PlatformAlertEvaluation) { e.Verdict = "" }, false},
		{"unsupported severity", func(e *PlatformAlertEvaluation) { e.Severity = "fatal" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := valid
			tc.mutate(&e)
			if e.valid() != tc.want {
				t.Fatal("source boundary validation did not match contract")
			}
		})
	}
}
