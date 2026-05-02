//go:build integration
// +build integration

package plugin_test

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/doris"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/mysql"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/python_workflow"
	_ "github.com/addp/common/engine/plugins/s3"
)

// TestPluginInterfaceImplementation 验证所有插件正确实现了接口
func TestPluginInterfaceImplementation(t *testing.T) {
	// 测试所有已注册插件
	plugins := plugin.GetAll()

	if len(plugins) == 0 {
		t.Fatal("No plugins registered")
	}

	t.Logf("Found %d registered plugins", len(plugins))

	for dbType, p := range plugins {
		t.Run(dbType, func(t *testing.T) {
			// 1. 基础接口验证
			if p.Type() == "" {
				t.Error("Type() should not be empty")
			}
			if p.DisplayName() == "" {
				t.Error("DisplayName() should not be empty")
			}
			if p.EngineCategory() == "" {
				t.Error("EngineCategory() should not be empty")
			}

			category := p.EngineCategory()
			t.Logf("Plugin: %s, Category: %s", dbType, category)

			// 2. 按类别验证
			switch category {
			case "standard":
				capabilities := p.Capabilities()
				if capabilities.Storage == nil || len(capabilities.Storage.Families) == 0 {
					t.Errorf("standard engine should declare storage capabilities")
					return
				}

				// 如果支持元数据，验证具体接口
				if capabilities.Storage.Metadata != nil && capabilities.Storage.Metadata.Supported {
					// 检查是关系型DB还是对象存储
					if relPlugin, ok := p.(plugin.RelationalDBPlugin); ok {
						t.Logf("%s implements RelationalDBPlugin ✓", dbType)

						// 验证 IsSystemSchema 方法
						if dbType == "postgresql" {
							if !relPlugin.IsSystemSchema("pg_catalog") {
								t.Error("pg_catalog should be system schema")
							}
							if relPlugin.IsSystemSchema("public") {
								t.Error("public should not be system schema")
							}
						} else if dbType == "mysql" {
							if !relPlugin.IsSystemSchema("mysql") {
								t.Error("mysql should be system schema")
							}
							if relPlugin.IsSystemSchema("test") {
								t.Error("test should not be system schema")
							}
						}
					} else if objPlugin, ok := p.(plugin.ObjectStoragePlugin); ok {
						t.Logf("%s implements ObjectStoragePlugin ✓", dbType)

						// 验证 InferContentType 方法
						contentType := objPlugin.InferContentType("test.geojson")
						if contentType != "application/geo+json" {
							t.Errorf("expected application/geo+json, got %s", contentType)
						}

						contentType = objPlugin.InferContentType("test.shp")
						if contentType != "application/x-shapefile" {
							t.Errorf("expected application/x-shapefile, got %s", contentType)
						}
					} else {
						t.Errorf("%s supports metadata but doesn't implement specific plugin interface", dbType)
					}
				}

			case "extension":
				capabilities := p.Capabilities()
				if capabilities.Compute == nil {
					t.Errorf("extension engine should declare compute capabilities")
					return
				}
				t.Logf("%s declares compute capabilities ✓", dbType)

			default:
				t.Errorf("unknown EngineCategory: %s", category)
			}
		})
	}
}

