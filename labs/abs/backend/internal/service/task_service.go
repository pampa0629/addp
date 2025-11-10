package service

import (
	"abs/internal/models"

	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskService manages AI bootstrapping tasks
type TaskService struct {
	tasks         map[string]*models.Task
	mu            sync.RWMutex
	codeGenerator CodeGenerator
	config        *Config
	wsManager     *WebSocketManager
	appService    *AppService
}

// NewTaskService creates a new task service
func NewTaskService(config *Config, wsManager *WebSocketManager, appService *AppService) *TaskService {
	return &TaskService{
		tasks:         make(map[string]*models.Task),
		codeGenerator: resolveCodeGenerator(config),
		config:        config,
		wsManager:     wsManager,
		appService:    appService,
	}
}

func resolveCodeGenerator(config *Config) CodeGenerator {
	provider := strings.ToLower(config.CodeGenProvider)

	switch provider {
	case "codex_cli", "codex-cli":
		// Codex CLI 模式（推荐）
		return NewCodexCLIClient(config)
	case "codex":
		// Codex API 模式
		if config.CodexAPIKey != "" {
			return NewCodexClient(config.CodexAPIKey, config.CodexBaseURL, config.CodexModel)
		}
		// 如果没有 API Key，回退到 CLI 模式
		fmt.Println("Warning: CODEX_API_KEY not set, falling back to codex_cli mode")
		return NewCodexCLIClient(config)
	case "claude":
		// Claude 模式（暂停使用，代码保留）
		return NewClaudeClient(config.ClaudeAPIKey, config.ClaudeAuthToken, config.ClaudeBaseURL, config.ClaudeModel)
	default:
		// 默认使用 Codex CLI 模式
		return NewCodexCLIClient(config)
	}
}

// CreateTask creates a new AI task
func (s *TaskService) CreateTask(prompt string) (*models.Task, error) {
	task := &models.Task{
		ID:        uuid.New().String(),
		Prompt:    prompt,
		Status:    models.StatusPending,
		Logs:      []string{},
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	// Log task creation
	fmt.Printf("\n========================================\n")
	fmt.Printf("[Task %s] 🆕 CREATED\n", task.ID[:8])
	fmt.Printf("Prompt: %s\n", prompt)
	fmt.Printf("========================================\n\n")

	// Process task asynchronously
	go s.processTask(task)

	return task, nil
}

// CreateModificationTask 创建一个基于已有应用的增量修改任务
func (s *TaskService) CreateModificationTask(appID, modificationPrompt string) (*models.Task, error) {
	// 获取应用信息
	app, err := s.appService.GetApp(appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	if app.WorkspacePath == "" {
		return nil, errors.New("app has no workspace path")
	}

	// 读取现有代码
	workspaceDir := filepath.Join(s.config.WorkspaceDir, app.WorkspacePath)
	existingCode, err := s.readWorkspaceCode(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read existing code: %w", err)
	}

	// 构建增量修改提示词
	enhancedPrompt := fmt.Sprintf(`你正在修改一个已有的应用程序。以下是当前代码：

%s

用户的修改需求：
%s

请基于上述代码进行修改，保留原有功能，只针对用户需求进行调整。如果是多文件项目，请保持文件结构并使用 filepath 标记。`, existingCode, modificationPrompt)

	// 创建任务
	task := &models.Task{
		ID:        uuid.New().String(),
		Prompt:    enhancedPrompt,
		Status:    models.StatusPending,
		Logs:      []string{fmt.Sprintf("基于应用 %s (%s) 进行增量修改", app.Name, appID)},
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	// 异步处理任务，使用原有workspace目录（覆盖更新）
	go s.processModificationTask(task, app)

	return task, nil
}

// readWorkspaceCode 读取workspace目录下的所有代码文件
func (s *TaskService) readWorkspaceCode(workspaceDir string) (string, error) {
	var codeBuilder strings.Builder

	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录和非代码文件
		if info.IsDir() {
			return nil
		}

		// 跳过二进制文件和临时文件
		if strings.HasPrefix(info.Name(), ".") || info.Name() == "app" ||
		   strings.HasSuffix(info.Name(), ".exe") || strings.HasSuffix(info.Name(), ".so") {
			return nil
		}

		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// 计算相对路径
		relPath, _ := filepath.Rel(workspaceDir, path)

		// 添加文件标记和内容
		codeBuilder.WriteString(fmt.Sprintf("// filepath: %s\n", relPath))
		codeBuilder.Write(content)
		codeBuilder.WriteString("\n\n")

		return nil
	})

	if err != nil {
		return "", err
	}

	return codeBuilder.String(), nil
}

// processModificationTask 处理修改任务（覆盖原workspace）
func (s *TaskService) processModificationTask(task *models.Task, originalApp *models.App) {
	providerName := "AI"
	if s.codeGenerator != nil {
		providerName = strings.ToUpper(s.codeGenerator.Provider())
	}
	s.updateStatus(task, models.StatusProcessing, fmt.Sprintf("使用 %s 生成修改后的代码...", providerName))

	// Step 1: Generate modified code
	if s.codeGenerator == nil {
		s.failTask(task, "No code generator configured")
		return
	}

	code, err := s.codeGenerator.GenerateCode(task.Prompt)
	if err != nil {
		s.failTask(task, fmt.Sprintf("Code generation failed: %v", err))
		return
	}

	task.Code = code
	s.addLog(task, "Modified code generated successfully")

	// Step 2: 备份原workspace（可选）
	workspaceDir := filepath.Join(s.config.WorkspaceDir, originalApp.WorkspacePath)
	backupDir := workspaceDir + ".backup." + time.Now().Format("20060102-150405")
	if err := s.copyDir(workspaceDir, backupDir); err != nil {
		s.addLog(task, fmt.Sprintf("Warning: Failed to backup workspace: %v", err))
	} else {
		s.addLog(task, fmt.Sprintf("Workspace backed up to: %s", backupDir))
	}

	// Step 3: 清空并重写workspace
	s.updateStatus(task, models.StatusCompiling, "Updating workspace with modified code...")
	if err := os.RemoveAll(workspaceDir); err != nil {
		s.failTask(task, fmt.Sprintf("Failed to clean workspace: %v", err))
		return
	}
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		s.failTask(task, fmt.Sprintf("Failed to recreate workspace: %v", err))
		return
	}

	// 使用原有的writeCodeToWorkspace逻辑，但指定workspace目录
	if err := s.writeModifiedCodeToWorkspace(task, originalApp.WorkspacePath); err != nil {
		s.failTask(task, fmt.Sprintf("Failed to write modified code: %v", err))
		return
	}

	s.addLog(task, "Modified code written to workspace")

	// Step 4: Compile code (if applicable)
	if err := s.compileCode(task); err != nil {
		s.failTask(task, fmt.Sprintf("Compilation failed: %v", err))
		return
	}

	// Step 5: Deploy/Run
	s.updateStatus(task, models.StatusDeploying, "Deploying modified application...")
	if err := s.deployCode(task); err != nil {
		s.failTask(task, fmt.Sprintf("Deployment failed: %v", err))
		return
	}

	// Step 6: 更新原app的信息而非创建新app
	s.updateAppAfterModification(task, originalApp.ID)

	// Complete
	s.completeTask(task)
}

// copyDir 递归复制目录
func (s *TaskService) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// writeModifiedCodeToWorkspace 将修改后的代码写入指定workspace
func (s *TaskService) writeModifiedCodeToWorkspace(task *models.Task, workspacePath string) error {
	workspaceDir := filepath.Join(s.config.WorkspaceDir, workspacePath)

	// Parse multi-file code
	files := s.parseCodeFiles(task.Code)

	if len(files) == 0 {
		// Single file code
		mainFile := filepath.Join(workspaceDir, "main.go")
		return os.WriteFile(mainFile, []byte(task.Code), 0644)
	}

	// Write multiple files
	for path, content := range files {
		fullPath := filepath.Join(workspaceDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

// updateAppAfterModification 更新应用的修改历史和相关信息
func (s *TaskService) updateAppAfterModification(task *models.Task, appID string) {
	if s.appService == nil {
		return
	}

	s.addLog(task, fmt.Sprintf("Updating app %s with modification history...", appID))

	// 调用AppService的更新方法（需要添加）
	if err := s.appService.RecordModification(appID, task.ID, task.Prompt, true); err != nil {
		s.addLog(task, fmt.Sprintf("Warning: Failed to record modification: %v", err))
	} else {
		s.addLog(task, "App modification history updated successfully")
	}
}

// GetTask retrieves a task by ID
func (s *TaskService) GetTask(id string) (*models.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task not found")
	}

	return task, nil
}

// ListTasks returns all tasks
func (s *TaskService) ListTasks() []*models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*models.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	return tasks
}

// processTask processes a task through all stages
func (s *TaskService) processTask(task *models.Task) {
	providerName := "AI"
	if s.codeGenerator != nil {
		providerName = strings.ToUpper(s.codeGenerator.Provider())
	}
	s.updateStatus(task, models.StatusProcessing, fmt.Sprintf("使用 %s 生成代码...", providerName))

	// Step 1: Generate code
	if s.codeGenerator == nil {
		s.failTask(task, "No code generator configured")
		return
	}

	code, err := s.codeGenerator.GenerateCode(task.Prompt)
	if err != nil {
		s.failTask(task, fmt.Sprintf("Code generation failed: %v", err))
		return
	}

	task.Code = code
	s.addLog(task, "Code generated successfully")

	// Step 2: Write code to workspace
	s.updateStatus(task, models.StatusCompiling, "Writing code to workspace...")
	if err := s.writeCodeToWorkspace(task); err != nil {
		s.failTask(task, fmt.Sprintf("Failed to write code: %v", err))
		return
	}

	s.addLog(task, "Code written to workspace")

	// Step 3: Compile code (if applicable)
	if err := s.compileCode(task); err != nil {
		s.failTask(task, fmt.Sprintf("Compilation failed: %v", err))
		return
	}

	// Step 4: Deploy/Run
	s.updateStatus(task, models.StatusDeploying, "Deploying and starting service...")
	if err := s.deployCode(task); err != nil {
		s.failTask(task, fmt.Sprintf("Deployment failed: %v", err))
		return
	}

	// Step 5: Auto register app if manifest found
	s.autoRegisterApp(task)

	// Complete
	s.completeTask(task)
}

// writeCodeToWorkspace writes generated code to the workspace directory
func (s *TaskService) writeCodeToWorkspace(task *models.Task) error {
	workspaceDir := filepath.Join(s.config.WorkspaceDir, task.ID)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return err
	}

	// Parse multi-file code
	files := s.parseCodeFiles(task.Code)

	if len(files) == 0 {
		// Single file code
		mainFile := filepath.Join(workspaceDir, "main.go")
		return os.WriteFile(mainFile, []byte(task.Code), 0644)
	}

	// Write multiple files
	for path, content := range files {
		fullPath := filepath.Join(workspaceDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

// parseCodeFiles parses multi-file code based on filepath comments
func (s *TaskService) parseCodeFiles(code string) map[string]string {
	files := make(map[string]string)
	lines := strings.Split(code, "\n")

	var currentFile string
	var currentContent []string

	for _, line := range lines {
		if strings.HasPrefix(line, "// filepath:") || strings.HasPrefix(line, "# filepath:") {
			// Save previous file
			if currentFile != "" {
				files[currentFile] = strings.Join(currentContent, "\n")
			}
			// Start new file
			currentFile = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "// filepath:"), "# filepath:"))
			currentContent = []string{}
		} else if currentFile != "" {
			currentContent = append(currentContent, line)
		}
	}

	// Save last file
	if currentFile != "" {
		files[currentFile] = strings.Join(currentContent, "\n")
	}

	return files
}

// compileCode compiles the generated code
func (s *TaskService) compileCode(task *models.Task) error {
	workspaceDir := filepath.Join(s.config.WorkspaceDir, task.ID)

	// Check if Go code
	if strings.Contains(task.Code, "package main") || strings.Contains(task.Code, "package ") {
		fmt.Printf("[Task %s] 🔨 Compiling Go code...\n", task.ID[:8])
		s.addLog(task, "Detected Go code, running go mod init...")

		// Initialize go module
		cmd := exec.Command("go", "mod", "init", "generated")
		cmd.Dir = workspaceDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			errMsg := fmt.Sprintf("go mod init failed: %v\n%s", err, output)
			fmt.Printf("[Task %s] ❌ %s\n", task.ID[:8], errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		fmt.Printf("[Task %s] ✓ go mod init successful\n", task.ID[:8])

		s.addLog(task, "Running go mod tidy...")
		cmd = exec.Command("go", "mod", "tidy")
		cmd.Dir = workspaceDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			errMsg := fmt.Sprintf("go mod tidy failed: %v\n%s", err, output)
			fmt.Printf("[Task %s] ❌ %s\n", task.ID[:8], errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		fmt.Printf("[Task %s] ✓ go mod tidy successful\n", task.ID[:8])

		s.addLog(task, "Compiling Go code...")
		cmd = exec.Command("go", "build", "-o", "app")
		cmd.Dir = workspaceDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			errMsg := fmt.Sprintf("compilation failed: %v\n%s", err, output)
			fmt.Printf("[Task %s] ❌ %s\n", task.ID[:8], errMsg)
			return fmt.Errorf("%s", errMsg)
		}

		s.addLog(task, "Compilation successful")
		fmt.Printf("[Task %s] ✅ Compilation successful\n", task.ID[:8])
	}

	return nil
}

// deployCode deploys and runs the generated code
func (s *TaskService) deployCode(task *models.Task) error {
	workspaceDir := filepath.Join(s.config.WorkspaceDir, task.ID)

	// Check for compiled binary
	appPath := filepath.Join(workspaceDir, "app")
	if _, err := os.Stat(appPath); err == nil {
		s.addLog(task, "Starting compiled application...")

		cmd := exec.Command("./app")
		cmd.Dir = workspaceDir

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start app: %v", err)
		}

		s.addLog(task, fmt.Sprintf("Application started with PID %d", cmd.Process.Pid))
		return nil
	}

	runtimeExecuted, err := s.runInterpretedCode(task, workspaceDir)
	if err != nil {
		return err
	}

	if runtimeExecuted {
		return nil
	}

	// Otherwise, try to run directly
	s.addLog(task, "Running code directly...")
	return nil
}

func (s *TaskService) autoRegisterApp(task *models.Task) {
	if s.appService == nil {
		return
	}

	workspaceDir := filepath.Join(s.config.WorkspaceDir, task.ID)

	manifest, fromFile, err := s.loadOrInferManifest(task, workspaceDir)
	if err != nil {
		s.addLog(task, fmt.Sprintf("自动注册应用失败: %v", err))
		return
	}

	if manifest == nil {
		s.addLog(task, "未检测到 app_manifest.json 且无法推断启动方式，跳过应用自动注册")
		return
	}

	entryURL := manifest.EntryURL
	if entryURL == "" {
		entryURL = s.previewEntryURL(task)
	}
	if entryURL == "" {
		s.addLog(task, "自动注册应用失败：缺少可访问的入口 URL")
		return
	}

	appName := manifest.Name
	if appName == "" {
		appName = s.deriveAppName(task)
	}

	req := &models.CreateAppRequest{
		Name:          appName,
		Description:   manifest.Description,
		Category:      manifest.Category,
		Icon:          manifest.Icon,
		Cover:         manifest.Cover,
		EntryURL:      entryURL,
		TaskID:        task.ID,
		WorkspacePath: task.ID,
		StartCommand:  manifest.StartCommand.ToSlice(),
		Tags:          manifest.Tags,
	}

	app, err := s.appService.CreateApp(req)
	if err != nil {
		s.addLog(task, fmt.Sprintf("自动注册应用失败: %v", err))
		return
	}

	if fromFile {
		s.addLog(task, fmt.Sprintf("已自动注册应用：%s (%s)", app.Name, app.EntryURL))
	} else {
		s.addLog(task, fmt.Sprintf("未检测到 manifest，使用默认配置注册应用：%s (%s)", app.Name, app.EntryURL))
	}
}

func (s *TaskService) loadOrInferManifest(task *models.Task, workspaceDir string) (*models.AppManifest, bool, error) {
	if manifest, err := s.loadManifestFromFile(workspaceDir); err != nil {
		return nil, false, err
	} else if manifest != nil {
		return manifest, true, nil
	}

	fallback := s.buildFallbackManifest(task, workspaceDir)
	if fallback == nil {
		return nil, false, nil
	}
	return fallback, false, nil
}

func (s *TaskService) loadManifestFromFile(workspaceDir string) (*models.AppManifest, error) {
	manifestPath := filepath.Join(workspaceDir, "app_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 app_manifest.json 失败: %w", err)
	}

	manifest, err := models.ParseAppManifest(data)
	if err != nil {
		return nil, fmt.Errorf("解析 app_manifest.json 失败: %w", err)
	}
	return manifest, nil
}

