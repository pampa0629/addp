package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
)

const deploymentDiscoveryLimit = 256 * 1024

var discoveredServiceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// DeploymentDiscoveryService only reads a deployment-owned snapshot directory.
// It never invokes Docker, creates a registration, or grants a host association.
type DeploymentDiscoveryService struct{ Directory string }

func discoveryState(scope, state, reason string) models.DeploymentDiscoveryScope {
	return models.DeploymentDiscoveryScope{Schema: "addp.deployment-discovery/v2", Scope: scope, State: state, Reason: reason, Services: []models.DiscoveredDeploymentService{}}
}

func (s *DeploymentDiscoveryService) Read(ctx context.Context) models.DeploymentDiscovery {
	result := models.DeploymentDiscovery{Scopes: make([]models.DeploymentDiscoveryScope, 0, 2), Candidates: []models.EngineDeploymentCandidate{}}
	for _, scope := range []string{"infra", "business"} {
		value := s.readScope(ctx, scope)
		value.PostgreSQLMetrics = nil // The browser projection has no professional source endpoint.
		result.Scopes = append(result.Scopes, value)
		if value.State == "available" {
			for _, service := range value.Services {
				result.Candidates = append(result.Candidates, deploymentCandidate(scope, value.Source, service))
			}
		}
	}
	return result
}

func deploymentSourceKey(source models.EngineDeploymentSource, service string) string {
	return source.DockerEngineID + "/" + source.Project + "/" + service
}