// TestRelationalDBPlugins 验证关系型数据库插件
func TestRelationalDBPlugins(t *testing.T) {
	dbTypes := []string{"postgresql", "mysql"}

	for _, dbType := range dbTypes {
		t.Run(dbType, func(t *testing.T) {
			p, err := plugin.Get(dbType)
			if err != nil {
				t.Fatalf("plugin not found: %v", err)
			}

			// 验证类别
			if p.EngineCategory() != "standard" {
				t.Errorf("expected category 'standard', got '%s'", p.EngineCategory())
			}

			capabilities := p.Capabilities()
			if capabilities.Storage == nil || capabilities.Storage.Metadata == nil || !capabilities.Storage.Metadata.Supported {
				t.Errorf("%s should support metadata query", dbType)
			}

			relPlugin, ok := p.(plugin.RelationalDBPlugin)
			if !ok {
				t.Fatalf("%s should implement RelationalDBPlugin", dbType)
			}

			// 验证元数据查询方法
			if dbType == "postgresql" {
				if !relPlugin.IsSystemSchema("information_schema") {
					t.Error("information_schema should be system schema")
				}
				if !relPlugin.IsSystemSchema("pg_catalog") {
					t.Error("pg_catalog should be system schema")
				}
				if relPlugin.IsSystemSchema("public") {
					t.Error("public should not be system schema")
				}
			} else if dbType == "mysql" {
				if !relPlugin.IsSystemSchema("information_schema") {
					t.Error("information_schema should be system schema")
				}
				if !relPlugin.IsSystemSchema("mysql") {
					t.Error("mysql should be system schema")
				}
				if relPlugin.IsSystemSchema("test") {
					t.Error("test should not be system schema")
				}
			}

			t.Logf("%s: RelationalDBPlugin ✓", dbType)
		})
	}
}

// TestObjectStoragePlugins 验证对象存储插件
func TestObjectStoragePlugins(t *testing.T) {
	storageTypes := []string{"minio", "s3"}

	for _, storageType := range storageTypes {
		t.Run(storageType, func(t *testing.T) {
			p, err := plugin.Get(storageType)
			if err != nil {
				t.Fatalf("plugin not found: %v", err)
			}

			// 验证类别
			if p.EngineCategory() != "standard" {
				t.Errorf("expected category 'standard', got '%s'", p.EngineCategory())
			}

			capabilities := p.Capabilities()
			if capabilities.Storage == nil || capabilities.Storage.Metadata == nil || !capabilities.Storage.Metadata.Supported {
				t.Errorf("%s should support metadata query", storageType)
			}

			objPlugin, ok := p.(plugin.ObjectStoragePlugin)
			if !ok {
				t.Fatalf("%s should implement ObjectStoragePlugin", storageType)
			}

			// 验证对象存储操作方法
			testCases := []struct {
				filename    string
				expected    string
				description string
			}{
				{"test.geojson", "application/geo+json", "GeoJSON"},
				{"test.shp", "application/x-shapefile", "Shapefile"},
				{"test.kml", "application/vnd.google-earth.kml+xml", "KML"},
				{"test.json", "application/json", "JSON"},
				{"test.png", "image/png", "PNG"},
				{"test.jpg", "image/jpeg", "JPEG"},
				{"test.txt", "text/plain; charset=utf-8", "Text"},
				{"test.unknown", "application/octet-stream", "Unknown"},
			}

			for _, tc := range testCases {
				contentType := objPlugin.InferContentType(tc.filename)
				if contentType != tc.expected {
					t.Errorf("%s: expected %s, got %s", tc.description, tc.expected, contentType)
				}
			}

			// 验证其他方法
			if objPlugin.DefaultBucket() != "" && storageType != "minio" && storageType != "s3" {
				t.Logf("%s has default bucket: %s", storageType, objPlugin.DefaultBucket())
			}

			if !objPlugin.SupportsSSL() {
				t.Errorf("%s should support SSL", storageType)
			}

			t.Logf("%s: ObjectStoragePlugin ✓", storageType)
		})
	}
}

// TestPluginRegistry 验证插件注册功能
func TestPluginRegistry(t *testing.T) {
	// 验证已知插件已注册
	expectedPlugins := []string{"postgresql", "mysql", "minio", "s3"}

	for _, dbType := range expectedPlugins {
		if !plugin.Has(dbType) {
			t.Errorf("plugin %s should be registered", dbType)
		}

		p, err := plugin.Get(dbType)
		if err != nil {
			t.Errorf("failed to get plugin %s: %v", dbType, err)
		}

		if p.Type() != dbType {
			t.Errorf("plugin type mismatch: expected %s, got %s", dbType, p.Type())
		}
	}

	// 验证 List 功能
	pluginList := plugin.List()
	if len(pluginList) == 0 {
		t.Error("plugin list should not be empty")
	}

	t.Logf("Registered plugins: %v", pluginList)
}
