package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/develop/backend/internal/config"
)

// JupyterVenvService 租户虚拟环境管理服务
// 管理租户级别的 Python 虚拟环境和 Jupyter Kernel
type JupyterVenvService struct {
	cfg              *config.Config
	projectRoot      string // 项目根目录
	tenantsDataPath  string // 租户数据目录
	initScriptPath   string // 初始化脚本路径
	jupyterServerURL string // Jupyter Server URL
}

// NewJupyterVenvService 创建租户虚拟环境管理服务
func NewJupyterVenvService(cfg *config.Config) (*JupyterVenvService, error) {
	// 获取项目根目录
	projectRoot := os.Getenv("PROJECT_ROOT")
	if projectRoot == "" {
		// 从当前工作目录推断
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current working directory: %w", err)
		}
		// 假设当前在 develop/backend/ 目录
		projectRoot = filepath.Join(cwd, "../..")
	}

	tenantsDataPath := filepath.Join(projectRoot, "engines/jupyter/tenants")
	initScriptPath := filepath.Join(projectRoot, "engines/jupyter/init_tenant_venv.sh")

	// Jupyter Server URL (开发环境固定为 localhost:8088)
	jupyterServerURL := "http://localhost:8088"

	return &JupyterVenvService{
		cfg:              cfg,
		projectRoot:      projectRoot,
		tenantsDataPath:  tenantsDataPath,
		initScriptPath:   initScriptPath,
		jupyterServerURL: jupyterServerURL,
	}, nil
}

// TenantVenvInfo 租户虚拟环境信息
type TenantVenvInfo struct {
	TenantID          uint      `json:"tenant_id"`
	VenvPath          string    `json:"venv_path"`
	KernelName        string    `json:"kernel_name"`
	KernelDisplayName string    `json:"kernel_display_name"`
	Exists            bool      `json:"exists"`
	JupyterURL        string    `json:"jupyter_url"` // Jupyter Lab 访问 URL
	CreatedAt         time.Time `json:"created_at,omitempty"`
}

// InitTenantVenv 初始化租户虚拟环境
func (s *JupyterVenvService) InitTenantVenv(ctx context.Context, tenantID uint) (*TenantVenvInfo, error) {
	logger.L().Info("初始化租户虚拟环境", "tenant_id", tenantID)

	// 1. 检查是否已存在
	info, err := s.GetTenantVenvInfo(ctx, tenantID)
	if err == nil && info.Exists {
		logger.L().Info("租户虚拟环境已存在", "tenant_id", tenantID, "venv_path", info.VenvPath)
		return info, nil
	}

	// 2. 验证初始化脚本存在
	if _, err := os.Stat(s.initScriptPath); err != nil {
		return nil, fmt.Errorf("初始化脚本不存在: %s, 错误: %w", s.initScriptPath, err)
	}

	// 3. 执行初始化脚本
	cmd := exec.CommandContext(ctx, s.initScriptPath, fmt.Sprintf("%d", tenantID))
	cmd.Dir = filepath.Dir(s.initScriptPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PROJECT_ROOT=%s", s.projectRoot),
		fmt.Sprintf("ADDP_API_BASE=%s", s.cfg.SystemServiceURL),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.L().Error("初始化租户虚拟环境失败",
			"tenant_id", tenantID,
			"error", err,
			"output", string(output))
		return nil, fmt.Errorf("初始化失败: %w\n输出: %s", err, string(output))
	}

	logger.L().Info("租户虚拟环境初始化成功",
		"tenant_id", tenantID,
		"output", string(output))

	// 4. 返回信息
	return s.GetTenantVenvInfo(ctx, tenantID)
}

