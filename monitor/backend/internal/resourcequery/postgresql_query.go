package resourcequery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

var postgresqlPhysical = map[string]string{
	"postgresql.connections.total": "connections",
	"postgresql.connections.active": "active_connections",
	"postgresql.connections.idle": "idle_connections",
	"postgresql.connections.idle_in_transaction": "idle_in_transaction_connections",
	"postgresql.connections.waiting": "active_waiting_connections",
	"postgresql.connections.lock_waiting": "active_lock_waiting_connections",
	"postgresql.connections.io_waiting": "active_io_waiting_connections",
	"postgresql.connections.maximum": "max_connections",
	"postgresql.transactions.committed_per_second": "transactions_committed_total",
	"postgresql.transactions.rolled_back_per_second": "transactions_rolled_back_total",
	"postgresql.deadlocks.per_second": "deadlocks_total",
	"postgresql.database.total_bytes": "database_bytes",
}

type PostgreSQLScope struct {
	DeploymentID, Instance string
	Source models.PostgreSQLMetricsSource
}

func (s PostgreSQLScope) Validate() error {
	id, err := uuid.Parse(s.DeploymentID)
	if err != nil || id == uuid.Nil || id.String() != s.DeploymentID || s.Instance == "" || len(s.Instance) > 512 || strings.ContainsAny(s.Instance, "\r\n") || s.Source.State != "available" || s.Source.Validate() != nil { return ErrInvalid }
	return nil
}

func (s PostgreSQLScope) selector(field string) string {
	metric := "addp_postgresql_"+field
	if field == "up" { metric = "up" }
	return metric + `{job="addp_nodes",addp_monitor_kind="engine_resources",addp_source="postgresql",addp_deployment_id=`+strconv.Quote(s.DeploymentID)+`,addp_database_container_id=`+strconv.Quote(s.Source.ContainerID)+`,addp_database_container_started_at=`+strconv.Quote(s.Source.StartedAt.UTC().Format(time.RFC3339Nano))+`,instance=`+strconv.Quote(s.Instance)+`}`
}

// snapshotGuard requires all non-nullable snapshot fields from the same scrape.
// A successful exporter HTTP response alone never proves a SQL snapshot.
func (s PostgreSQLScope) snapshotGuard(offset string) string {
	up, observed := s.selector("up")+offset, s.selector("sample_time_seconds")+offset
	stamp := `max(timestamp(`+up+`))`
	guard := ` and (count(`+up+`)==1) and (max(`+up+`)==1)`+
		` and (`+stamp+`>=time()-`+func() string { if offset == "" { return "16" }; return "76" }()+`)`
	for _, field := range []string{"sample_time_seconds", "server_start_time_seconds", "track_counts_enabled", "connections", "active_connections", "idle_connections", "idle_in_transaction_connections", "active_waiting_connections", "active_lock_waiting_connections", "active_io_waiting_connections", "max_connections", "database_bytes", "statistics_info"} {
		raw := s.selector(field)+offset
		guard += ` and (count(`+raw+`)==1) and (max(timestamp(`+raw+`))==`+stamp+`) and (min(`+raw+`)>=0)`
	}
	guard += ` and (max(`+observed+`)>=`+stamp+`-5) and (max(`+observed+`)<=`+stamp+`+5)`
	guard += ` and (max(`+s.selector("statistics_info")+offset+`)==1)`+
		` and ((max(`+s.selector("track_counts_enabled")+offset+`)==0) or (max(`+s.selector("track_counts_enabled")+offset+`)==1))`+
		` and (max(`+s.selector("server_start_time_seconds")+offset+`)>=`+strconv.FormatInt(s.Source.StartedAt.Unix()-1,10)+`)`
	return guard
}

func (s PostgreSQLScope) expression(p Plan) (string, error) {
	if s.Validate() != nil || len(p.Metrics) == 0 || len(p.Metrics) > len(postgresqlDefinitions) { return "", ErrInvalid }
	guard := s.snapshotGuard("")
	parts, seen := []string{}, map[string]bool{}
	for _, d := range p.Metrics {
		field, ok := postgresqlPhysical[d.Key]
		if !ok || seen[d.Key] { return "", ErrInvalid }; seen[d.Key] = true
		raw := s.selector(field)
		value, stamp := `max(`+raw+`)`, `max(timestamp(`+raw+`))`
		eligible := guard
		if d.WindowSeconds == 60 {
			previous := raw+` offset 1m`
			rate := `rate(`+raw+`[1m])`
			valid := `(`+rate+` and (count_over_time(`+raw+`[1m])>=4) and (resets(`+raw+`[1m])==0) and (`+raw+`>=`+previous+`) and (timestamp(`+previous+`)>=time()-76) and (timestamp(`+raw+`)>=time()-16) and (`+rate+`>=0))`
			// counter_epoch is part of the vector match; statistics_generation
			// and server start must also match across both window boundaries.
			eligible += s.snapshotGuard(" offset 1m")+` and (max(`+s.selector("track_counts_enabled")+`)==1) and (max(`+s.selector("track_counts_enabled")+` offset 1m)==1)`+
				` and (max(`+s.selector("server_start_time_seconds")+`)==max(`+s.selector("server_start_time_seconds")+` offset 1m))`+
				` and (count(`+s.selector("statistics_info")+` and `+s.selector("statistics_info")+` offset 1m)==1)`+
				` and (count(`+previous+`)==1) and (max(timestamp(`+previous+`))==max(timestamp(`+s.selector("up")+` offset 1m))) and (count(`+valid+`)==1)`
			value = `max(`+valid+`)`
		}
		eligible += ` and (count(`+raw+`)==1) and (min(`+raw+`)>=0) and (max(timestamp(`+raw+`))==max(timestamp(`+s.selector("up")+`)))`
		for _, item := range []struct{key, expression string}{{"value",value},{"sampled_at",stamp}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, item.expression, eligible, strconv.Quote(d.Key), strconv.Quote(item.key)))
		}
	}
	return strings.Join(parts," or "), nil
}

