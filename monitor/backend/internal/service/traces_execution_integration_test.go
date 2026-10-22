package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/logger"
	"github.com/addp/common/models"
	"github.com/addp/monitor/internal/tracequery"
	"github.com/gin-gonic/gin"
)

// These routes exist only inside the standard gate's disposable child process.
// Owner PostgreSQL gates separately prove the real transaction and lease writes.
func installOwnedExecutionTraceRoutes(t *testing.T, router *gin.Engine) {
	path := filepath.Join(os.Getenv("TRACE_T2_WORK"), "execution-checkpoint.json")
	router.GET("/execution-stage/:phase", func(c *gin.Context) {
		phase := httptrace.ExecutionPhase(c.Param("phase"))
		item := &execution.TaskExecution{ExecutionConfig: models.JSONMap{"private_input": "never-observe-this"}, Metadata: models.JSONMap{"kept": "owner"}}
		if phase != httptrace.PhaseEnqueue {
			data, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(data, item) != nil {
				c.Status(500)
				return
			}
		}
		entry, err := execution.EnqueueTrace(item)
		if err != nil {
			c.Status(500)
			return
		}
		latest, err := execution.LatestTrace(item)
		if err != nil {
			c.Status(500)
			return
		}
		attempt := 1
		if phase == httptrace.PhaseEnqueue {
			attempt = 0
		}
		ctx, stage := httptrace.StartExecutionStage(c.Request.Context(), phase, attempt, entry, latest)
		ref := stage.Reference()
		if ref == nil {
			c.Status(500)
			return
		}
		if phase == httptrace.PhaseEnqueue {
			err = execution.FreezeEnqueueTrace(item, ref)
		}
		if err == nil {
			item.Metadata, err = execution.LatestTraceMetadata(item.Metadata, ref)
		}
		var encoded []byte
		if err == nil {
			encoded, err = json.Marshal(item)
		}
		if err == nil {
			err = os.WriteFile(path, encoded, 0600)
		}
		if err != nil {
			stage.End(httptrace.OutcomeRejected)
			c.Status(500)
			return
		}
		logger.L().InfoContext(ctx, "execution fixture checkpoint")
		outcome := httptrace.OutcomeSuccess
		if phase == httptrace.PhaseTerminal {
			outcome = httptrace.OutcomeFailed
		}
		stage.End(outcome)
		c.JSON(200, ref)
	})
}

type ownedExecutionStages struct {
	enqueue  *execution.TraceReference
	dispatch *execution.TraceReference
	terminal *execution.TraceReference
}

func runOwnedExecutionTraceStages(t *testing.T, first, resumed string) ownedExecutionStages {
	t.Helper()
	refs := make(map[httptrace.ExecutionPhase]*execution.TraceReference)
	for _, phase := range []httptrace.ExecutionPhase{httptrace.PhaseEnqueue, httptrace.PhaseClaim, httptrace.PhaseDispatch, httptrace.PhaseTerminal} {
		address := resumed
		if phase == httptrace.PhaseEnqueue {
			address = first
		}
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(address + "/execution-stage/" + string(phase))
		if err != nil {
			t.Fatal(err)
		}
		var ref execution.TraceReference
		err = json.NewDecoder(response.Body).Decode(&ref)
		response.Body.Close()
		if response.StatusCode != 200 || err != nil || ref.Validate() != nil {
			t.Fatalf("real execution stage %s: status=%d err=%v", phase, response.StatusCode, err)
		}
		for _, previous := range refs {
			if previous.TraceID == ref.TraceID {
				t.Fatal("execution phases shared a long trace")
			}
		}
		refs[phase] = &ref
	}
	if refs[httptrace.PhaseEnqueue].InstanceID == refs[httptrace.PhaseTerminal].InstanceID {
		t.Fatal("checkpoint was not resumed in another owned process")
	}
	return ownedExecutionStages{refs[httptrace.PhaseEnqueue], refs[httptrace.PhaseDispatch], refs[httptrace.PhaseTerminal]}
}

