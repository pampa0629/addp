package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CodexCLIClient struct {
	binaryPath string
	extraArgs  []string
	timeout    time.Duration
	env        map[string]string
}

func NewCodexCLIClient(cfg *Config) *CodexCLIClient {
	args := cfg.CodexCLIArgs
	if len(args) == 0 {
		args = []string{"--skip-git-repo-check"}
	}

	timeout := cfg.CodexCLITimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	env := make(map[string]string)
	if cfg.CodexAPIKey != "" {
		env["OPENAI_API_KEY"] = cfg.CodexAPIKey
		env["CODEX_API_KEY"] = cfg.CodexAPIKey
	}
	if cfg.CodexBaseURL != "" {
		env["OPENAI_BASE_URL"] = cfg.CodexBaseURL
		env["CODEX_API_BASE"] = cfg.CodexBaseURL
	}
	if cfg.CodexModel != "" {
		env["CODEX_MODEL"] = cfg.CodexModel
	}

	return &CodexCLIClient{
		binaryPath: cfg.CodexCLIPath,
		extraArgs:  args,
		timeout:    timeout,
		env:        env,
	}
}

func (c *CodexCLIClient) GenerateCode(prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt cannot be empty")
	}

	workDir, err := os.MkdirTemp("", "codex-cli-work-")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	combinedPrompt := buildCLIPrompt(prompt)

	stdout, stderr, err := c.runCLI(workDir, combinedPrompt)
	if err != nil {
		return "", fmt.Errorf("codex cli failed: %w\nstdout: %s\nstderr: %s", err, truncateString(string(stdout), 4000), truncateString(string(stderr), 4000))
	}

	lastMessage := extractLastAgentMessage(stdout)
	if lastMessage != "" {
		log.Printf("Codex CLI summary: %s", truncateString(lastMessage, 512))
	}

	code, err := collectWorkspaceFiles(workDir)
	if err != nil {
		return "", fmt.Errorf("failed to collect files from Codex workspace: %w", err)
	}

	if strings.TrimSpace(code) == "" {
		code = lastMessage
	}

	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("Codex CLI did not produce any files or textual output")
	}

	return code, nil
}

func (c *CodexCLIClient) runCLI(workDir, prompt string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	args := []string{"exec", "--json", "--cd", workDir}
	args = append(args, c.extraArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	env := os.Environ()
	for k, v := range c.env {
		if v == "" {
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func buildCLIPrompt(userPrompt string) string {
	base := `You are an autonomous coding agent. Generate working code for the user's request.
- Prefer creating the necessary project files directly in the current workspace.
- Keep responses concise; the backend will capture file contents separately.`
	return fmt.Sprintf("%s\n\nUser request:\n%s", base, strings.TrimSpace(userPrompt))
}

func truncateString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func extractLastAgentMessage(output []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	buf := make([]byte, 0, 1024)
	scanner.Buffer(buf, 10*1024*1024)

	type cliItem struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type cliEvent struct {
		Type string   `json:"type"`
		Item *cliItem `json:"item"`
	}

	var lastMessage string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var evt cliEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		if evt.Type == "item.completed" && evt.Item != nil && evt.Item.Type == "agent_message" {
			lastMessage = evt.Item.Text
		}
	}

	return lastMessage
}

func collectWorkspaceFiles(root string) (string, error) {
	builder := &strings.Builder{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path == root {
				return nil
			}

			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if strings.HasPrefix(rel, ".") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		builder.WriteString("// filepath: ")
		builder.WriteString(rel)
		builder.WriteString("\n")
		builder.Write(data)
		builder.WriteString("\n\n")
		return nil
	})

	if err != nil {
		return "", err
	}

	return builder.String(), nil
}

func (c *CodexCLIClient) Provider() string {
	return "codex_cli"
}
