package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/develop/backend/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// JupyterInstanceService Jupyter 实例管理服务
// 负责租户级别的 Jupyter 容器生命周期管理
type JupyterInstanceService struct {
	cfg          *config.Config
	dockerClient *client.Client
}

// NewJupyterInstanceService 创建 Jupyter 实例管理服务
func NewJupyterInstanceService(cfg *config.Config) (*JupyterInstanceService, error) {
	// 创建 Docker 客户端
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &JupyterInstanceService{
		cfg:          cfg,
		dockerClient: dockerClient,
	}, nil
}

// JupyterInstance Jupyter 实例信息
type JupyterInstance struct {
	TenantID      uint      `json:"tenant_id"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Status        string    `json:"status"`      // running, stopped, starting
	JupyterURL    string    `json:"jupyter_url"` // Jupyter Lab 访问 URL
	JupyterPort   int       `json:"jupyter_port"`
	CreatedAt     time.Time `json:"created_at"`
	StartedAt     time.Time `json:"started_at,omitempty"`
}

// StartInstance 启动租户的 Jupyter 实例
func (s *JupyterInstanceService) StartInstance(ctx context.Context, tenantID uint) (*JupyterInstance, error) {
	logger.L().Info("启动 Jupyter 实例", "tenant_id", tenantID)

	// 1. 检查是否已经有运行中的实例
	existingInstance, err := s.GetInstance(ctx, tenantID)
	if err == nil && existingInstance != nil {
		if existingInstance.Status == "running" {
			logger.L().Info("Jupyter 实例已在运行", "tenant_id", tenantID, "container_id", existingInstance.ContainerID)
			return existingInstance, nil
		}

		// 如果实例存在但已停止，先删除
		if err := s.removeContainer(ctx, existingInstance.ContainerID); err != nil {
			logger.L().Warn("删除已停止的容器失败", "error", err)
		}
	}

	// 2. 分配端口（8100 + tenant_id）
	jupyterPort := 8100 + int(tenantID)

	// 3. 创建容器配置
	containerName := fmt.Sprintf("jupyter-tenant-%d", tenantID)

	// 环境变量
	env := []string{
		fmt.Sprintf("ADDP_TENANT_ID=%d", tenantID),
		fmt.Sprintf("ADDP_API_BASE=%s", s.cfg.SystemServiceURL),
		"ADDP_AUTO_LOAD=true",
		"JUPYTER_PORT=8088",
		"API_PORT=8097",
	}

	// 端口映射
	portBindings := nat.PortMap{
		"8088/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", jupyterPort)}},
	}

	// 容器配置
	containerConfig := &container.Config{
		Image: "addp/jupyter-engine:latest", // 使用本地构建的镜像
		Env:   env,
		Labels: map[string]string{
			"app":        "addp",
			"service":    "jupyter",
			"tenant_id":  fmt.Sprintf("%d", tenantID),
			"managed_by": "addp-develop",
		},
	}

	// 主机配置
	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		Resources: container.Resources{
			// 资源限制
			Memory:   4 * 1024 * 1024 * 1024, // 4GB
			NanoCPUs: 2 * 1e9,                // 2 核
		},
	}

	// 网络配置
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"addp": {}, // 连接到 addp 网络
		},
	}

	// 4. 创建容器
	logger.L().Info("创建 Jupyter 容器", "tenant_id", tenantID, "name", containerName, "port", jupyterPort)

	resp, err := s.dockerClient.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		containerName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	containerID := resp.ID

	// 5. 启动容器
	if err := s.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		// 启动失败，尝试删除容器
		_ = s.removeContainer(ctx, containerID)
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	logger.L().Info("Jupyter 容器已启动", "tenant_id", tenantID, "container_id", containerID)

	// 6. 等待 Jupyter Lab 就绪（最多等待 30 秒）
	logger.L().Info("等待 Jupyter Lab 就绪...")
	time.Sleep(10 * time.Second) // 初始等待

	// 7. 返回实例信息
	instance := &JupyterInstance{
		TenantID:      tenantID,
		ContainerID:   containerID,
		ContainerName: containerName,
		Status:        "running",
		JupyterURL:    fmt.Sprintf("http://localhost:%d/lab", jupyterPort),
		JupyterPort:   jupyterPort,
		CreatedAt:     time.Now(),
		StartedAt:     time.Now(),
	}

	return instance, nil
}

// StopInstance 停止租户的 Jupyter 实例
func (s *JupyterInstanceService) StopInstance(ctx context.Context, tenantID uint) error {
	logger.L().Info("停止 Jupyter 实例", "tenant_id", tenantID)

	// 1. 查找实例
	instance, err := s.GetInstance(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to find instance: %w", err)
	}

	if instance == nil {
		logger.L().Warn("Jupyter 实例不存在", "tenant_id", tenantID)
		return nil
	}

	// 2. 停止容器
	timeout := 10
	if err := s.dockerClient.ContainerStop(ctx, instance.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		if !strings.Contains(err.Error(), "is not running") {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}

	// 3. 删除容器
	if err := s.removeContainer(ctx, instance.ContainerID); err != nil {
		logger.L().Warn("删除容器失败", "error", err)
	}

	logger.L().Info("Jupyter 实例已停止", "tenant_id", tenantID, "container_id", instance.ContainerID)

	return nil
}

// GetInstance 获取租户的 Jupyter 实例信息
func (s *JupyterInstanceService) GetInstance(ctx context.Context, tenantID uint) (*JupyterInstance, error) {
	// 查找带有 tenant_id 标签的容器
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("tenant_id=%d", tenantID))
	filterArgs.Add("label", "service=jupyter")

	containers, err := s.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true, // 包括已停止的容器
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return nil, nil // 没有找到实例
	}

	// 取第一个匹配的容器
	c := containers[0]

	// 解析端口
	jupyterPort := 0
	if len(c.Ports) > 0 {
		for _, port := range c.Ports {
			if port.PrivatePort == 8088 {
				jupyterPort = int(port.PublicPort)
				break
			}
		}
	}

	// 构造实例信息
	instance := &JupyterInstance{
		TenantID:      tenantID,
		ContainerID:   c.ID,
		ContainerName: strings.TrimPrefix(c.Names[0], "/"),
		Status:        c.State,
		JupyterPort:   jupyterPort,
		CreatedAt:     time.Unix(c.Created, 0),
	}

	if jupyterPort > 0 {
		instance.JupyterURL = fmt.Sprintf("http://localhost:%d/lab", jupyterPort)
	}

	return instance, nil
}

// ListInstances 列出所有 Jupyter 实例
func (s *JupyterInstanceService) ListInstances(ctx context.Context) ([]*JupyterInstance, error) {
	// 查找所有 Jupyter 容器
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "service=jupyter")
	filterArgs.Add("label", "managed_by=addp-develop")

	containers, err := s.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	instances := make([]*JupyterInstance, 0, len(containers))

	for _, c := range containers {
		// 从标签中提取 tenant_id
		tenantIDStr := c.Labels["tenant_id"]
		if tenantIDStr == "" {
			continue
		}

		var tenantID uint
		if _, err := fmt.Sscanf(tenantIDStr, "%d", &tenantID); err != nil {
			logger.L().Warn("无效的 tenant_id", "label", tenantIDStr)
			continue
		}

		// 解析端口
		jupyterPort := 0
		if len(c.Ports) > 0 {
			for _, port := range c.Ports {
				if port.PrivatePort == 8088 {
					jupyterPort = int(port.PublicPort)
					break
				}
			}
		}

		instance := &JupyterInstance{
			TenantID:      tenantID,
			ContainerID:   c.ID,
			ContainerName: strings.TrimPrefix(c.Names[0], "/"),
			Status:        c.State,
			JupyterPort:   jupyterPort,
			CreatedAt:     time.Unix(c.Created, 0),
		}

		if jupyterPort > 0 {
			instance.JupyterURL = fmt.Sprintf("http://localhost:%d/lab", jupyterPort)
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

// removeContainer 删除容器
func (s *JupyterInstanceService) removeContainer(ctx context.Context, containerID string) error {
	return s.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

// Close 关闭服务
func (s *JupyterInstanceService) Close() error {
	if s.dockerClient != nil {
		return s.dockerClient.Close()
	}
	return nil
}
