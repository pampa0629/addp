package service

import (
	"context"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type HostNodeService struct {
	repo *repository.HostNodeRepository
}

func NewHostNodeService(repo *repository.HostNodeRepository) *HostNodeService {
	return &HostNodeService{repo: repo}
}

var nodeDNSLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var nodeModuleName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)

func validateHostNode(input models.HostNodeInput) (models.HostNode, error) {
	invalid := func() (models.HostNode, error) { return models.HostNode{}, commonapi.ErrBadRequest }
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || strings.IndexFunc(input.DisplayName, unicode.IsControl) >= 0 || utf8.RuneCountInString(input.DisplayName) > 255 || (input.NodeKind != "physical" && input.NodeKind != "virtual") || input.Enabled == nil || input.Addresses == nil || len(input.Addresses) > 32 || input.AllowedModuleBindings == nil || len(input.AllowedModuleBindings) > 64 {
		return invalid()
	}
	addresses := make([]string, 0, len(input.Addresses))
	seen := map[string]bool{}
	for _, raw := range input.Addresses {
		address := strings.ToLower(strings.TrimSpace(raw))
		if ip, err := netip.ParseAddr(address); err == nil {
			if ip.Zone() != "" {
				return invalid()
			}
			address = ip.Unmap().String()
		} else {
			if len(address) > 253 || address == "" {
				return invalid()
			}
			for _, label := range strings.Split(address, ".") {
				if !nodeDNSLabel.MatchString(label) {
					return invalid()
				}
			}
		}
		if seen[address] {
			return invalid()
		}
		seen[address] = true
		addresses = append(addresses, address)
	}
	pairs := map[string]bool{}
	for _, binding := range input.AllowedModuleBindings {
		if !nodeModuleName.MatchString(binding.ModuleName) || binding.ClientID != "addp-"+binding.ModuleName || pairs[binding.ClientID] {
			return invalid()
		}
		pairs[binding.ClientID] = true
	}
	return models.HostNode{DisplayName: input.DisplayName, NodeKind: input.NodeKind, Addresses: addresses, Enabled: *input.Enabled, AllowedModuleBindings: input.AllowedModuleBindings}, nil
}
func validateNodeID(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return commonapi.ErrBadRequest
	}
	return nil
}
func (s *HostNodeService) Get(ctx context.Context, id string) (*models.HostNode, error) {
	if err := validateNodeID(id); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}
func (s *HostNodeService) List(ctx context.Context, page, size int, search string) (*models.HostNodePage, error) {
	if page < 1 || size < 1 || size > 100 || page > 1000000 || utf8.RuneCountInString(search) > 255 || strings.IndexFunc(search, unicode.IsControl) >= 0 {
		return nil, commonapi.ErrBadRequest
	}
	nodes, total, err := s.repo.List(ctx, page, size, strings.TrimSpace(search))
	if err != nil {
		return nil, err
	}
	return &models.HostNodePage{Data: nodes, Total: total, Page: page, PageSize: size, TotalPages: (total + int64(size) - 1) / int64(size)}, nil
}
func (s *HostNodeService) Create(ctx context.Context, input models.HostNodeInput, audit iam.AuditMetadata) (*models.HostNode, error) {
	node, err := validateHostNode(input)
	if err != nil {
		return nil, err
	}
	node.NodeID = uuid.NewString()
	err = s.repo.Save(ctx, &node, 0, nodeAudit(ctx, audit, "host_node.created"))
	if err != nil {
		return nil, err
	}
	return &node, nil
}
func (s *HostNodeService) Update(ctx context.Context, id string, input models.HostNodeUpdateRequest, audit iam.AuditMetadata) (*models.HostNode, error) {
	if err := validateNodeID(id); err != nil {
		return nil, err
	}
	if input.Version < 1 {
		return nil, commonapi.ErrBadRequest
	}
	node, err := validateHostNode(input.HostNodeInput)
	if err != nil {
		return nil, err
	}
	node.NodeID = id
	err = s.repo.Save(ctx, &node, input.Version, nodeAudit(ctx, audit, "host_node.updated"))
	if err != nil {
		return nil, err
	}
	return &node, nil
}
func nodeAudit(ctx context.Context, metadata iam.AuditMetadata, event string) func(*gorm.DB, *models.HostNode, models.HostNode) error {
	return func(tx *gorm.DB, before *models.HostNode, after models.HostNode) error {
		return iam.NewAuditWriter(iam.NewRepository(tx)).Write(ctx, iam.AuditEvent{Metadata: metadata, EventName: event, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskMedium, ModuleName: "system", EntityType: "host_node", EntityID: after.NodeID, Details: map[string]interface{}{"before": before, "after": after}})
	}
}
