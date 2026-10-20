package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"net/netip"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type engineUsers struct { row client.EngineDeploymentReference; err error; calls int; tokens []string }
func (u *engineUsers) GetEngineDeploymentsForUser(_ context.Context, ids []string, token string) ([]client.EngineDeploymentReference,error) {
	u.calls++; u.tokens=append(u.tokens,token); if u.err!=nil { return nil,u.err }
	var row client.EngineDeploymentReference; raw,_:=json.Marshal(u.row); json.Unmarshal(raw,&row)
	return []client.EngineDeploymentReference{row},nil
}
type engineBackend struct { calls int; complete func(); plan resourcequery.Plan }
func (b *engineBackend) PostgreSQLQuery(_ context.Context,p resourcequery.Plan,_ resourcequery.PostgreSQLScope,_ resourcequery.Budget) (resourcequery.Summary,error) {
	b.calls++; b.plan=p
	if b.complete!=nil { b.complete() }
	series:=resourcequery.Empty(p,"no_data")
	for i:=range series { for j:=range series[i].Points { point:=&series[i].Points[j]; value:=float64(0); stamp:=point.EvaluatedAt; point.Value=&value; point.SampledAt=&stamp; point.DataState="valid" } }
	at:=p.End
	return resourcequery.Summary{Series:series,Collection:resourcequery.Collection{State:"collecting",SampledAt:&at}},nil
}
func (b *engineBackend) PostgreSQLSummaries(ctx context.Context,scopes []resourcequery.PostgreSQLScope,at time.Time,budget resourcequery.Budget) (map[string]resourcequery.Summary,error) {
	p,err:=resourcequery.NewPostgreSQLSummaryPlan(len(scopes),at,budget); if err!=nil { return nil,err }
	row,err:=b.PostgreSQLQuery(ctx,p,scopes[0],budget); return map[string]resourcequery.Summary{scopes[0].DeploymentID:row},err
}

func TestEngineObservationRevocationAndRuntimeChangesDiscardAllValues(t *testing.T) {
	for _,change:=range []string{"valid","denied-before","denied-after","version","runtime","source","policy","disabled"} { t.Run(change,func(t *testing.T){
		now:=time.Now().UTC().Truncate(time.Second); started:=now.Add(-2*time.Minute); yes:=true
		users:=&engineUsers{row:client.EngineDeploymentReference{DeploymentID:uuid.NewString(),DisplayName:"Infra PostgreSQL",EngineType:"postgresql",Version:1,Enabled:&yes,NodeID:uuid.NewString(),HostBindingState:"bound"}}
		identity:=commonmodels.ObservabilityEngineIdentity{DeploymentID:users.row.DeploymentID,Version:1,Scope:"infra",EngineType:"postgresql",NodeID:users.row.NodeID,SourceKey:commonmodels.ObservabilityEngineSourceKey{DockerEngineID:"fixture",Project:"addp-infra",Service:"postgres"},Metrics:&commonmodels.PostgreSQLMetricsSource{State:"available",Endpoint:"https://127.0.0.1:9100/metrics",ContainerID:strings.Repeat("a",64),StartedAt:&started}}
		reader:=&processIdentities{snapshot:&commonmodels.ObservabilityIdentitySnapshot{ObservedAt:now,Nodes:[]commonmodels.ObservabilityNodeIdentity{{NodeID:users.row.NodeID,Version:1}},EngineDeployments:[]commonmodels.ObservabilityEngineIdentity{identity}}}
		policies:=&resourcePolicies{row:models.ResourceQueryPolicy{Budget:resourcequery.DefaultBudget(),Version:1}}
		backend:=&engineBackend{}
		svc:=NewResourceObservationService(policies,nil,nil,nil,true,resourceSourcePolicyWithNetwork(t,[]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},[]uint16{9100}),ProcessObservationDependencies{}).WithEngines(EngineObservationDependencies{Users:users,Identities:reader,Backend:backend})
		svc.now=func() time.Time { return now }
		if change=="denied-before" { users.err=&client.SystemAPIError{StatusCode:403} }
		if change=="disabled" { yes=false }
		backend.complete=func(){ switch change {
		case "denied-after":users.err=&client.SystemAPIError{StatusCode:403}
		case "version":users.row.Version++
		case "runtime": next:=*reader.snapshot.EngineDeployments[0].Metrics; next.ContainerID=strings.Repeat("b",64); reader.snapshot.EngineDeployments[0].Metrics=&next
		case "source": next:=*reader.snapshot.EngineDeployments[0].Metrics; next.Endpoint="https://127.0.0.2:9100/metrics"; reader.snapshot.EngineDeployments[0].Metrics=&next
		case "policy":policies.row.Version++
		} }
		result,err:=svc.EngineResources(context.Background(),"human","addp_at_current_user",[]string{users.row.DeploymentID},nil,time.Time{},time.Time{},false,true)
		if change=="valid" || change=="disabled" {
			if err!=nil || len(result.Data)!=1 { t.Fatalf("valid read: %+v %v",result,err) }
			if change=="valid" && (backend.calls!=1 || result.Data[0].Series[0].Points[0].Value==nil || *result.Data[0].Series[0].Points[0].Value!=0) { t.Fatal("valid zero lost") }
			if change=="disabled" && (backend.calls!=0 || result.Data[0].Collection.State!="not_active") { t.Fatal("disabled deployment queried history") }
		} else if err==nil || len(result.Data)!=0 { t.Fatalf("changed authority returned data: %+v %v",result,err) }
		if change=="denied-before" && (reader.calls!=0 || backend.calls!=0) { t.Fatal("private read before user authorization") }
		for _,token:=range users.tokens { if token!="addp_at_current_user" { t.Fatal("User replaced by machine credential") } }
	}) }
}
