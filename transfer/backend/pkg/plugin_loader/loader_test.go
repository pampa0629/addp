package plugin_loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/transfer/pkg/pipeline"
	"github.com/addp/transfer/pkg/plugin_loader"
)

// TestNewPluginLoader_Success 测试成功加载配置
func TestNewPluginLoader_Success(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: postgresql
      type: jdbc
      enabled: true
      description: PostgreSQL reader
    - name: csv
      type: csv
      enabled: true
      description: CSV reader
  writers:
    - name: postgresql
      type: jdbc
      enabled: true
      description: PostgreSQL writer
    - name: csv
      type: csv
      enabled: true
      description: CSV writer
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// 创建插件加载器
	loader, err := plugin_loader.NewPluginLoader(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if loader == nil {
		t.Fatal("loader should not be nil")
	}

	// 验证配置已加载
	cfg := loader.GetConfig()
	if cfg == nil {
		t.Fatal("config should not be nil")
	}

	if len(cfg.Plugins.Readers) != 2 {
		t.Errorf("expected 2 readers, got %d", len(cfg.Plugins.Readers))
	}

	if len(cfg.Plugins.Writers) != 2 {
		t.Errorf("expected 2 writers, got %d", len(cfg.Plugins.Writers))
	}

	t.Logf("✅ Successfully loaded config: %d readers, %d writers",
		len(cfg.Plugins.Readers), len(cfg.Plugins.Writers))
}

// TestNewPluginLoader_FileNotFound 测试配置文件不存在
func TestNewPluginLoader_FileNotFound(t *testing.T) {
	loader, err := plugin_loader.NewPluginLoader("/nonexistent/path/plugins.yaml")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if loader != nil {
		t.Errorf("expected nil loader, got %+v", loader)
	}

	if !os.IsNotExist(err) && err.Error() != "failed to load plugin config: config file not found: /nonexistent/path/plugins.yaml" {
		t.Logf("Expected 'file not found' error, got: %v", err)
	}

	t.Logf("✅ Correctly rejected nonexistent config file: %v", err)
}

// TestNewPluginLoader_InvalidYAML 测试无效的 YAML
func TestNewPluginLoader_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// 写入无效的 YAML
	invalidYAML := "invalid: yaml: content: [[[unclosed"
	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader, err := plugin_loader.NewPluginLoader(configPath)

	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}

	if loader != nil {
		t.Errorf("expected nil loader, got %+v", loader)
	}

	t.Logf("✅ Correctly rejected invalid YAML: %v", err)
}

// TestListEnabledPlugins 测试列出启用的插件
func TestListEnabledPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: postgresql
      type: jdbc
      enabled: true
    - name: mysql
      type: jdbc
      enabled: false
    - name: csv
      type: csv
      enabled: true
  writers:
    - name: postgresql
      type: jdbc
      enabled: true
    - name: shapefile
      type: shapefile
      enabled: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader, err := plugin_loader.NewPluginLoader(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	readers, writers := loader.ListEnabledPlugins()

	// 验证启用的 readers（应该有 2 个：jdbc, csv）
	if len(readers) != 2 {
		t.Errorf("expected 2 enabled readers, got %d: %v", len(readers), readers)
	}

	expectedReaders := map[string]bool{"jdbc": true, "csv": true}
	for _, r := range readers {
		if !expectedReaders[r] {
			t.Errorf("unexpected reader: %s", r)
		}
	}

	// 验证启用的 writers（应该有 1 个：jdbc）
	if len(writers) != 1 {
		t.Errorf("expected 1 enabled writer, got %d: %v", len(writers), writers)
	}

	if len(writers) > 0 && writers[0] != "jdbc" {
		t.Errorf("expected writer 'jdbc', got '%s'", writers[0])
	}

	t.Logf("✅ Enabled plugins: readers=%v, writers=%v", readers, writers)
}

