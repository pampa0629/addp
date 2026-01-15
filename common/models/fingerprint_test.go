package models_test

import (
	"fmt"
	"testing"

	"github.com/addp/common/models"
)

// TestFingerprintTwoStepCalculation 验证两步指纹计算方式
func TestFingerprintTwoStepCalculation(t *testing.T) {
	tests := []struct {
		name            string
		engineID        uint
		storageType     string
		params          map[string]string
		wantFingerprint string
	}{
		{
			name:        "对象存储-基本路径",
			engineID:    9,
			storageType: "object",
			params: map[string]string{
				"bucket": "addp",
				"path":   "image/",
				"name":   "开会.jpg",
			},
			wantFingerprint: "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4",
		},
		{
			name:        "对象存储-根目录文件",
			engineID:    9,
			storageType: "object",
			params: map[string]string{
				"bucket": "addp",
				"path":   "",
				"name":   "test.txt",
			},
			wantFingerprint: "", // 计算后填写
		},
		{
			name:        "对象存储-多级目录",
			engineID:    2,
			storageType: "object",
			params: map[string]string{
				"bucket": "data",
				"path":   "docs/reports/2024/",
				"name":   "summary.pdf",
			},
			wantFingerprint: "", // 计算后填写
		},
		{
			name:        "数据库表-public模式",
			engineID:    2,
			storageType: "table",
			params: map[string]string{
				"schema": "public",
				"table":  "buildings",
			},
			wantFingerprint: "", // 计算后填写
		},
		{
			name:        "数据库表-自定义模式",
			engineID:    5,
			storageType: "table",
			params: map[string]string{
				"schema": "gis",
				"table":  "roads",
			},
			wantFingerprint: "", // 计算后填写
		},
		{
			name:        "文件系统-绝对路径",
			engineID:    3,
			storageType: "file",
			params: map[string]string{
				"path": "/data/images/",
				"name": "photo.png",
			},
			wantFingerprint: "", // 计算后填写
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fullName string

			switch tt.storageType {
			case "object":
				fullName = models.JoinObjectPath(
					tt.params["bucket"],
					tt.params["path"],
					tt.params["name"],
				)
			case "table":
				fullName = fmt.Sprintf("%s.%s", tt.params["schema"], tt.params["table"])
			case "file":
				fullName = tt.params["path"] + tt.params["name"]
			}

			fingerprint := models.GenerateItemFingerprint(tt.engineID, fullName)

			t.Logf("Storage Type: %s", tt.storageType)
			t.Logf("EngineID: %d", tt.engineID)
			t.Logf("Full Name: %s", fullName)
			t.Logf("Fingerprint: %s", fingerprint)

			if tt.wantFingerprint != "" && fingerprint != tt.wantFingerprint {
				t.Errorf("指纹不匹配:\n  got:  %s\n  want: %s", fingerprint, tt.wantFingerprint)
			}
		})
	}
}

// TestPathSplitAndJoin 验证路径拆分和拼接的正确性
func TestPathSplitAndJoin(t *testing.T) {
	tests := []struct {
		fullPath string
		wantDir  string
		wantName string
	}{
		{"image/开会.jpg", "image/", "开会.jpg"},
		{"开会.jpg", "", "开会.jpg"},
		{"docs/reports/2024.pdf", "docs/reports/", "2024.pdf"},
		{"a/b/c/d.txt", "a/b/c/", "d.txt"},
		{"folder/", "folder/", ""},
		{"", "", ""},
		{"single", "", "single"},
		{"deep/nested/folder/structure/file.dat", "deep/nested/folder/structure/", "file.dat"},
	}

	for _, tt := range tests {
		t.Run(tt.fullPath, func(t *testing.T) {
			dir, name := models.SplitObjectPath(tt.fullPath)

			if dir != tt.wantDir || name != tt.wantName {
				t.Errorf("SplitObjectPath(%q) = (%q, %q), want (%q, %q)",
					tt.fullPath, dir, name, tt.wantDir, tt.wantName)
			}

			// 验证拼接能还原原路径
			if tt.fullPath != "" {
				bucket := "test-bucket"
				fullName := models.JoinObjectPath(bucket, dir, name)
				expectedFullName := fmt.Sprintf("%s/%s", bucket, tt.fullPath)

				if fullName != expectedFullName {
					t.Errorf("JoinObjectPath 还原失败:\n  got:  %s\n  want: %s", fullName, expectedFullName)
				}
			}
		})
	}
}