func (s *TaskService) buildFallbackManifest(task *models.Task, workspaceDir string) *models.AppManifest {
	startCmd := s.detectStartCommand(workspaceDir)
	if len(startCmd) == 0 {
		return nil
	}

	entryURL := s.previewEntryURL(task)
	if entryURL == "" {
		return nil
	}

	return &models.AppManifest{
		Name:         s.deriveAppName(task),
		Description:  fmt.Sprintf("自动注册于 %s", time.Now().Format("2006-01-02 15:04")),
		EntryURL:     entryURL,
		StartCommand: models.ManifestCommand(startCmd),
		Tags:         []string{"auto-generated"},
	}
}

func (s *TaskService) deriveAppName(task *models.Task) string {
	if task == nil {
		return "AI 应用"
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return fmt.Sprintf("AI 应用 %s", task.ID[:8])
	}
	runes := []rune(prompt)
	if len(runes) > 16 {
		prompt = string(runes[:16]) + "..."
	}
	return prompt
}

func (s *TaskService) previewEntryURL(task *models.Task) string {
	if task == nil {
		return ""
	}
	frontend := strings.TrimRight(s.config.FrontendURL, "/")
	if frontend == "" {
		return ""
	}
	return fmt.Sprintf("%s?app=%s", frontend, task.ID)
}

