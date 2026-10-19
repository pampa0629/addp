package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var deploymentDockerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$`)
var deploymentContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrDeploymentSourceChanged = errors.New("deployment discovery source changed or unavailable")

type EngineDeploymentService struct {
	repo *repository.EngineDeploymentRepository
}

func NewEngineDeploymentService(repo *repository.EngineDeploymentRepository) *EngineDeploymentService {
	return &EngineDeploymentService{repo: repo}
}

func validateEngineDeployment(input models.EngineDeploymentInput) (models.EngineDeployment, error) {
	bad := func() (models.EngineDeployment, error) { return models.EngineDeployment{}, commonapi.ErrBadRequest }
	name := strings.TrimSpace(input.DisplayName)
	if name == "" || utf8.RuneCountInString(name) > 255 || strings.IndexFunc(name, unicode.IsControl) >= 0 || input.Enabled == nil || validateNodeID(input.DeclaredNodeID) != nil {
		return bad()
	}
	i := input.Inventory
	encoded, err := json.Marshal(i)
	if err != nil || len(encoded) > 16*1024 || i.Schema != "addp.engine-deployment-inventory/v1" || len(i.Services) != 1 || i.Source.Type != "docker_compose" || (i.Source.Project != "business" && i.Source.Project != "addp-infra") || !deploymentDockerID.MatchString(i.Source.DockerEngineID) || (i.Source.Runtime != "linux_docker" && i.Source.Runtime != "desktop_linux_vm") {
		return bad()
	}
	if at, err := time.Parse(time.RFC3339, i.ObservedAt); err != nil || at.Format("2006-01-02T15:04:05Z") != i.ObservedAt {
		return bad()
	}
	service := i.Services[0]
	if service.Service != "postgres" || service.EngineType != "postgresql" || len(service.Containers) != 1 {
		return bad()
	}
	container := service.Containers[0]
	started, err := time.Parse(time.RFC3339Nano, container.StartedAt)
	if err != nil || started.Year() < 1000 || !strings.HasSuffix(container.StartedAt, "Z") || !deploymentContainerID.MatchString(container.ContainerID) || container.RestartCount < 0 || len(container.PublishedPorts) < 1 || len(container.PublishedPorts) > 8 {
		return bad()
	}
	seen := map[models.EngineDeploymentPort]bool{}
	for _, port := range container.PublishedPorts {
		ip, err := netip.ParseAddr(port.Address)
		if err != nil || ip.Zone() != "" || port.Port < 1 || port.Port > 65535 || port.ContainerPort != 5432 || port.Protocol != "tcp" {
			return bad()
		}
		canonical := port
		canonical.Address = ip.String()
		if seen[canonical] {
			return bad()
		}
		seen[canonical] = true
	}
	return models.EngineDeployment{DisplayName: name, EngineType: "postgresql", Enabled: *input.Enabled, Inventory: i, DeclaredNodeID: input.DeclaredNodeID}, nil
}

func (s *EngineDeploymentService) Get(ctx context.Context, id string) (*models.EngineDeployment, error) {
	if err := validateNodeID(id); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}
func (s *EngineDeploymentService) List(ctx context.Context, page, size int, filter models.EngineDeploymentFilter) (*models.EngineDeploymentPage, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 || utf8.RuneCountInString(filter.Search) > 255 || strings.IndexFunc(filter.Search, unicode.IsControl) >= 0 || (filter.NodeID != "" && validateNodeID(filter.NodeID) != nil) {
		return nil, commonapi.ErrBadRequest
	}
	filter.Search = strings.TrimSpace(filter.Search)
	rows, total, err := s.repo.List(ctx, page, size, filter)
	if err != nil {
		return nil, err
	}
	return &models.EngineDeploymentPage{Data: rows, Total: total, Page: page, PageSize: size, TotalPages: (total + int64(size) - 1) / int64(size)}, nil
}
func (s *EngineDeploymentService) Create(ctx context.Context, input models.EngineDeploymentInput, audit iam.AuditMetadata) (*models.EngineDeployment, error) {
	value, err := validateEngineDeployment(input)
	if err != nil {
		return nil, err
	}
	value.DeploymentID = uuid.NewString()
	if err = s.repo.Save(ctx, &value, 0, deploymentAudit(ctx, audit, "engine_deployment.created")); err != nil {
		return nil, err
	}
	return &value, nil
}
func (s *EngineDeploymentService) Update(ctx context.Context, id string, input models.EngineDeploymentUpdateRequest, audit iam.AuditMetadata) (*models.EngineDeployment, error) {
	if err := validateNodeID(id); err != nil {
		return nil, err
	}
	if input.Version < 1 {
		return nil, commonapi.ErrBadRequest
	}
	before, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	value, err := validateEngineDeployment(models.EngineDeploymentInput{DisplayName: input.DisplayName, Enabled: input.Enabled, Inventory: before.Inventory, DeclaredNodeID: input.DeclaredNodeID})
	if err != nil {
		return nil, err
	}
	value.DeploymentID = id
	if err = s.repo.Save(ctx, &value, input.Version, deploymentAudit(ctx, audit, "engine_deployment.updated")); err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *EngineDeploymentService) Admit(ctx context.Context, input models.EngineDeploymentAdmissionRequest, discovery models.DeploymentDiscovery, audit iam.AuditMetadata) (*models.EngineDeploymentAdmissionResponse, error) {
	values, err := buildDeploymentAdmissions(input, discovery, time.Now().UTC())
	if err != nil { return nil, err }
	result := &models.EngineDeploymentAdmissionResponse{Data: make([]models.EngineDeployment, 0, len(values))}
	err = s.repo.Transaction(ctx, func(repo *repository.EngineDeploymentRepository) error {
		service := NewEngineDeploymentService(repo)
		for _, value := range values {
			at, _ := time.Parse(time.RFC3339, value.Inventory.ObservedAt)
			if time.Since(at) > 120*time.Second { return ErrDeploymentSourceChanged }
			created, err := service.Create(ctx, value, audit)
			if err != nil { return err }
			result.Data = append(result.Data, *created)
		}
		for _,value:=range values {at,_:=time.Parse(time.RFC3339,value.Inventory.ObservedAt);if time.Since(at)>120*time.Second {return ErrDeploymentSourceChanged}}
		return nil
	})
	if err != nil { return nil, err }
	return result, nil
}

func buildDeploymentAdmissions(input models.EngineDeploymentAdmissionRequest, discovery models.DeploymentDiscovery, now time.Time) ([]models.EngineDeploymentInput, error) {
	if len(input.Items)<1 || len(input.Items)>16 { return nil, commonapi.ErrBadRequest }
	seen := map[string]bool{}
	values := make([]models.EngineDeploymentInput,0,len(input.Items))
	for _, item := range input.Items {
		key := item.Scope+"/"+item.Service
		if (item.Scope!="infra" && item.Scope!="business") || !discoveredServiceName.MatchString(item.Service) || !deploymentContainerID.MatchString(item.Fingerprint) || seen[key] {
			return nil, commonapi.ErrBadRequest
		}
		seen[key]=true
		var value *models.EngineDeploymentInput
		for _, scope := range discovery.Scopes {
			if scope.Scope!=item.Scope { continue }
			raw,_:=json.Marshal(scope)
			current:=parseDeploymentDiscovery(raw,item.Scope,now)
			if current.State!="available" { return nil, ErrDeploymentSourceChanged }
			for _, service := range current.Services {
				if service.Service!=item.Service { continue }
				if !deploymentCandidate(item.Scope,current.Source,service).Eligible || deploymentFingerprint(item.Scope,current.Source,service)!=item.Fingerprint { return nil, ErrDeploymentSourceChanged }
				ports:=[]models.EngineDeploymentPort{}
				for _, port:=range service.Containers[0].PublishedPorts { if port.ContainerPort==5432 && port.Protocol=="tcp" { ports=append(ports,port) } }
				container:=service.Containers[0]
				candidate:=models.EngineDeploymentInput{DisplayName:item.DisplayName,Enabled:item.Enabled,DeclaredNodeID:item.DeclaredNodeID,
					Inventory:models.EngineDeploymentInventory{Schema:"addp.engine-deployment-inventory/v1",ObservedAt:current.ObservedAt,Source:*current.Source,
						Services:[]models.EngineDeploymentService{{Service:service.Service,EngineType:service.EngineType,Containers:[]models.EngineDeploymentContainer{{ContainerID:container.ContainerID,StartedAt:container.StartedAt,RestartCount:container.RestartCount,PublishedPorts:ports}}}}}}
				if _,err:=validateEngineDeployment(candidate);err!=nil { return nil,err }
				value=&candidate
			}
		}
		if value==nil { return nil,ErrDeploymentSourceChanged }
		values=append(values,*value)
	}
	sort.Slice(values,func(i,j int)bool{return deploymentSourceKey(values[i].Inventory.Source,values[i].Inventory.Services[0].Service)<deploymentSourceKey(values[j].Inventory.Source,values[j].Inventory.Services[0].Service)})
	return values,nil
}
func deploymentAudit(ctx context.Context, metadata iam.AuditMetadata, event string) func(*gorm.DB, *models.EngineDeployment, models.EngineDeployment) error {
	return func(tx *gorm.DB, before *models.EngineDeployment, after models.EngineDeployment) error {
		return iam.NewAuditWriter(iam.NewRepository(tx)).Write(ctx, iam.AuditEvent{Metadata: metadata, EventName: event, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskMedium, ModuleName: "system", EntityType: "engine_deployment", EntityID: after.DeploymentID, Details: map[string]interface{}{"before": before, "after": after}})
	}
}
