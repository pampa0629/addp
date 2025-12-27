package plugins_test

import (
	"sort"
	"testing"

	"github.com/addp/common/database/plugin"

	// 导入所有插件以触发 init() 注册
	_ "github.com/addp/common/database/plugins/clickhouse"
	_ "github.com/addp/common/database/plugins/doris"
	_ "github.com/addp/common/database/plugins/minio"
	_ "github.com/addp/common/database/plugins/mongodb"
	_ "github.com/addp/common/database/plugins/mysql"
	_ "github.com/addp/common/database/plugins/postgresql"
	_ "github.com/addp/common/database/plugins/s3"
	_ "github.com/addp/common/database/plugins/spark_sql"
)

func TestAllPluginsRegistered(t *testing.T) {
	expectedTypes := []string{
		"clickhouse",
		"doris",
		"minio",
		"mongodb",
		"mysql",
		"postgresql",
		"s3",
		"spark_sql",
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

	if len(plugins) != 8 {
		t.Errorf("Expected 8 plugins, got %d", len(plugins))
	}

	// 验证每个插件的基本信息
	testCases := []struct {
		dbType   string
		expected string
	}{
		{"postgresql", "PostgreSQL"},
		{"mysql", "MySQL"},
		{"doris", "Apache Doris"},
		{"spark_sql", "Spark SQL"},
		{"clickhouse", "ClickHouse"},
		{"mongodb", "MongoDB"},
		{"minio", "MinIO"},
		{"s3", "Amazon S3"},
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
		dbType   string
		category string
	}{
		{"postgresql", "relational_db"},
		{"mysql", "relational_db"},
		{"doris", "relational_db"},
		{"clickhouse", "relational_db"},
		{"mongodb", "nosql_db"},
		{"spark_sql", "compute_engine"},
		{"minio", "object_storage"},
		{"s3", "object_storage"},
	}

	for _, tc := range testCases {
		p, err := plugin.Get(tc.dbType)
		if err != nil {
			t.Errorf("Failed to get plugin for '%s': %v", tc.dbType, err)
			continue
		}

		if p.ConnectionCategory() != tc.category {
			t.Errorf("Expected category '%s' for '%s', got '%s'",
				tc.category, tc.dbType, p.ConnectionCategory())
		}

		// 验证能力字符串不为空
		capabilities := p.GenerateCapabilities()
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
		{"spark_sql", 10000},
		{"minio", 9000},
		{"s3", 443},
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
		{"spark_sql", "host"},
		{"minio", "endpoint"},
		{"s3", "access_key"},
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
	// 所有插件都应该有 password 或 secret_key 作为敏感字段
	allPlugins := plugin.GetAll()

	for dbType, p := range allPlugins {
		sensitiveFields := p.SensitiveFields()
		if len(sensitiveFields) == 0 {
			t.Errorf("Plugin '%s' has no sensitive fields defined", dbType)
		}
	}
}