func (s PostgreSQLScope) collectionExpression() string {
	up := s.selector("up")
	parts := []string{}
	for _, item := range []struct{key, value string}{
		{"up_count", `count(`+up+`) or vector(0)`}, {"up",`max(`+up+`)`}, {"sampled_at",`max(timestamp(`+up+`))`},
		{"snapshot_valid",`(vector(1)`+s.snapshotGuard("")+`) or vector(0)`},
	} { parts=append(parts, `label_replace((`+item.value+`),"signal",`+strconv.Quote(item.key)+`,"","")`) }
	return strings.Join(parts," or ")
}

func normalizePostgreSQLCollection(data envelope, at time.Time) (Collection,error) {
	values, err := normalizeSignals(data,at,map[string]bool{"up_count":true,"up":true,"sampled_at":true,"snapshot_valid":true})
	if err != nil { return Collection{},err }
	result, err := normalizeUpEvidence(values,at)
	if err != nil || result.State != "collecting" { return result,err }
	valid, ok := values["snapshot_valid"]
	if !ok || (valid!=0 && valid!=1) { return Collection{},ErrUnavailable }
	if valid==0 { result.State="snapshot_unavailable" }
	return result,nil
}

// PostgreSQLQuery uses the shared transport/normalizer and fixed plans. POST
// carries read-only PromQL to avoid URL limits for the complete snapshot guard.
func (c *Client) PostgreSQLQuery(ctx context.Context, p Plan, scope PostgreSQLScope, b Budget) (Summary,error) {
	if b.Validate()!=nil || scope.Validate()!=nil { return Summary{},ErrInvalid }
	if c==nil { return Summary{},ErrUnavailable }
	expression,err := scope.expression(p); if err!=nil { return Summary{},err }
	ctx,cancel:=context.WithTimeout(ctx,time.Duration(b.TimeoutSeconds)*time.Second); defer cancel()
	endpoint,q:=planExpressionQuery(p,expression,b)
	data,err:=c.readForm(ctx,endpoint,q); if err!=nil { return Summary{},err }
	series,err:=normalize(data,p,b); if err!=nil { return Summary{},err }
	if p.Trend { return Summary{Series:series},nil }
	_,q=planExpressionQuery(p,scope.collectionExpression(),b)
	evidence,err:=c.readForm(ctx,"/api/v1/query",q); if err!=nil { return Summary{},err }
	collection,err:=normalizePostgreSQLCollection(evidence,p.End); if err!=nil { return Summary{},err }
	if collection.State!="collecting" { series=Empty(p,"no_data") }
	return Summary{Series:series,Collection:collection},nil
}

func (c *Client) PostgreSQLSummaries(ctx context.Context, scopes []PostgreSQLScope, at time.Time, b Budget) (map[string]Summary,error) {
	p,err:=NewPostgreSQLSummaryPlan(len(scopes),at,b); if err!=nil { return nil,err }
	if c==nil { return nil,ErrUnavailable }
	ctx,cancel:=context.WithTimeout(ctx,time.Duration(b.TimeoutSeconds)*time.Second); defer cancel()
	ids:=map[string]bool{}; metrics,collections:=[]string{},[]string{}
	for _,scope:=range scopes {
		if scope.Validate()!=nil || ids[scope.DeploymentID] { return nil,ErrInvalid }; ids[scope.DeploymentID]=true
		expression,err:=scope.expression(p); if err!=nil { return nil,err }
		label:=func(e string) string { return `label_replace((`+e+`),"addp_summary_engine",`+strconv.Quote(scope.DeploymentID)+`,"","")` }
		metrics=append(metrics,label(expression)); collections=append(collections,label(scope.collectionExpression()))
	}
	data,err:=c.readSummaryGroups(ctx,metrics,at,b,ids,2*len(p.Metrics),"addp_summary_engine"); if err!=nil { return nil,err }
	evidence,err:=c.readSummaryGroups(ctx,collections,at,b,ids,4,"addp_summary_engine"); if err!=nil { return nil,err }
	result:=map[string]Summary{}
	for _,scope:=range scopes {
		series,err:=normalize(data[scope.DeploymentID],p,b); if err!=nil { return nil,err }
		collection,err:=normalizePostgreSQLCollection(evidence[scope.DeploymentID],at); if err!=nil { return nil,err }
		if collection.State!="collecting" { series=Empty(p,"no_data") }
		result[scope.DeploymentID]=Summary{Series:series,Collection:collection}
	}
	return result,nil
}