func (s *TaskService) detectStartCommand(workspaceDir string) []string {
	appBinary := filepath.Join(workspaceDir, "app")
	if fi, err := os.Stat(appBinary); err == nil && !fi.IsDir() {
		return []string{"./app"}
	}

	if nodeCmd := detectNodeStartCommand(workspaceDir); len(nodeCmd) > 0 {
		return nodeCmd
	}

	if entry, pythonExec := findPythonEntry(workspaceDir); entry != "" && pythonExec != "" {
		return []string{pythonExec, entry}
	}

	// 检测 HTML 文件（静态 web 应用）
	if htmlEntry := findHTMLEntry(workspaceDir); htmlEntry != "" {
		// HTML 应用不需要启动命令，只需要提供访问路径
		return []string{"echo", "HTML app - access via entry_url"}
	}

	return nil
}

func detectNodeStartCommand(workspaceDir string) []string {
	pkgPath := filepath.Join(workspaceDir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	if pkg.Scripts == nil {
		return nil
	}

	if _, ok := pkg.Scripts["dev"]; ok {
		return []string{"npm", "run", "dev"}
	}
	if _, ok := pkg.Scripts["start"]; ok {
		return []string{"npm", "run", "start"}
	}
	return nil
}

func (s *TaskService) runInterpretedCode(task *models.Task, workspaceDir string) (bool, error) {
	if entry, pythonExec := findPythonEntry(workspaceDir); entry != "" && pythonExec != "" {
		cmd := exec.Command(pythonExec, entry)
		cmd.Dir = workspaceDir
		return true, s.runCommandWithLogs(task, cmd, fmt.Sprintf("使用 %s 执行 %s...", pythonExec, entry))
	}

	return false, nil
}

func (s *TaskService) runCommandWithLogs(task *models.Task, cmd *exec.Cmd, startMessage string) error {
	if startMessage != "" {
		s.addLog(task, startMessage)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to capture stdout: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to capture stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	stream := func(reader io.Reader, prefix string) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			if prefix != "" {
				line = fmt.Sprintf("%s %s", prefix, line)
			}
			s.addLog(task, line)
		}
		if err := scanner.Err(); err != nil {
			s.addLog(task, fmt.Sprintf("日志流读取失败: %v", err))
		}
	}

	go stream(stdout, "")
	go stream(stderr, "[stderr]")

	wgDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(wgDone)
	}()

	if err := cmd.Wait(); err != nil {
		<-wgDone
		return fmt.Errorf("command execution failed: %w", err)
	}

	<-wgDone
	s.addLog(task, "程序执行完成")
	return nil
}

