package resourcequery

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type EngineScope struct {
	DeploymentID, Instance string
	EngineType             string
	Source                 models.EngineMetricsSource
}

func (s EngineScope) Validate() error {
	_, port := models.PlatformEngineService(s.EngineType)
	id, err := uuid.Parse(s.DeploymentID)
	if port == 0 || len(EngineCatalog(s.EngineType)) == 0 || err != nil || id == uuid.Nil || id.String() != s.DeploymentID || s.Instance == "" || len(s.Instance) > 512 || strings.ContainsAny(s.Instance, "\r\n") || s.Source.State != "available" || s.Source.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

func (s EngineScope) selector(field string) string {
	metric := "addp_" + s.EngineType + "_" + field
	if field == "up" {
		metric = "up"
	}
	return metric + `{job="addp_nodes",addp_monitor_kind="engine_resources",addp_source=` + strconv.Quote(s.EngineType) + `,addp_deployment_id=` + strconv.Quote(s.DeploymentID) + `,addp_database_container_id=` + strconv.Quote(s.Source.ContainerID) + `,addp_database_container_started_at=` + strconv.Quote(s.Source.StartedAt.UTC().Format(time.RFC3339Nano)) + `,instance=` + strconv.Quote(s.Instance) + `}`
}

func (s EngineScope) collectionExpression() string {
	up := s.selector("up")
	parts := []string{}
	for _, item := range []struct{ key, value string }{
		{"up_count", `count(` + up + `) or vector(0)`}, {"up", `max(` + up + `)`}, {"sampled_at", `max(timestamp(` + up + `))`},
		{"snapshot_valid", `(vector(1)` + s.snapshotGuard("") + `) or vector(0)`},
	} {
		parts = append(parts, `label_replace((`+item.value+`),"signal",`+strconv.Quote(item.key)+`,"","")`)
	}
	return strings.Join(parts, " or ")
}

func normalizeEngineCollection(data envelope, at time.Time) (Collection, error) {
	values, err := normalizeSignals(data, at, map[string]bool{"up_count": true, "up": true, "sampled_at": true, "snapshot_valid": true})
	if err != nil {
		return Collection{}, err
	}
	result, err := normalizeUpEvidence(values, at)
	if err != nil || result.State != "collecting" {
		return result, err
	}
	valid, ok := values["snapshot_valid"]
	if !ok || (valid != 0 && valid != 1) {
		return Collection{}, ErrUnavailable
	}
	if valid == 0 {
		result.State = "snapshot_unavailable"
	}
	return result, nil
}

// EngineQuery uses the shared transport/normalizer and fixed plans. POST
// carries read-only PromQL to avoid URL limits for the complete snapshot guard.
func (c *Client) EngineQuery(ctx context.Context, p Plan, scope EngineScope, b Budget) (Summary, error) {
	if b.Validate() != nil || scope.Validate() != nil {
		return Summary{}, ErrInvalid
	}
	if c == nil {
		return Summary{}, ErrUnavailable
	}
	expression, err := scope.expression(p)
	if err != nil {
		return Summary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	endpoint, q := planExpressionQuery(p, expression, b)
	data, err := c.readForm(ctx, endpoint, q)
	if err != nil {
		return Summary{}, err
	}
	series, err := normalize(data, p, b)
	if err != nil {
		return Summary{}, err
	}
	if p.Trend {
		return Summary{Series: series}, nil
	}
	_, q = planExpressionQuery(p, scope.collectionExpression(), b)
	evidence, err := c.readForm(ctx, "/api/v1/query", q)
	if err != nil {
		return Summary{}, err
	}
	collection, err := normalizeEngineCollection(evidence, p.End)
	if err != nil {
		return Summary{}, err
	}
	if collection.State != "collecting" {
		series = Empty(p, "no_data")
	}
	return Summary{Series: series, Collection: collection}, nil
}

func (c *Client) EngineSummaries(ctx context.Context, scopes []EngineScope, at time.Time, b Budget) (map[string]Summary, error) {
	if len(scopes) == 0 {
		return nil, ErrInvalid
	}
	p, err := NewEngineSummaryPlan(scopes[0].EngineType, len(scopes), at, b)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	ids := map[string]bool{}
	metrics, collections := []string{}, []string{}
	for _, scope := range scopes {
		if scope.Validate() != nil || ids[scope.DeploymentID] {
			return nil, ErrInvalid
		}
		ids[scope.DeploymentID] = true
		rowPlan, err := NewEngineSummaryPlan(scope.EngineType, 1, at, b)
		if err != nil {
			return nil, err
		}
		expression, err := scope.expression(rowPlan)
		if err != nil {
			return nil, err
		}
		label := func(e string) string {
			return `label_replace((` + e + `),"addp_summary_engine",` + strconv.Quote(scope.DeploymentID) + `,"","")`
		}
		metrics = append(metrics, label(expression))
		collections = append(collections, label(scope.collectionExpression()))
	}
	data, err := c.readSummaryGroups(ctx, metrics, at, b, ids, 2*len(p.Metrics), "addp_summary_engine")
	if err != nil {
		return nil, err
	}
	evidence, err := c.readSummaryGroups(ctx, collections, at, b, ids, 4, "addp_summary_engine")
	if err != nil {
		return nil, err
	}
	result := map[string]Summary{}
	for _, scope := range scopes {
		rowPlan, err := NewEngineSummaryPlan(scope.EngineType, 1, at, b)
		if err != nil {
			return nil, err
		}
		series, err := normalize(data[scope.DeploymentID], rowPlan, b)
		if err != nil {
			return nil, err
		}
		collection, err := normalizeEngineCollection(evidence[scope.DeploymentID], at)
		if err != nil {
			return nil, err
		}
		if collection.State != "collecting" {
			series = Empty(rowPlan, "no_data")
		}
		result[scope.DeploymentID] = Summary{Series: series, Collection: collection}
	}
	return result, nil
}

func (s EngineScope) snapshotGuard(offset string) string {
	if s.EngineType == "redis" {
		return s.redisSnapshotGuard(offset)
	}
	if s.EngineType == "minio" {
		return s.minioSnapshotGuard(offset)
	}
	return s.postgresqlSnapshotGuard(offset)
}
func (s EngineScope) expression(p Plan) (string, error) {
	if s.Validate() != nil {
		return "", ErrInvalid
	}
	if s.EngineType == "redis" {
		return s.redisExpression(p)
	}
	if s.EngineType == "minio" {
		return s.minioExpression(p)
	}
	return s.postgresqlExpression(p)
}
