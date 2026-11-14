package service

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/monitor/internal/models"
	"strconv"
	"testing"
	"time"
)

func TestCDCPerformanceWindowsUseDistinctSamplesAndSeparatePause(t *testing.T) {
	start := time.Now().UTC()
	makeRow := func(seconds int, offset, lag int64) *commonClient.TransferCDCRiskItem {
		at := start.Add(time.Duration(seconds) * time.Second)
		return &commonClient.TransferCDCRiskItem{State: "observing", InitialSnapshotCompleted: performanceSnapshotCompleted(), Generation: 1, ConsumptionState: "running", BufferSample: &commonClient.CDCBufferSample{CommittedOffset: strconv.FormatInt(offset, 10), LagOffsets: strconv.FormatInt(lag, 10), SampledAt: at, ExpiresAt: at.Add(30 * time.Second)}}
	}
	rule := models.AlertRule{RuleType: models.AlertRuleCDCStalled}
	feed := func(row *commonClient.TransferCDCRiskItem) {
		rule.CDCWindow = advanceCDCWindow(rule, *row, row.BufferSample.SampledAt)
	}
	for seconds := 0; seconds <= 165; seconds += 15 {
		feed(makeRow(seconds, 10, 3))
	}
	if rule.CDCWindow.Active {
		t.Fatal("opened before 180s")
	}
	duplicate := makeRow(165, 10, 3)
	rule.CDCWindow = advanceCDCWindow(rule, *duplicate, start.Add(179*time.Second))
	if rule.CDCWindow.Active {
		t.Fatal("duplicate sample accumulated evaluator time")
	}
	feed(makeRow(180, 10, 3))
	if !rule.CDCWindow.Active {
		t.Fatal("180 seconds failed to open")
	}
	paused := makeRow(195, 10, 3)
	paused.ConsumptionState = "paused"
	feed(paused)
	if !rule.CDCWindow.Active || rule.CDCWindow.State != "paused" || rule.CDCWindow.TriggerSince != nil {
		t.Fatal("pause resolved or retained timer")
	}
	unknown := makeRow(210, 10, 3)
	unknown.State = "unknown"
	feed(unknown)
	if !rule.CDCWindow.Active || rule.CDCWindow.State != "unknown" {
		t.Fatal("unknown resolved")
	}
	for seconds := 225; seconds <= 300; seconds += 15 {
		feed(makeRow(seconds, int64(seconds), 3))
	}
	if rule.CDCWindow.Active {
		t.Fatal("sustained progress did not recover")
	}
	feed(makeRow(315, 315, 3))
	feed(makeRow(360, 315, 3))
	if rule.CDCWindow.TriggerSince == nil || !rule.CDCWindow.TriggerSince.Equal(start.Add(360*time.Second)) {
		t.Fatal("expired gap bridged trigger")
	}
	feed(makeRow(375, 315, 0))
	if rule.CDCWindow.Active || rule.CDCWindow.State != "healthy" {
		t.Fatal("zero backlog did not recover")
	}
}

func TestCDCDelayNeedsNewEventsAndFreshRecovery(t *testing.T) {
	start := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCDelayed}
	feed := func(seconds int, count string, delay float64, paused bool) {
		at := start.Add(time.Duration(seconds) * time.Second)
		row := commonClient.TransferCDCRiskItem{State: "observing", InitialSnapshotCompleted: performanceSnapshotCompleted(), Generation: 1, ConsumptionState: "running", BufferSample: &commonClient.CDCBufferSample{SampledAt: at, ExpiresAt: at.Add(30 * time.Second)}, DelaySample: &commonClient.CDCDelaySample{EventCount: count, DelaySeconds: delay, LastEventAgeSeconds: 1, SampledAt: at, ExpiresAt: at.Add(30 * time.Second)}}
		if paused {
			row.ConsumptionState = "paused"
		}
		rule.CDCWindow = advanceCDCWindow(rule, row, at)
	}
	for seconds := 0; seconds <= 240; seconds += 15 {
		feed(seconds, "1", 80, false)
	}
	if rule.CDCWindow.Active || rule.CDCWindow.TriggerSince != nil {
		t.Fatal("same event created sustained delay")
	}
	for seconds := 255; seconds <= 450; seconds += 15 {
		feed(seconds, strconv.Itoa(seconds), 80, true)
	}
	if !rule.CDCWindow.Active {
		t.Fatal("target pause suppressed capture delay")
	}
	feed(465, "450", 80, false)
	if !rule.CDCWindow.Active || rule.CDCWindow.State != "idle" {
		t.Fatal("idle closed existing delay")
	}
	for seconds := 480; seconds <= 540; seconds += 15 {
		feed(seconds, strconv.Itoa(seconds), 20, false)
	}
	if rule.CDCWindow.Active {
		t.Fatal("fresh low delay did not recover")
	}
	feed(555, "1", 80, false)
	if rule.CDCWindow.TriggerSince != nil || rule.CDCWindow.State != "unknown" {
		t.Fatal("counter reset counted as activity")
	}
}

