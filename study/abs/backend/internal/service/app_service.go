package service

import (
	"abs/internal/models"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AppService 负责管理 ABS 内注册的应用
type AppService struct {
	mu       sync.RWMutex
	apps     map[string]*models.App
	config   *Config
	dataFile string
}

// NewAppService 创建应用管理服务
func NewAppService(config *Config) (*AppService, error) {
	if config.AppsDataFile == "" {
		return nil, errors.New("AppsDataFile is not configured")
	}

	if err := os.MkdirAll(filepath.Dir(config.AppsDataFile), 0o755); err != nil {
		return nil, fmt.Errorf("failed to prepare apps data directory: %w", err)
	}

	svc := &AppService{
		apps:     make(map[string]*models.App),
		config:   config,
		dataFile: config.AppsDataFile,
	}

	if err := svc.load(); err != nil {
		return nil, err
	}

	return svc, nil
}

// ListApps 返回所有应用
func (s *AppService) ListApps() []*models.App {
	s.mu.RLock()
	defer s.mu.RUnlock()

	apps := make([]*models.App, 0, len(s.apps))
	for _, app := range s.apps {
		clone := *app
		apps = append(apps, &clone)
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].CreatedAt.After(apps[j].CreatedAt)
	})

	return apps
}

// GetApp 获取单个应用
func (s *AppService) GetApp(id string) (*models.App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	app, ok := s.apps[id]
	if !ok {
		return nil, fmt.Errorf("app %s not found", id)
	}

	clone := *app
	return &clone, nil
}

// CreateApp 创建新应用
func (s *AppService) CreateApp(req *models.CreateAppRequest) (*models.App, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("应用名称不能为空")
	}
	if strings.TrimSpace(req.EntryURL) == "" {
		return nil, errors.New("入口 URL 不能为空")
	}

	now := time.Now()
	app := &models.App{
		ID:            uuid.New().String(),
		Name:          req.Name,
		Slug:          slugify(req.Name),
		Description:   req.Description,
		Category:      req.Category,
		Icon:          req.Icon,
		Cover:         req.Cover,
		EntryURL:      req.EntryURL,
		TaskID:        req.TaskID,
		WorkspacePath: req.WorkspacePath,
		StartCommand:  normalizeCommand(req.StartCommand),
		Tags:          uniqueStrings(req.Tags),
		Status:        models.AppStatusRegistered,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.apps[app.ID] = app
	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	clone := *app
	return &clone, nil
}

// LaunchApp 启动一个应用的运行指令
func (s *AppService) LaunchApp(id string) (*models.App, error) {
	s.mu.Lock()
	app, ok := s.apps[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("app %s not found", id)
	}

	if len(app.StartCommand) == 0 {
		s.mu.Unlock()
		return nil, errors.New("该应用没有配置启动命令")
	}

	workdir := s.resolveWorkspaceDir(app)
	if workdir != "" {
		if _, err := os.Stat(workdir); err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("工作目录不可用: %w", err)
		}
	}

	cmd := exec.Command(app.StartCommand[0], app.StartCommand[1:]...)
	if workdir != "" {
		cmd.Dir = workdir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("启动失败: %w", err)
	}

	now := time.Now()
	app.Status = models.AppStatusRunning
	app.LastStartedAt = &now
	app.LastStartResult = fmt.Sprintf("进程 %d 启动中...", cmd.Process.Pid)
	app.RunningPID = cmd.Process.Pid
	app.UpdatedAt = now

	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}

	go s.waitForAppProcess(app.ID, cmd, &stdout, &stderr)
	clone := *app
	s.mu.Unlock()
	return &clone, nil
}

