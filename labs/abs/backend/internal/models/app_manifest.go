package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// AppManifest 描述 AI 生成应用的运行元数据
type AppManifest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	Icon         string          `json:"icon"`
	Cover        string          `json:"cover"`
	EntryURL     string          `json:"entry_url"`
	Host         string          `json:"host"`
	Port         int             `json:"port"`
	PreviewPath  string          `json:"preview_path"`
	StartCommand ManifestCommand `json:"start_command"`
	Tags         []string        `json:"tags"`
}

// ManifestCommand 支持 string 或 string 数组
type ManifestCommand []string

// ParseAppManifest 解析并标准化 manifest 内容
func ParseAppManifest(data []byte) (*AppManifest, error) {
	var manifest AppManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid app manifest: %w", err)
	}

	manifest.normalize()
	return &manifest, nil
}

// ToSlice 返回命令的切片副本
func (m ManifestCommand) ToSlice() []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, len(m))
	copy(out, m)
	return out
}

// UnmarshalJSON 支持 string/[]string
func (m *ManifestCommand) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	switch trimmed[0] {
	case '[':
		var arr []string
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		*m = cleanCommandParts(arr)
		return nil
	case '"', '\'':
		var cmd string
		if err := json.Unmarshal(trimmed, &cmd); err != nil {
			return err
		}
		*m = splitCommandString(cmd)
		return nil
	default:
		return fmt.Errorf("unsupported start_command format")
	}
}

func (m *AppManifest) normalize() {
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	m.Category = strings.TrimSpace(m.Category)
	m.Icon = strings.TrimSpace(m.Icon)
	m.Cover = strings.TrimSpace(m.Cover)
	m.EntryURL = strings.TrimSpace(m.EntryURL)
	m.Host = strings.TrimSpace(m.Host)
	m.PreviewPath = strings.TrimSpace(m.PreviewPath)

	if len(m.StartCommand) > 0 {
		m.StartCommand = ManifestCommand(cleanCommandParts(m.StartCommand))
	}

	m.Tags = normalizeTags(m.Tags)

	if m.EntryURL == "" && m.Port != 0 {
		host := m.Host
		if host == "" {
			host = "http://localhost"
		}
		host = strings.TrimRight(host, "/")
		path := strings.TrimPrefix(m.PreviewPath, "/")
		if path != "" {
			m.EntryURL = fmt.Sprintf("%s:%d/%s", host, m.Port, path)
		} else {
			m.EntryURL = fmt.Sprintf("%s:%d", host, m.Port)
		}
	}
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, t)
	}
	return result
}

func cleanCommandParts(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func splitCommandString(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	var quote rune

	for _, r := range input {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}