func TestCDCBacklogThresholdExactnessAndRecoveryHysteresis(t *testing.T) {
	start := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCBacklog, BacklogThreshold: "9007199254740993"}
	feed := func(seconds int, lag string) {
		at := start.Add(time.Duration(seconds) * time.Second)
		row := commonClient.TransferCDCRiskItem{State: "observing", InitialSnapshotCompleted: performanceSnapshotCompleted(), Generation: 1, ConsumptionState: "running", BufferSample: &commonClient.CDCBufferSample{SampledAt: at, ExpiresAt: at.Add(30 * time.Second), CommittedOffset: "0", LagOffsets: lag}}
		rule.CDCWindow = advanceCDCWindow(rule, row, at)
	}
	for seconds := 0; seconds <= 300; seconds += 15 {
		feed(seconds, "9007199254740994")
	}
	if !rule.CDCWindow.Active {
		t.Fatal("backlog window did not open")
	}
	for seconds := 315; seconds <= 390; seconds += 15 {
		feed(seconds, "5000000000000000")
	}
	if !rule.CDCWindow.Active {
		t.Fatal("middle band closed backlog")
	}
	for seconds := 405; seconds <= 465; seconds += 15 {
		feed(seconds, "4503599627370496")
	}
	if rule.CDCWindow.Active {
		t.Fatal("exact half threshold recovery failed")
	}
	for _, value := range []string{"0", "01", "-1", "1.5", "9223372036854775808"} {
		if normalizeCDCBacklog(rule.RuleType, value) == nil {
			t.Fatal("invalid backlog threshold", value)
		}
	}
}

func TestCDCPerformanceGenerationAndUnknownCannotOpenNewGeneration(t *testing.T) {
	now := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCDelayed, CDCWindow: models.CDCPerformanceWindow{Generation: 1, Active: true, PreviousValue: "100"}}
	row := commonClient.TransferCDCRiskItem{State: "observing", InitialSnapshotCompleted: performanceSnapshotCompleted(), Generation: 2, ConsumptionState: "running", BufferSample: &commonClient.CDCBufferSample{SampledAt: now, ExpiresAt: now.Add(time.Minute)}, DelaySample: &commonClient.CDCDelaySample{EventCount: "1", DelaySeconds: 80, LastEventAgeSeconds: 1, SampledAt: now, ExpiresAt: now.Add(time.Minute)}}
	next := advanceCDCWindow(rule, row, now)
	if next.Active || next.TriggerSince != nil {
		t.Fatal("old generation opened new warning", next)
	}
	row.State = "unknown"
	rule.CDCWindow = next
	if next = advanceCDCWindow(rule, row, now); next.State != "unknown" || next.TriggerSince != nil {
		t.Fatal(next)
	}
}

func performanceSnapshotCompleted() *bool { v := true; return &v }

func TestCDCDelayIndependentOfBufferAvailabilityAndClockRegression(t *testing.T) {
	now := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCDelayed}
	row := commonClient.TransferCDCRiskItem{State: "observing", Generation: 1, ConsumptionState: "running", InitialSnapshotCompleted: performanceSnapshotCompleted(), DelaySample: &commonClient.CDCDelaySample{DelaySeconds: 80, EventCount: "1", LastEventAgeSeconds: 1, SampledAt: now, ExpiresAt: now.Add(30 * time.Second)}}
	rule.CDCWindow = advanceCDCWindow(rule, row, now)
	for sec := 15; sec <= 195; sec += 15 {
		row.DelaySample.SampledAt = now.Add(time.Duration(sec) * time.Second)
		row.DelaySample.ExpiresAt = row.DelaySample.SampledAt.Add(30 * time.Second)
		row.DelaySample.EventCount = strconv.Itoa(sec)
		rule.CDCWindow = advanceCDCWindow(rule, row, row.DelaySample.SampledAt)
	}
	if !rule.CDCWindow.Active {
		t.Fatal("buffer query failure hid capture delay", rule.CDCWindow)
	}
	row.DelaySample.SampledAt = now.Add(190 * time.Second)
	if w := advanceCDCWindow(rule, row, now.Add(195*time.Second)); w.State != "unknown" || w.TriggerSince != nil {
		t.Fatal("backwards clock accepted", w)
	}
}

