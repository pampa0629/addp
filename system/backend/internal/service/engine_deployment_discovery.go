package service

import (
	"context"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/addp/system/internal/models"
)

const deploymentDiscoveryLimit = 256 * 1024

var discoveredServiceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// DeploymentDiscoveryService only reads a deployment-owned snapshot directory.
// It never invokes Docker, creates a registration, or grants a host association.
type DeploymentDiscoveryService struct{ Directory string }

func discoveryState(scope, state, reason string) models.DeploymentDiscoveryScope {
	return models.DeploymentDiscoveryScope{Schema: "addp.deployment-discovery/v1", Scope: scope, State: state, Reason: reason, Services: []models.DiscoveredDeploymentService{}}
}

func (s *DeploymentDiscoveryService) Read(ctx context.Context) models.DeploymentDiscovery {
	result := models.DeploymentDiscovery{Scopes: make([]models.DeploymentDiscoveryScope, 0, 2)}
	for _, scope := range []string{"infra", "business"} {
		result.Scopes = append(result.Scopes, s.readScope(ctx, scope))
	}
	return result
}

func (s *DeploymentDiscoveryService) readScope(ctx context.Context, scope string) models.DeploymentDiscoveryScope {
	if s == nil || s.Directory == "" {
		return discoveryState(scope, "unconfigured", "source_unconfigured")
	}
	bad := discoveryState(scope, "unavailable", "source_unavailable")
	if ctx.Err() != nil || !filepath.IsAbs(s.Directory) {
		return bad
	}
	directory, err := os.Lstat(s.Directory)
	if err != nil || !directory.IsDir() || directory.Mode().Perm()&0077 != 0 {
		return bad
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return bad
	}
	defer root.Close()
	openedDirectory, err := root.Stat(".")
	if err != nil || !os.SameFile(directory, openedDirectory) || openedDirectory.Mode().Perm()&0077 != 0 {
		return bad
	}
	before, err := root.Lstat(scope + ".json")
	if os.IsNotExist(err) {
		return discoveryState(scope, "missing", "source_missing")
	}
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return bad
	}
	file, err := root.OpenFile(scope+".json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return discoveryState(scope, "missing", "source_missing")
	}
	if err != nil {
		return bad
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Mode().Perm()&0077 != 0 || info.Size() > deploymentDiscoveryLimit {
		return bad
	}
	raw, err := io.ReadAll(io.LimitReader(file, deploymentDiscoveryLimit+1))
	if err != nil || len(raw) > deploymentDiscoveryLimit || ctx.Err() != nil {
		return bad
	}
	after, err := root.Lstat(scope + ".json")
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(after, info) || after.Mode().Perm()&0077 != 0 {
		return bad
	}
	return parseDeploymentDiscovery(raw, scope, time.Now().UTC())
}

func parseDeploymentDiscovery(raw []byte, scope string, now time.Time) models.DeploymentDiscoveryScope {
	bad := discoveryState(scope, "unavailable", "source_invalid")
	var value models.DeploymentDiscoveryScope
	if len(raw) > deploymentDiscoveryLimit || DecodeDeploymentJSON(raw, &value) != nil || value.Schema != "addp.deployment-discovery/v1" || value.Scope != scope || value.Services == nil || (scope != "infra" && scope != "business") {
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
			ports := map[models.BusinessDeploymentPort]bool{}
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
	return value
}