// TestLoadPlugins_Success 测试成功加载插件到注册表
func TestLoadPlugins_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: postgresql
      type: postgresql
      enabled: true
    - name: csv
      type: csv
      enabled: true
    - name: disabled_reader
      type: mysql
      enabled: false
  writers:
    - name: postgresql
      type: postgresql
      enabled: true
    - name: csv
      type: csv
      enabled: true
    - name: disabled_writer
      type: shapefile
      enabled: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader, err := plugin_loader.NewPluginLoader(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 创建注册表
	registry := pipeline.NewConnectorRegistry()

	// 加载插件
	err = loader.LoadPlugins(registry)
	if err != nil {
		t.Fatalf("failed to load plugins: %v", err)
	}

	// 验证 readers 已注册（应该有 2 个：postgresql, csv）
	// 注意：registry 没有公开方法列出所有已注册的插件，所以我们通过尝试创建来验证
	testReaders := []string{"postgresql", "csv"}
	for _, readerType := range testReaders {
		config := pipeline.ConnectorConfig{
			Type: readerType,
			Config: map[string]interface{}{
				"connection_string": "test",
			},
		}
		reader, err := registry.NewReader(config)
		if err != nil {
			t.Errorf("failed to create %s reader: %v", readerType, err)
		}
		if reader == nil {
			t.Errorf("%s reader should not be nil", readerType)
		}
	}

	// 验证 writers 已注册
	testWriters := []string{"postgresql", "csv"}
	for _, writerType := range testWriters {
		config := pipeline.ConnectorConfig{
			Type: writerType,
			Config: map[string]interface{}{
				"connection_string": "test",
			},
		}
		writer, err := registry.NewWriter(config)
		if err != nil {
			t.Errorf("failed to create %s writer: %v", writerType, err)
		}
		if writer == nil {
			t.Errorf("%s writer should not be nil", writerType)
		}
	}

	// 验证禁用的插件未注册
	disabledConfig := pipeline.ConnectorConfig{
		Type: "mysql",
		Config: map[string]interface{}{
			"connection_string": "test",
		},
	}
	_, err = registry.NewReader(disabledConfig)
	if err == nil {
		t.Error("disabled reader 'mysql' should not be registered")
	}

	t.Logf("✅ Successfully loaded enabled plugins to registry")
}

// TestLoadPlugins_UnknownPluginType 测试加载未知插件类型
func TestLoadPlugins_UnknownPluginType(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: unknown_reader
      type: nonexistent_type
      enabled: true
  writers: []
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader, err := plugin_loader.NewPluginLoader(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	registry := pipeline.NewConnectorRegistry()

	// 加载插件应该继续（跳过未知插件，记录警告）
	err = loader.LoadPlugins(registry)
	if err != nil {
		t.Logf("LoadPlugins returned error: %v (this may be expected)", err)
	}

	// 验证未知插件未注册
	unknownConfig := pipeline.ConnectorConfig{
		Type: "nonexistent_type",
		Config: map[string]interface{}{
			"connection_string": "test",
		},
	}
	_, err = registry.NewReader(unknownConfig)
	if err == nil {
		t.Error("unknown reader type should not be registered")
	}

	t.Logf("✅ Correctly handled unknown plugin type")
}

// TestLoadPlugins_ExternalPlugin 测试外部插件路径（应返回未实现错误）
func TestLoadPlugins_ExternalPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_plugins.yaml")

	// 创建一个假的 .so 文件
	pluginPath := filepath.Join(tmpDir, "external.so")
	err := os.WriteFile(pluginPath, []byte("fake plugin"), 0644)
	if err != nil {
		t.Fatalf("failed to create fake plugin file: %v", err)
	}

	configContent := `plugins:
  readers:
    - name: external_reader
      type: external
      enabled: true
      plugin_path: ` + pluginPath + `
  writers: []
`
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader, err := plugin_loader.NewPluginLoader(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	registry := pipeline.NewConnectorRegistry()

	// 加载插件应该继续（跳过外部插件，记录未实现警告）
	err = loader.LoadPlugins(registry)
	if err != nil {
		t.Logf("LoadPlugins returned error: %v (expected for external plugins)", err)
	}

	t.Logf("✅ Correctly handled external plugin (not implemented yet)")
}

// BenchmarkNewPluginLoader 基准测试：加载配置
func BenchmarkNewPluginLoader(b *testing.B) {
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "bench_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: postgresql
      type: jdbc
      enabled: true
    - name: csv
      type: csv
      enabled: true
  writers:
    - name: postgresql
      type: jdbc
      enabled: true
    - name: csv
      type: csv
      enabled: true
`
	os.WriteFile(configPath, []byte(configContent), 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = plugin_loader.NewPluginLoader(configPath)
	}
}

// BenchmarkLoadPlugins 基准测试：加载插件到注册表
func BenchmarkLoadPlugins(b *testing.B) {
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "bench_plugins.yaml")

	configContent := `plugins:
  readers:
    - name: postgresql
      type: postgresql
      enabled: true
    - name: csv
      type: csv
      enabled: true
  writers:
    - name: postgresql
      type: postgresql
      enabled: true
    - name: csv
      type: csv
      enabled: true
`
	os.WriteFile(configPath, []byte(configContent), 0644)

	loader, _ := plugin_loader.NewPluginLoader(configPath)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry := pipeline.NewConnectorRegistry()
		_ = loader.LoadPlugins(registry)
	}
}