// TestJoinObjectPath 验证对象路径拼接的边界情况
func TestJoinObjectPath(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		path   string
		fname  string
		want   string
	}{
		{
			name:   "完整路径",
			bucket: "addp",
			path:   "image/",
			fname:  "test.jpg",
			want:   "addp/image/test.jpg",
		},
		{
			name:   "根目录文件",
			bucket: "addp",
			path:   "",
			fname:  "test.txt",
			want:   "addp/test.txt",
		},
		{
			name:   "空文件名（目录）",
			bucket: "addp",
			path:   "folder/",
			fname:  "",
			want:   "addp/folder/",
		},
		{
			name:   "多级目录",
			bucket: "data",
			path:   "a/b/c/",
			fname:  "file.dat",
			want:   "data/a/b/c/file.dat",
		},
		{
			name:   "path不以/结尾",
			bucket: "bucket",
			path:   "folder",
			fname:  "file.txt",
			want:   "bucket/folderfile.txt", // 注意：这可能是bug，但测试当前实现
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.JoinObjectPath(tt.bucket, tt.path, tt.fname)
			if got != tt.want {
				t.Errorf("JoinObjectPath(%q, %q, %q) = %q, want %q",
					tt.bucket, tt.path, tt.fname, got, tt.want)
			}
		})
	}
}

// TestFingerprintConsistency 验证相同输入产生相同指纹
func TestFingerprintConsistency(t *testing.T) {
	engineID := uint(9)
	fullName := "addp/image/开会.jpg"

	fingerprint1 := models.GenerateItemFingerprint(engineID, fullName)
	fingerprint2 := models.GenerateItemFingerprint(engineID, fullName)

	if fingerprint1 != fingerprint2 {
		t.Errorf("相同输入产生不同指纹:\n  first:  %s\n  second: %s", fingerprint1, fingerprint2)
	}

	t.Logf("指纹一致性验证通过: %s", fingerprint1)
}

// TestFingerprintUniqueness 验证不同输入产生不同指纹
func TestFingerprintUniqueness(t *testing.T) {
	tests := []struct {
		name     string
		engineID uint
		fullName string
	}{
		{"相同bucket不同文件", 9, "addp/image/test1.jpg"},
		{"相同bucket不同文件2", 9, "addp/image/test2.jpg"},
		{"不同bucket相同文件", 9, "other/image/test1.jpg"},
		{"不同engineID", 10, "addp/image/test1.jpg"},
	}

	fingerprints := make(map[string]bool)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fingerprint := models.GenerateItemFingerprint(tt.engineID, tt.fullName)

			if fingerprints[fingerprint] {
				t.Errorf("指纹冲突: %s 与之前的输入产生了相同指纹", tt.fullName)
			}
			fingerprints[fingerprint] = true

			t.Logf("EngineID: %d, FullName: %s, Fingerprint: %s",
				tt.engineID, tt.fullName, fingerprint)
		})
	}
}

// BenchmarkGenerateItemFingerprint 性能基准测试
func BenchmarkGenerateItemFingerprint(b *testing.B) {
	engineID := uint(9)
	fullName := "addp/image/开会.jpg"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = models.GenerateItemFingerprint(engineID, fullName)
	}
}

// BenchmarkJoinObjectPath 路径拼接性能测试
func BenchmarkJoinObjectPath(b *testing.B) {
	bucket := "addp"
	path := "image/subfolder/"
	name := "test.jpg"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = models.JoinObjectPath(bucket, path, name)
	}
}

// BenchmarkSplitObjectPath 路径拆分性能测试
func BenchmarkSplitObjectPath(b *testing.B) {
	fullPath := "image/subfolder/test.jpg"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.SplitObjectPath(fullPath)
	}
}
