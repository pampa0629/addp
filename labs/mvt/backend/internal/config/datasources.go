package config

import (
	"fmt"
	"os"

	"github.com/addp/mvt/internal/models"
	"gopkg.in/yaml.v3"
)

// LoadDataSources 加载数据源配置
func LoadDataSources(configPath string) ([]models.DataSource, error) {
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read datasources config: %w", err)
	}

	// 解析 YAML
	var config models.DataSourcesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse datasources config: %w", err)
	}

	// 验证数据源配置
	for i, ds := range config.DataSources {
		if ds.ID == "" {
			return nil, fmt.Errorf("datasource at index %d has no ID", i)
		}
		if ds.Type != "postgis" {
			return nil, fmt.Errorf("datasource %s has unsupported type: %s", ds.ID, ds.Type)
		}
		if ds.Connection.Table == "" {
			return nil, fmt.Errorf("datasource %s has no table name", ds.ID)
		}
		if ds.Connection.GeometryColumn == "" {
			return nil, fmt.Errorf("datasource %s has no geometry column", ds.ID)
		}
	}

	return config.DataSources, nil
}