func findPythonEntry(workspaceDir string) (string, string) {
	candidates := []string{"main.py", "app.py"}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(workspaceDir, candidate)); err == nil {
			if python := detectPythonExecutable(); python != "" {
				return candidate, python
			}
			return "", ""
		}
	}

	var fallback string
	filepath.WalkDir(workspaceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".py") && fallback == "" {
			rel, relErr := filepath.Rel(workspaceDir, path)
			if relErr == nil {
				fallback = rel
			}
		}
		return nil
	})

	if fallback != "" {
		if python := detectPythonExecutable(); python != "" {
			return fallback, python
		}
	}

	return "", ""
}

func detectPythonExecutable() string {
	for _, candidate := range []string{"python3", "python"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func findHTMLEntry(workspaceDir string) string {
	// 优先查找 index.html
	candidates := []string{"index.html", "index.htm"}
	for _, candidate := range candidates {
		htmlPath := filepath.Join(workspaceDir, candidate)
		if _, err := os.Stat(htmlPath); err == nil {
			return candidate
		}
	}

	// 如果没有 index.html，查找任何 HTML 文件
	var fallback string
	filepath.WalkDir(workspaceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lowerName := strings.ToLower(d.Name())
		if (strings.HasSuffix(lowerName, ".html") || strings.HasSuffix(lowerName, ".htm")) && fallback == "" {
			rel, relErr := filepath.Rel(workspaceDir, path)
			if relErr == nil {
				fallback = rel
			}
		}
		return nil
	})

	return fallback
}

// Helper methods
func (s *TaskService) updateStatus(task *models.Task, status models.TaskStatus, message string) {
	s.mu.Lock()
	task.Status = status
	s.mu.Unlock()

	// Log to console for debugging
	fmt.Printf("[Task %s] Status: %s - %s\n", task.ID[:8], status, message)

	s.wsManager.BroadcastProgress(&models.ProgressUpdate{
		TaskID:  task.ID,
		Status:  status,
		Message: message,
	})
}

func (s *TaskService) addLog(task *models.Task, log string) {
	s.mu.Lock()
	task.Logs = append(task.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), log))
	s.mu.Unlock()

	// Log to console for debugging
	fmt.Printf("[Task %s] LOG: %s\n", task.ID[:8], log)

	s.wsManager.BroadcastProgress(&models.ProgressUpdate{
		TaskID: task.ID,
		Log:    log,
	})
}

func (s *TaskService) failTask(task *models.Task, errMsg string) {
	s.mu.Lock()
	task.Status = models.StatusFailed
	task.Error = errMsg
	now := time.Now()
	task.CompletedAt = &now
	s.mu.Unlock()

	// Log error to console for debugging
	fmt.Printf("[Task %s] ❌ FAILED: %s\n", task.ID[:8], errMsg)

	s.wsManager.BroadcastProgress(&models.ProgressUpdate{
		TaskID:  task.ID,
		Status:  models.StatusFailed,
		Message: errMsg,
		Error:   errMsg, // Include error field for frontend display
	})
}

func (s *TaskService) completeTask(task *models.Task) {
	s.mu.Lock()
	task.Status = models.StatusCompleted
	now := time.Now()
	task.CompletedAt = &now
	s.mu.Unlock()

	// Log success to console
	fmt.Printf("[Task %s] ✅ COMPLETED successfully!\n", task.ID[:8])

	s.wsManager.BroadcastProgress(&models.ProgressUpdate{
		TaskID:  task.ID,
		Status:  models.StatusCompleted,
		Message: "Task completed successfully!",
	})
}
