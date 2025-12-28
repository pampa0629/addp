package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// PluginConfig 描述前后端统一的预览插件配置。
type PluginConfig struct {
	Name          string    `json:"name,omitempty"`
	Type          string    `json:"type,omitempty"`    // builtin | command，默认 command
	Builtin       string    `json:"builtin,omitempty"` // builtin 类型必填
	Command       string    `json:"command,omitempty"` // command 类型必填
	Args          []string  `json:"args,omitempty"`
	ResourceTypes []string  `json:"resource_types,omitempty"`
	Modes         []string  `json:"modes,omitempty"`
	Priority      *int      `json:"priority,omitempty"`
	TimeoutSec    int       `json:"timeout,omitempty"`
	Enabled       *bool     `json:"enabled,omitempty"`
	Metadata      JSONAlias `json:"metadata,omitempty"` // 预留
}

// JSONAlias 用于兼容未来扩展的透明 JSON 字段。
type JSONAlias map[string]interface{}

func (c *PluginConfig) isEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func (c *PluginConfig) pluginType() string {
	if t := strings.TrimSpace(strings.ToLower(c.Type)); t != "" {
		return t
	}
	if c.Builtin != "" {
		return "builtin"
	}
	return "command"
}

func (c *PluginConfig) priorityOr(defaultValue int) int {
	if c.Priority == nil {
		return defaultValue
	}
	return *c.Priority
}

// ==================== command provider ====================

type commandPreviewProvider struct {
	cfg          PluginConfig
	metadataRepo *repository.MetadataRepository
	resourceSet  map[string]struct{}
	modeSet      map[string]struct{}
	timeout      time.Duration
}

func newCommandPreviewProvider(cfg PluginConfig, metadataRepo *repository.MetadataRepository) (PreviewProvider, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("plugin %s command is empty", displayName(cfg))
	}

	resourceSet := make(map[string]struct{})
	for _, rt := range cfg.ResourceTypes {
		resourceSet[sanitizeResourceType(rt)] = struct{}{}
	}

	modeSet := make(map[string]struct{})
	for _, mode := range cfg.Modes {
		modeSet[strings.ToLower(strings.TrimSpace(mode))] = struct{}{}
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	return &commandPreviewProvider{
		cfg:          cfg,
		metadataRepo: metadataRepo,
		resourceSet:  resourceSet,
		modeSet:      modeSet,
		timeout:      timeout,
	}, nil
}

func (p *commandPreviewProvider) Name() string {
	if strings.TrimSpace(p.cfg.Name) != "" {
		return p.cfg.Name
	}
	return fmt.Sprintf("command:%s", p.cfg.Command)
}

func (p *commandPreviewProvider) Priority() int {
	return p.cfg.priorityOr(10)
}

func (p *commandPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}

	if len(p.resourceSet) > 0 {
		if _, ok := p.resourceSet[sanitizeResourceType(req.Resource.EngineType)]; !ok {
			return false
		}
	}

	mode := strings.ToLower(req.Mode())
	if len(p.modeSet) > 0 && mode != "" {
		if _, ok := p.modeSet[mode]; !ok {
			return false
		}
	}

	return true
}

func (p *commandPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	decrypted, err := p.metadataRepo.DecryptConnectionInfo(req.Resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("plugin %s failed to decrypt connection info: %w", p.Name(), err)
	}

	payload := map[string]interface{}{
		"schema":    req.Schema,
		"table":     req.Table,
		"page":      req.Page,
		"page_size": req.PageSize,
		"mode":      req.Mode(),
		"resource": map[string]interface{}{
			"id":              req.Resource.ID,
			"name":            req.Resource.Name,
			"resource_type":   req.Resource.EngineType,
			"connection_info": decrypted,
			"description":     req.Resource.Description,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("plugin %s failed to marshal payload: %w", p.Name(), err)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, p.cfg.Command, p.cfg.Args...)
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("plugin %s execution failed: %v (%s)", p.Name(), err, strings.TrimSpace(stderr.String()))
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("plugin %s returned empty response", p.Name())
	}

	var preview models.TablePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
		return nil, fmt.Errorf("plugin %s returned invalid JSON: %w", p.Name(), err)
	}

	return &preview, nil
}

// ==================== common helpers ====================

func displayName(cfg PluginConfig) string {
	if strings.TrimSpace(cfg.Name) != "" {
		return cfg.Name
	}
	if cfg.Builtin != "" {
		return cfg.Builtin
	}
	if cfg.Command != "" {
		return cfg.Command
	}
	return "unnamed"
}

// wrapProviderForOverrides 允许配置覆盖插件名称与优先级。
func wrapProviderForOverrides(provider PreviewProvider, cfg PluginConfig) PreviewProvider {
	needsName := strings.TrimSpace(cfg.Name) != "" && cfg.Name != provider.Name()
	needsPriority := cfg.Priority != nil && provider.Priority() != *cfg.Priority
	if !needsName && !needsPriority {
		return provider
	}

	return &providerOverrides{
		PreviewProvider: provider,
		overrideName:    strings.TrimSpace(cfg.Name),
		overridePriority: func() *int {
			if cfg.Priority == nil {
				return nil
			}
			return cfg.Priority
		}(),
	}
}

type providerOverrides struct {
	PreviewProvider
	overrideName     string
	overridePriority *int
}

func (p *providerOverrides) Name() string {
	if p.overrideName != "" {
		return p.overrideName
	}
	return p.PreviewProvider.Name()
}

func (p *providerOverrides) Priority() int {
	if p.overridePriority != nil {
		return *p.overridePriority
	}
	return p.PreviewProvider.Priority()
}

// builtinProviderFactory 负责实例化内置插件。
type builtinProviderFactory func(*repository.MetadataRepository) (PreviewProvider, error)
