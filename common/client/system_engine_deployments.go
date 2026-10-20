package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EngineDeploymentReference contains only the current User-authorized management
// facts; no SQL endpoint, inventory, tenant Engine or credential is retained.
type EngineDeploymentReference struct {
	DeploymentID string `json:"deployment_id"`
	DisplayName string `json:"display_name"`
	EngineType string `json:"engine_type"`
	Version int64 `json:"version"`
	Enabled *bool `json:"enabled"`
	NodeID string `json:"node_id"`
	HostBindingState string `json:"host_binding_state"`
}

func (c *SystemServiceClient) GetEngineDeploymentsForUser(ctx context.Context, ids []string, token string) ([]EngineDeploymentReference,error) {
	invalid:=errors.New("engine deployment references require current User and canonical IDs")
	if c==nil || c.baseURL=="" || c.httpClient==nil || len(ids)==0 || len(ids)>100 || !strings.HasPrefix(token,"addp_at_") || len(token)<=8 || strings.ContainsAny(token," \t\r\n") { return nil,invalid }
	wanted:=map[string]bool{}
	for _,id:=range ids { parsed,err:=uuid.Parse(id); if err!=nil || parsed==uuid.Nil || parsed.String()!=id || wanted[id] { return nil,invalid }; wanted[id]=true }
	ctx,cancel:=context.WithTimeout(ctx,5*time.Second); defer cancel()
	result:=make([]EngineDeploymentReference,0,len(ids))
	for _,id:=range ids {
		var ref EngineDeploymentReference
		status,err:=c.doJSON(ctx,http.MethodGet,"/api/v1/system/platform/engine_deployments/"+id,token,nil,&ref,256<<10)
		if err!=nil { return nil,err }
		if status!=http.StatusOK || ref.DeploymentID!=id || ref.Version<1 || ref.Enabled==nil || ref.EngineType!="postgresql" { return nil,invalid }
		switch ref.HostBindingState { case "bound": parsed,err:=uuid.Parse(ref.NodeID); if err!=nil || parsed==uuid.Nil || parsed.String()!=ref.NodeID { return nil,invalid }; case "unbound","rejected": if ref.NodeID!="" { return nil,invalid }; default:return nil,invalid }
		result=append(result,ref)
	}
	return result,nil
}
