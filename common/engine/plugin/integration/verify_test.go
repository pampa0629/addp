//go:build integration
// +build integration

package plugin_test

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/all"
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
			if p.EngineOrigin() == "" {
				t.Error("EngineOrigin() should not be empty")
			}

			general := p.EngineOrigin()
			t.Logf("Plugin: %s, Origin: %s", dbType, general)

			// 2. 按类别验证
			switch general {
			case "general":
				capabilities := p.Capabilities()
				if capabilities.EngineFamily == "" || capabilities.Storage == nil {
					t.Errorf("general engine should declare engine_family and storage capabilities")
					return
				}

				// 如果支持元数据，验证 provider 能力。
				if capabilities.Storage.Facts != nil && capabilities.Storage.Facts.Supported {
					if capabilities.Storage.Catalog != nil && capabilities.Storage.Catalog.Supported {
						if _, ok := p.(plugin.CatalogProvider); !ok {
							t.Errorf("%s declares catalog but doesn't implement CatalogProvider", dbType)
						}
						if _, ok := p.(plugin.CatalogModelProvider); !ok {
							t.Errorf("%s declares catalog but doesn't implement CatalogModelProvider", dbType)
						}
					}
					if _, ok := p.(plugin.CatalogFactsProvider); !ok {
						t.Errorf("%s declares catalog facts but doesn't implement CatalogFactsProvider", dbType)
					}
					if capabilities.Storage.Store != nil && capabilities.Storage.Store.StreamRead {
						if _, ok := p.(plugin.ContentReadableProvider); !ok {
							t.Errorf("%s declares stream read but doesn't implement ContentReadableProvider", dbType)
						}
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
				t.Errorf("unknown EngineOrigin: %s", general)
			}
		})
	}
}

// TestTabularDBPlugins 验证表格型数据库插件 provider 能力
func TestTabularDBPlugins(t *testing.T) {
	dbTypes := []string{"postgresql", "mysql"}

	for _, dbType := range dbTypes {
		t.Run(dbType, func(t *testing.T) {
			p, err := plugin.Get(dbType)
			if err != nil {
				t.Fatalf("plugin not found: %v", err)
			}

			// 验证类别
			if p.EngineOrigin() != "general" {
				t.Errorf("expected engine origin general, got '%s'", p.EngineOrigin())
			}

			capabilities := p.Capabilities()
			if capabilities.Storage == nil || capabilities.Storage.Facts == nil || !capabilities.Storage.Facts.Supported {
				t.Errorf("%s should support catalog facts query", dbType)
			}

			if _, ok := p.(plugin.CatalogProvider); !ok {
				t.Fatalf("%s should implement CatalogProvider", dbType)
			}
			if _, ok := p.(plugin.CatalogFactsProvider); !ok {
				t.Fatalf("%s should implement CatalogFactsProvider", dbType)
			}
			if _, ok := p.(plugin.SQLQueryRuntimeProvider); !ok {
				t.Fatalf("%s should implement SQLQueryRuntimeProvider", dbType)
			}

			if _, ok := p.(plugin.ConnectionPoolPlugin); !ok {
				t.Fatalf("%s should implement ConnectionPoolPlugin", dbType)
			}

			if capabilities.Storage.Catalog == nil || !capabilities.Storage.Catalog.SystemFiltering {
				t.Fatalf("%s should declare catalog system filtering", dbType)
			}

			t.Logf("%s: tabular providers ✓", dbType)
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
			if p.EngineOrigin() != "general" {
				t.Errorf("expected engine origin general, got '%s'", p.EngineOrigin())
			}

			capabilities := p.Capabilities()
			if capabilities.Storage == nil || capabilities.Storage.Facts == nil || !capabilities.Storage.Facts.Supported {
				t.Errorf("%s should support catalog facts query", storageType)
			}

			catalogProvider, ok := p.(plugin.CatalogProvider)
			if !ok {
				t.Fatalf("%s should implement CatalogProvider", storageType)
			}
			_ = catalogProvider

			contentReader, ok := p.(plugin.ContentReadableProvider)
			if !ok {
				t.Fatalf("%s should implement ContentReadableProvider", storageType)
			}
			_ = contentReader

			t.Logf("%s: object storage providers ✓", storageType)
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