func TestCDCMissingRoundRejectsAnOldButUnexpiredSample(t *testing.T) {
	now := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCStalled}
	unknown := commonClient.TransferCDCRiskItem{State: "unknown"}
	rule.CDCWindow = advanceCDCWindow(rule, unknown, now)
	row := commonClient.TransferCDCRiskItem{State: "observing", Generation: 1, ConsumptionState: "running", InitialSnapshotCompleted: performanceSnapshotCompleted(), BufferSample: &commonClient.CDCBufferSample{CommittedOffset: "10", LagOffsets: "3", SampledAt: now.Add(-time.Second), ExpiresAt: now.Add(29 * time.Second)}}
	w := advanceCDCWindow(rule, row, now.Add(time.Second))
	if w.State != "unknown" || w.TriggerSince != nil {
		t.Fatal("pre-gap sample restarted timer", w)
	}
}

func TestCDCPerformanceThresholdEqualityAndSchemaPause(t *testing.T) {
	now := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCDelayed}
	for sec := 0; sec <= 240; sec += 15 {
		at := now.Add(time.Duration(sec) * time.Second)
		row := commonClient.TransferCDCRiskItem{State: "observing", Generation: 1, ConsumptionState: "running", InitialSnapshotCompleted: performanceSnapshotCompleted(), DelaySample: &commonClient.CDCDelaySample{DelaySeconds: 60, EventCount: strconv.Itoa(sec + 1), LastEventAgeSeconds: 1, SampledAt: at, ExpiresAt: at.Add(30 * time.Second)}}
		rule.CDCWindow = advanceCDCWindow(rule, row, at)
	}
	if rule.CDCWindow.Active || rule.CDCWindow.TriggerSince != nil {
		t.Fatal("exactly 60 seconds triggered")
	}
	rule = models.AlertRule{RuleType: models.AlertRuleCDCStalled, CDCWindow: models.CDCPerformanceWindow{Generation: 1, Active: true, TriggerSince: &now}}
	row := commonClient.TransferCDCRiskItem{State: "observing", Generation: 1, ConsumptionState: "blocked"}
	w := advanceCDCWindow(rule, row, now.Add(time.Second))
	if w.State != "paused" || !w.Active || w.TriggerSince != nil {
		t.Fatal("schema block did not pause consumer evaluation", w)
	}
}

func TestCDCConsumerRecoveryAllowsBatchedCommitsButRequiresNewProgress(t *testing.T) {
	now := time.Now().UTC()
	rule := models.AlertRule{RuleType: models.AlertRuleCDCStalled, CDCWindow: models.CDCPerformanceWindow{Generation: 1, Active: true, PreviousValue: "10"}}
	feed := func(sec int, offset string) {
		at := now.Add(time.Duration(sec) * time.Second)
		row := commonClient.TransferCDCRiskItem{State: "observing", Generation: 1, ConsumptionState: "running", InitialSnapshotCompleted: performanceSnapshotCompleted(), BufferSample: &commonClient.CDCBufferSample{CommittedOffset: offset, LagOffsets: "3", SampledAt: at, ExpiresAt: at.Add(30 * time.Second)}}
		rule.CDCWindow = advanceCDCWindow(rule, row, at)
	}
	feed(0, "10")
	feed(15, "11")
	feed(30, "11")
	feed(45, "12")
	feed(60, "12")
	if !rule.CDCWindow.Active {
		t.Fatal("recovery before a 60s window")
	}
	feed(75, "13")
	if rule.CDCWindow.Active {
		t.Fatal("normal 30s batches never recovered")
	}
	rule.CDCWindow.Active = true
	feed(90, "14")
	for sec := 105; sec <= 285; sec += 15 {
		feed(sec, "14")
	}
	feed(300, "15")
	if !rule.CDCWindow.Active {
		t.Fatal("one pulse after another 180s stall falsely recovered")
	}
}
