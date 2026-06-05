package geojsonformat

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// GeoJSONFeature 表示 GeoJSON Feature。
type GeoJSONFeature struct {
	Type       string                 `json:"type"`
	ID         interface{}            `json:"id,omitempty"`
	Geometry   json.RawMessage        `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

// GeoJSONFeatureCollection 表示 GeoJSON FeatureCollection，并保留平台计数字段。
type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []GeoJSONFeature `json:"features"`
	Count    int              `json:"count"`
}

// RowsToGeoJSON 将数据库行转换为 GeoJSON FeatureCollection。
func RowsToGeoJSON(rows *sql.Rows, columns []string) (*GeoJSONFeatureCollection, error) {
	fc := &GeoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []GeoJSONFeature{},
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		feature := GeoJSONFeature{
			Type:       "Feature",
			Properties: make(map[string]interface{}),
		}

		for i, col := range columns {
			val := values[i]
			if val == nil {
				continue
			}

			var strVal string
			if bval, ok := val.([]byte); ok {
				strVal = string(bval)
			} else if sval, ok := val.(string); ok {
				strVal = sval
			}

			switch col {
			case "id":
				feature.ID = val
			case "geometry":
				if strVal != "" {
					feature.Geometry = json.RawMessage(strVal)
				}
			default:
				feature.Properties[col] = val
			}
		}

		fc.Features = append(fc.Features, feature)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	fc.Count = len(fc.Features)
	return fc, nil
}

// ToJSON 将 GeoJSON FeatureCollection 转换为 JSON 字符串。
func (fc *GeoJSONFeatureCollection) ToJSON() (string, error) {
	data, err := json.Marshal(fc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal GeoJSON: %w", err)
	}
	return string(data), nil
}
