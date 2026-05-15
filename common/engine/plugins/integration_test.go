package plugins_test

import (
	"sort"
	"testing"

	"github.com/addp/common/engine/plugin"

	// 导入所有插件以触发 init() 注册
	_ "github.com/addp/common/engine/plugins/builtin/all"
)

func TestAllPluginsRegistered(t *testing.T) {
	expectedTypes := []string{
		"clickhouse",
		"doris",
		"jupyter",
		"math_workflow",
		"minio",
		"mongodb",
		"mysql",
		"neo4j",
		"nfs",
		"postgresql",
		"python_workflow",
		"s3",
		"spark",
		"spark_workflow",
	}

	registeredTypes := plugin.List()
	sort.Strings(registeredTypes)
	sort.Strings(expectedTypes)

	if len(registeredTypes) != len(expectedTypes) {
		t.Errorf("Expected %d plugins, got %d", len(expectedTypes), len(registeredTypes))
		t.Logf("Expected: %v", expectedTypes)
		t.Logf("Registered: %v", registeredTypes)
		return
	}

	for i, expected := range expectedTypes {
		if registeredTypes[i] != expected {
			t.Errorf("Expected plugin '%s' at index %d, got '%s'", expected, i, registeredTypes[i])
		}
	}
}

func TestGetAllPlugins(t *testing.T) {
	plugins := plugin.GetAll()

	if len(plugins) != 14 {
		t.Errorf("Expected 14 plugins, got %d", len(plugins))
	}

	// 验证每个插件的基本信息
	testCases := []struct {
		dbType   string
		expected string
	}{
		{"postgresql", "PostgreSQL"},
		{"mysql", "MySQL"},
		{"doris", "Apache Doris"},
		{"spark", "Apache Spark"},
		{"clickhouse", "ClickHouse"},
		{"jupyter", "Jupyter Engine"},
		{"math_workflow", "Math Workflow"},
		{"mongodb", "MongoDB"},
		{"neo4j", "Neo4j"},
		{"nfs", "NFS 文件系统"},
		{"minio", "MinIO"},
		{"python_workflow", "Python Workflow"},
		{"s3", "Amazon S3"},
		{"spark_workflow", "Spark Workflow"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.DisplayName() != tc.expected {
			t.Errorf("Expected display name '%s' for '%s', got '%s'",
				tc.expected, tc.dbType, p.DisplayName())
		}
	}
}

func TestPluginCapabilities(t *testing.T) {
	testCases := []struct {
		dbType string
		origin string
	}{
		{"postgresql", "general"},
		{"mysql", "general"},
		{"doris", "general"},
		{"clickhouse", "general"},
		{"mongodb", "general"},
		{"spark", "general"},
		{"minio", "general"},
		{"s3", "general"},
		{"nfs", "general"},
		{"neo4j", "general"},
		{"python_workflow", "extension"},
		{"spark_workflow", "extension"},
		{"math_workflow", "extension"},
		{"jupyter", "extension"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.EngineOrigin() != tc.origin {
			t.Errorf("Expected origin '%s' for '%s', got '%s'",
				tc.origin, tc.dbType, p.EngineOrigin())
		}

		// 验证能力声明不为空
		capabilities, err := plugin.GenerateCapabilities(tc.dbType)
		if err != nil {
			t.Errorf("Plugin '%s' failed to generate capabilities: %v", tc.dbType, err)
			continue
		}
		if capabilities == "" {
			t.Errorf("Plugin '%s' returned empty capabilities", tc.dbType)
		}
	}
}

func TestPluginDefaultPorts(t *testing.T) {
	testCases := []struct {
		dbType      string
		defaultPort int
	}{
		{"postgresql", 5432},
		{"mysql", 3306},
		{"doris", 9030},
		{"clickhouse", 9000},
		{"mongodb", 27017},
		{"spark", 10000},
		{"minio", 9000},
		{"s3", 443},
		{"nfs", 2049},
		{"neo4j", 7687},
		{"python_workflow", 8099},
		{"spark_workflow", 8098},
		{"math_workflow", 8089},
		{"jupyter", 8097},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.DefaultPort() != tc.defaultPort {
			t.Errorf("Expected default port %d for '%s', got %d",
				tc.defaultPort, tc.dbType, p.DefaultPort())
		}
	}
}

func TestPluginRequiredFields(t *testing.T) {
	testCases := []struct {
		dbType   string
		hasField string
	}{
		{"postgresql", "host"},
		{"mysql", "user"},
		{"doris", "database"},
		{"clickhouse", "host"},
		{"mongodb", "host"},
		{"spark", "host"},
		{"minio", "endpoint"},
		{"s3", "access_key"},
		{"nfs", "server"},
		{"neo4j", "password"},
		{"python_workflow", "host"},
		{"spark_workflow", "port"},
		{"math_workflow", "host"},
		{"jupyter", "port"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		requiredFields := p.RequiredFields()
		found := false
		for _, field := range requiredFields {
			if field == tc.hasField {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected required field '%s' for '%s', not found in %v",
				tc.hasField, tc.dbType, requiredFields)
		}
	}
}

func TestPluginSensitiveFields(t *testing.T) {
	allPlugins := plugin.GetAll()

	for dbType, p := range allPlugins {
		if p.EngineOrigin() == "extension" || dbType == "nfs" {
			continue
		}
		sensitiveFields := p.SensitiveFields()
		if len(sensitiveFields) == 0 {
			t.Errorf("Plugin '%s' has no sensitive fields defined", dbType)
		}
	}
}

func TestPluginCapabilitiesMatchProviders(t *testing.T) {
	for engineType, p := range plugin.GetAll() {
		t.Run(engineType, func(t *testing.T) {
			if err := plugin.ValidatePluginCapabilities(p); err != nil {
				t.Fatal(err)
			}
		})
	}
}