func assertOwnedExecutionTraceStages(t *testing.T, service *TraceService, users *ownedTraceUsers, refs ownedExecutionStages) {
	t.Helper()
	query := TraceQuery{InstanceID: 4, ID: refs.terminal.TraceID, Start: refs.terminal.StartedAt.Add(-time.Second), End: refs.terminal.StartedAt.Add(time.Second), Limit: 1}
	result, err := service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", query)
	if err != nil || len(result.Data) != 1 {
		t.Fatalf("failed execution phase not retained by real policy v2: %v", err)
	}
	detail := result.Data[0]
	if detail.Partial || len(detail.Spans) != 1 || len(detail.Edges) != 0 {
		t.Fatalf("causal link became HTTP edge or incomplete root: %+v", detail)
	}
	span := detail.Spans[0]
	if span.Scope != "addp.execution" || span.Phase != "terminal" || span.Outcome != "failed" || span.Attempt != 1 || !span.Failed || len(span.Links) != 2 {
		t.Fatalf("unsafe or missing execution projection: %+v", span)
	}
	expected := map[string]*execution.TraceReference{refs.enqueue.TraceID: refs.enqueue, refs.dispatch.TraceID: refs.dispatch}
	for _, link := range span.Links {
		ref := expected[link.TraceID]
		if ref == nil || link.SpanID != ref.SpanID || link.InstanceID != ref.InstanceID || !link.StartedAt.Equal(ref.StartedAt) || link.RuntimeInstanceID == 0 {
			t.Fatalf("persisted link lost identity/time: %+v", link)
		}
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("never-observe-this")) || bytes.Contains(encoded, []byte("private_input")) {
		t.Fatal("execution private config escaped projection")
	}
	// Revoking only the old linked instance must refuse the entire response,
	// even though no Span in this new Trace belongs to that instance.
	rows := users.rows
	users.rows = nil
	for _, row := range rows {
		if row.ID != 3 {
			users.rows = append(users.rows, row)
		}
	}
	denied, deniedErr := service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", query)
	users.rows = rows
	if !errors.Is(deniedErr, tracequery.ErrDenied) || len(denied.Data) != 0 {
		t.Fatalf("link target escaped current User authorization: %v", deniedErr)
	}
	// A target is independently sampled. A normal exact query may return
	// not-found; it must not invent data or recursively fetch its own links.
	linked, linkedErr := service.Query(context.Background(), "t2-user", "addp_at_t2_current_user", TraceQuery{InstanceID: 3, ID: refs.enqueue.TraceID, Start: refs.enqueue.StartedAt.Add(-time.Second), End: refs.enqueue.StartedAt.Add(time.Second), Limit: 1})
	if linkedErr != nil && !errors.Is(linkedErr, tracequery.ErrNotFound) {
		t.Fatalf("authorized standalone link query failed: %v", linkedErr)
	}
	if linkedErr == nil && len(linked.Data) > 1 {
		t.Fatal("link query exceeded its explicit limit")
	}
	logURL, err := url.Parse(os.Getenv("TRACE_T2_LOKI") + "/loki/api/v1/query_range")
	if err != nil {
		t.Fatal(err)
	}
	logURL.RawQuery = url.Values{"query": {`{module_name="system"} | trace_id="` + refs.terminal.TraceID + `"`}, "start": {fmt.Sprint(query.Start.UnixNano())}, "end": {fmt.Sprint(time.Now().UnixNano())}, "limit": {"10"}}.Encode()
	request, _ := http.NewRequest("GET", logURL.String(), nil)
	request.Header.Set("Authorization", "Bearer "+os.Getenv("LOKI_READ_TOKEN"))
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Contains(body, []byte(refs.terminal.SpanID)) || !bytes.Contains(body, []byte("execution fixture checkpoint")) {
		t.Fatalf("real execution log correlation missing: status=%d err=%v", response.StatusCode, err)
	}
	t.Log("real policy v2 retained failed short execution; independent roots, persisted cross-process links and linked-target revocation passed")
}