// GetTenantVenvInfo 获取租户虚拟环境信息
func (s *JupyterVenvService) GetTenantVenvInfo(ctx context.Context, tenantID uint) (*TenantVenvInfo, error) {
	venvPath := filepath.Join(s.tenantsDataPath, fmt.Sprintf("tenant_%d", tenantID), "venv")
	kernelName := fmt.Sprintf("tenant_%d", tenantID)
	kernelDisplayName := fmt.Sprintf("Python 3 (租户 %d)", tenantID)

	info := &TenantVenvInfo{
		TenantID:          tenantID,
		VenvPath:          venvPath,
		KernelName:        kernelName,
		KernelDisplayName: kernelDisplayName,
		JupyterURL:        fmt.Sprintf("%s/lab?kernel=%s", s.jupyterServerURL, kernelName),
	}

	// 检查虚拟环境是否存在
	if stat, err := os.Stat(venvPath); err == nil && stat.IsDir() {
		info.Exists = true
		info.CreatedAt = stat.ModTime()

		logger.L().Debug("租户虚拟环境存在",
			"tenant_id", tenantID,
			"venv_path", venvPath,
			"created_at", info.CreatedAt)
	} else {
		logger.L().Debug("租户虚拟环境不存在",
			"tenant_id", tenantID,
			"venv_path", venvPath)
	}

	return info, nil
}

// DeleteTenantVenv 删除租户虚拟环境 (慎用)
func (s *JupyterVenvService) DeleteTenantVenv(ctx context.Context, tenantID uint) error {
	tenantDir := filepath.Join(s.tenantsDataPath, fmt.Sprintf("tenant_%d", tenantID))

	logger.L().Warn("删除租户虚拟环境", "tenant_id", tenantID, "path", tenantDir)

	// 检查目录是否存在
	if _, err := os.Stat(tenantDir); os.IsNotExist(err) {
		return fmt.Errorf("租户虚拟环境不存在: tenant_id=%d", tenantID)
	}

	// 删除整个租户目录
	if err := os.RemoveAll(tenantDir); err != nil {
		return fmt.Errorf("删除失败: %w", err)
	}

	// 删除 Jupyter Kernel 软链接
	homeDir, err := os.UserHomeDir()
	if err == nil {
		kernelName := fmt.Sprintf("tenant_%d", tenantID)
		kernelLink := filepath.Join(homeDir, ".local/share/jupyter/kernels", kernelName)
		if _, err := os.Lstat(kernelLink); err == nil {
			os.Remove(kernelLink) // 忽略错误
		}
	}

	logger.L().Info("租户虚拟环境已删除", "tenant_id", tenantID)
	return nil
}

// ListTenantVenvs 列出所有租户的虚拟环境
func (s *JupyterVenvService) ListTenantVenvs(ctx context.Context) ([]*TenantVenvInfo, error) {
	var infos []*TenantVenvInfo

	// 检查租户数据目录是否存在
	if _, err := os.Stat(s.tenantsDataPath); os.IsNotExist(err) {
		return infos, nil // 返回空列表
	}

	// 读取所有租户目录
	entries, err := os.ReadDir(s.tenantsDataPath)
	if err != nil {
		return nil, fmt.Errorf("读取租户数据目录失败: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// 解析租户 ID (tenant_1 -> 1)
		name := entry.Name()
		if !strings.HasPrefix(name, "tenant_") {
			continue
		}

		var tenantID uint
		_, err := fmt.Sscanf(name, "tenant_%d", &tenantID)
		if err != nil {
			logger.L().Warn("无法解析租户目录名", "name", name, "error", err)
			continue
		}

		// 获取虚拟环境信息
		info, err := s.GetTenantVenvInfo(ctx, tenantID)
		if err != nil {
			logger.L().Warn("获取租户虚拟环境信息失败", "tenant_id", tenantID, "error", err)
			continue
		}

		if info.Exists {
			infos = append(infos, info)
		}
	}

	return infos, nil
}

// GetJupyterServerStatus 获取 Jupyter Server 状态
func (s *JupyterVenvService) GetJupyterServerStatus(ctx context.Context) map[string]interface{} {
	// 简单的健康检查
	// TODO: 可以调用 Jupyter API 的 /api/status 端点
	return map[string]interface{}{
		"url":    s.jupyterServerURL,
		"status": "running", // 简化处理,假设总是运行中
	}
}