func (s *AppService) waitForAppProcess(appID string, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) {
	err := cmd.Wait()
	output := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))

	s.mu.Lock()
	defer s.mu.Unlock()

	app, ok := s.apps[appID]
	if !ok {
		return
	}

	app.RunningPID = 0
	app.UpdatedAt = time.Now()

	if err != nil {
		app.Status = models.AppStatusFailed
		if output == "" {
			app.LastStartResult = fmt.Sprintf("进程异常退出: %v", err)
		} else {
			app.LastStartResult = fmt.Sprintf("进程异常退出: %v\n%s", err, output)
		}
	} else {
		app.Status = models.AppStatusRegistered
		if output == "" {
			app.LastStartResult = "进程执行完成"
		} else {
			app.LastStartResult = fmt.Sprintf("进程执行完成:\n%s", output)
		}
	}

	_ = s.saveLocked()
}

func (s *AppService) resolveWorkspaceDir(app *models.App) string {
	if app.WorkspacePath == "" {
		if app.TaskID != "" {
			return filepath.Join(s.config.WorkspaceDir, app.TaskID)
		}
		return ""
	}

	if filepath.IsAbs(app.WorkspacePath) {
		return app.WorkspacePath
	}

	return filepath.Join(s.config.WorkspaceDir, app.WorkspacePath)
}

func (s *AppService) load() error {
	data, err := os.ReadFile(s.dataFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read apps data: %w", err)
	}

	var apps []*models.App
	if err := json.Unmarshal(data, &apps); err != nil {
		return fmt.Errorf("failed to parse apps data: %w", err)
	}

	for _, app := range apps {
		appCopy := *app
		s.apps[app.ID] = &appCopy
	}

	return nil
}

func (s *AppService) saveLocked() error {
	apps := make([]*models.App, 0, len(s.apps))
	for _, app := range s.apps {
		apps = append(apps, app)
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].CreatedAt.Before(apps[j].CreatedAt)
	})

	data, err := json.MarshalIndent(apps, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal apps data: %w", err)
	}

	tempFile := s.dataFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp apps data: %w", err)
	}

	return os.Rename(tempFile, s.dataFile)
}

func slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(lower, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return uuid.New().String()
	}
	return slug
}

func normalizeCommand(cmd []string) []string {
	result := make([]string, 0, len(cmd))
	for _, part := range cmd {
		p := strings.TrimSpace(part)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// DeleteApp 删除应用及其工作目录
func (s *AppService) DeleteApp(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	app, ok := s.apps[id]
	if !ok {
		return fmt.Errorf("app %s not found", id)
	}

	// 如果应用正在运行，先终止进程
	if app.RunningPID > 0 {
		if proc, err := os.FindProcess(app.RunningPID); err == nil {
			_ = proc.Kill()
		}
	}

	// 删除工作目录
	workdir := s.resolveWorkspaceDir(app)
	if workdir != "" {
		if err := os.RemoveAll(workdir); err != nil {
			return fmt.Errorf("failed to delete workspace directory: %w", err)
		}
	}

	// 从内存中删除
	delete(s.apps, id)

	// 保存到磁盘
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("failed to save apps data after deletion: %w", err)
	}

	return nil
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))

	for _, item := range items {
		val := strings.TrimSpace(item)
		if val == "" {
			continue
		}
		lower := strings.ToLower(val)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, val)
	}
	return result
}

// RecordModification 记录应用的修改历史
func (s *AppService) RecordModification(appID, taskID, prompt string, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	app, ok := s.apps[appID]
	if !ok {
		return fmt.Errorf("app %s not found", appID)
	}

	// 初始化修改历史数组（如果为空）
	if app.ModificationHistory == nil {
		app.ModificationHistory = []models.ModificationRecord{}
	}

	// 添加新的修改记录
	record := models.ModificationRecord{
		Timestamp: time.Now(),
		Prompt:    prompt,
		TaskID:    taskID,
		Success:   success,
	}

	app.ModificationHistory = append(app.ModificationHistory, record)
	app.UpdatedAt = time.Now()

	// 保存到磁盘
	return s.saveLocked()
}