func deploymentFingerprint(scope string, source *models.EngineDeploymentSource, service models.DiscoveredDeploymentService) string {
	raw, _ := json.Marshal(struct {
		Scope   string
		Source  *models.EngineDeploymentSource
		Service models.DiscoveredDeploymentService
	}{scope, source, service})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func deploymentCandidate(scope string, source *models.EngineDeploymentSource, service models.DiscoveredDeploymentService) models.EngineDeploymentCandidate {
	value := models.EngineDeploymentCandidate{Scope: scope, Service: service.Service, Fingerprint: deploymentFingerprint(scope, source, service), Reason: "unsupported_engine"}
	if source == nil || service.Service != "postgres" || service.EngineType != "postgresql" {
		return value
	}
	if len(service.Containers) != 1 {
		value.Reason = "unsupported_topology"
		return value
	}
	if service.Containers[0].Status != "running" {
		value.Reason = "not_running"
		return value
	}
	count := 0
	for _, port := range service.Containers[0].PublishedPorts {
		if port.ContainerPort == 5432 && port.Protocol == "tcp" {
			count++
		}
	}
	if count < 1 || count > 8 {
		value.Reason = "sql_port_missing"
		return value
	}
	value.Eligible, value.Reason = true, ""
	return value
}

func (s *EngineDeploymentService) MarkRegisteredDiscovery(ctx context.Context, value *models.DeploymentDiscovery) error {
	keys := make([][3]string, 0, len(value.Candidates))
	byScope := map[string]*models.EngineDeploymentSource{}
	for _, scope := range value.Scopes {
		if scope.State == "available" {
			byScope[scope.Scope] = scope.Source
		}
	}
	for _, candidate := range value.Candidates {
		if source := byScope[candidate.Scope]; source != nil {
			keys = append(keys, [3]string{source.DockerEngineID, source.Project, candidate.Service})
		}
	}
	registered, err := s.repo.RegisteredSources(ctx, keys)
	if err != nil {
		return err
	}
	for index, candidate := range value.Candidates {
		if source := byScope[candidate.Scope]; source != nil {
			value.Candidates[index].DeploymentID = registered[[3]string{source.DockerEngineID, source.Project, candidate.Service}]
		}
	}
	return nil
}

func (s *DeploymentDiscoveryService) readScope(ctx context.Context, scope string) models.DeploymentDiscoveryScope {
	raw, state := s.readDocument(ctx, scope+".json")
	if state != "" {
		return discoveryState(scope, state, "source_"+state)
	}
	return parseDeploymentDiscovery(raw, scope, time.Now().UTC())
}

// readDocument is the sole bounded, no-follow private snapshot reader.
func (s *DeploymentDiscoveryService) readDocument(ctx context.Context, name string) ([]byte, string) {
	if s == nil || s.Directory == "" {
		return nil, "unconfigured"
	}
	bad := "unavailable"
	if ctx.Err() != nil || !filepath.IsAbs(s.Directory) {
		return nil, bad
	}
	directory, err := os.Lstat(s.Directory)
	if err != nil || !directory.IsDir() || directory.Mode().Perm()&0077 != 0 {
		return nil, bad
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return nil, bad
	}
	defer root.Close()
	openedDirectory, err := root.Stat(".")
	if err != nil || !os.SameFile(directory, openedDirectory) || openedDirectory.Mode().Perm()&0077 != 0 {
		return nil, bad
	}
	before, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return nil, bad
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil {
		return nil, bad
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Mode().Perm()&0077 != 0 || info.Size() > deploymentDiscoveryLimit {
		return nil, bad
	}
	raw, err := io.ReadAll(io.LimitReader(file, deploymentDiscoveryLimit+1))
	if err != nil || len(raw) > deploymentDiscoveryLimit || ctx.Err() != nil {
		return nil, bad
	}
	after, err := root.Lstat(name)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(after, info) || after.Mode().Perm()&0077 != 0 {
		return nil, bad
	}
	return raw, ""
}

func parseDeploymentDiscovery(raw []byte, scope string, now time.Time) models.DeploymentDiscoveryScope {
	bad := discoveryState(scope, "unavailable", "source_invalid")
	var document struct {
		models.DeploymentDiscoveryScope
		Metrics json.RawMessage `json:"postgresql_metrics,omitempty"`
	}
	if len(raw) > deploymentDiscoveryLimit || DecodeDeploymentJSON(raw, &document) != nil {
		return bad
	}
	value := document.DeploymentDiscoveryScope
	if value.Schema != "addp.deployment-discovery/v2" || value.Scope != scope || value.Services == nil || (scope != "infra" && scope != "business") {
		return bad
	}
	at, err := time.Parse(time.RFC3339, value.ObservedAt)
	if err != nil || at.Format("2006-01-02T15:04:05Z") != value.ObservedAt || at.After(now.Add(30*time.Second)) {
		return bad
	}
	if now.Sub(at) > 120*time.Second {
		stale := discoveryState(scope, "stale", "source_stale")
		stale.ObservedAt = value.ObservedAt
		return stale
	}
	if value.State == "unavailable" {
		if value.Source != nil || len(value.Services) != 0 || value.Reason != "source_unavailable" {
			return bad
		}
		value.Services = []models.DiscoveredDeploymentService{}
		return value
	}
	source := value.Source
	project := "business"
	if scope == "infra" {
		project = "addp-infra"
	}
	if value.State != "available" || value.Reason != "" || source == nil || source.Type != "docker_compose" || source.Project != project || !deploymentDockerID.MatchString(source.DockerEngineID) || (source.Runtime != "linux_docker" && source.Runtime != "desktop_linux_vm") || len(value.Services) > 64 {
		return bad
	}
	services, containers, count := map[string]bool{}, map[string]bool{}, 0
	for _, service := range value.Services {
		if !discoveredServiceName.MatchString(service.Service) || services[service.Service] || !discoveredServiceName.MatchString(service.EngineType) || len(service.Containers) < 1 || len(service.Containers) > 16 {
			return bad
		}
		services[service.Service] = true
		for _, container := range service.Containers {
			count++
			if count > 128 || containers[container.ContainerID] || !deploymentContainerID.MatchString(container.ContainerID) || container.RestartCount < 0 || len(container.PublishedPorts) > 64 {
				return bad
			}
			containers[container.ContainerID] = true
			switch container.Status {
			case "created", "running", "paused", "restarting", "exited", "dead", "removing":
			default:
				return bad
			}
			if container.StartedAt != "" {
				started, err := time.Parse(time.RFC3339Nano, container.StartedAt)
				if err != nil || started.Year() < 1000 || container.StartedAt[len(container.StartedAt)-1] != 'Z' {
					return bad
				}
			} else if container.Status == "running" {
				return bad
			}
			ports := map[models.EngineDeploymentPort]bool{}
			for _, port := range container.PublishedPorts {
				ip, err := netip.ParseAddr(port.Address)
				if err != nil || ip.Zone() != "" || ip.String() != port.Address || port.Port < 1 || port.Port > 65535 || port.ContainerPort < 1 || port.ContainerPort > 65535 || (port.Protocol != "tcp" && port.Protocol != "udp" && port.Protocol != "sctp") || ports[port] {
					return bad
				}
				ports[port] = true
			}
		}
	}
	if value.Services == nil {
		value.Services = []models.DiscoveredDeploymentService{}
	}
	value.PostgreSQLMetrics = &commonmodels.PostgreSQLMetricsSource{State: "not_deployed"}
	if len(document.Metrics) != 0 {
		var metrics commonmodels.PostgreSQLMetricsSource
		if DecodeDeploymentJSON(document.Metrics, &metrics) != nil || metrics.Validate() != nil ||
			(metrics.StartedAt != nil && metrics.StartedAt.After(at)) {
			metrics = commonmodels.PostgreSQLMetricsSource{State: "unavailable"}
		}
		if metrics.State == "available" {
			matched := false
			for _, service := range value.Services {
				if service.Service == "postgres" && service.EngineType == "postgresql" && len(service.Containers) == 1 {
					container := service.Containers[0]
					started, err := time.Parse(time.RFC3339Nano, container.StartedAt)
					matched = err == nil && container.Status == "running" && container.ContainerID == metrics.ContainerID && started.Equal(*metrics.StartedAt)
				}
			}
			if !matched {
				metrics = commonmodels.PostgreSQLMetricsSource{State: "unavailable"}
			}
		}
		value.PostgreSQLMetrics = &metrics
	}
	return value
}
