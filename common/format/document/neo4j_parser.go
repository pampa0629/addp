package document

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/format"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jCollectionParser Neo4j 节点标签解析器
// "database" 参数对应 Neo4j 数据库名（CE 版默认为 "neo4j"）
// "collection" 参数对应 Neo4j 节点标签（如 Person、Company）
type Neo4jCollectionParser struct{}

func init() {
	format.RegisterDocCollectionParser(&Neo4jCollectionParser{})
}

// SupportedEngineTypes 实现 DocCollectionParser 接口
func (p *Neo4jCollectionParser) SupportedEngineTypes() []string {
	return []string{"neo4j"}
}

// ParseTableInfo 通过采样节点推断属性字段类型
// 若 collection 是关系类型（节点数为 0 但关系数 > 0），自动切换为关系查询
func (p *Neo4jCollectionParser) ParseTableInfo(
	ctx context.Context,
	client interface{},
	database, collection string,
	options *format.ParseOptions,
) (*format.TableInfo, error) {
	driver, ok := client.(neo4jdriver.DriverWithContext)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected neo4j.DriverWithContext")
	}

	sampleSize := 100
	if options != nil && options.SampleSize > 0 {
		sampleSize = options.SampleSize
	}

	// 先尝试节点查询，判断是否是关系类型
	nodeCount, _ := p.countNodes(ctx, driver, database, collection)
	isRelType := false
	if nodeCount == 0 {
		relCount, _ := p.countRelationships(ctx, driver, database, collection)
		if relCount > 0 {
			isRelType = true
		}
	}

	var totalCount int64
	var cypher string
	if isRelType {
		totalCount, _ = p.countRelationships(ctx, driver, database, collection)
		cypher = fmt.Sprintf(
			"MATCH ()-[r:`%s`]->() RETURN properties(r) AS props LIMIT %d",
			escapeCypherLabel(collection), sampleSize,
		)
	} else {
		totalCount = nodeCount
		cypher = fmt.Sprintf(
			"MATCH (n:`%s`) RETURN properties(n) AS props LIMIT %d",
			escapeCypherLabel(collection), sampleSize,
		)
	}

	result, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(database),
		neo4jdriver.ExecuteQueryWithReadersRouting(),
	)
	if err != nil {
		return &format.TableInfo{
			Name:     collection,
			RowCount: &totalCount,
			Fields:   []format.FieldInfo{},
		}, nil
	}

	if len(result.Records) == 0 {
		return &format.TableInfo{
			Name:     collection,
			RowCount: &totalCount,
			Fields:   []format.FieldInfo{},
		}, nil
	}

	// 统计字段类型
	fieldStats := make(map[string]*fieldStat)
	for _, record := range result.Records {
		propsVal, _ := record.Get("props")
		props, ok := propsVal.(map[string]interface{})
		if !ok {
			continue
		}
		for key, value := range props {
			stat := ensureFieldStat(fieldStats, key)
			stat.Count++
			typeStr := detectNeo4jType(value)
			if stat.Type == "" {
				stat.Type = typeStr
			} else if stat.Type != typeStr && typeStr != "null" {
				stat.Type = "mixed"
			}
		}
	}

	fields := make([]format.FieldInfo, 0, len(fieldStats))
	for name, stat := range fieldStats {
		fields = append(fields, format.FieldInfo{
			Name:           name,
			Type:           neo4jMapToFieldType(stat.Type),
			OriginalType:   stat.Type,
			Nullable:       true,
			OccurrenceRate: float64(stat.Count) / float64(len(result.Records)),
		})
	}

	// 按出现率降序排序
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].OccurrenceRate > fields[j].OccurrenceRate
	})

	return &format.TableInfo{
		Name:     collection,
		RowCount: &totalCount,
		Fields:   fields,
	}, nil
}

// ReadPreview 分页读取节点或关系属性数据
// 若 collection 是关系类型（节点数为 0 但关系数 > 0），自动切换为关系查询
func (p *Neo4jCollectionParser) ReadPreview(
	ctx context.Context,
	client interface{},
	database, collection string,
	offset, limit int64,
	options *format.ParseOptions,
) ([]map[string]interface{}, error) {
	driver, ok := client.(neo4jdriver.DriverWithContext)
	if !ok {
		return nil, fmt.Errorf("invalid client type: expected neo4j.DriverWithContext")
	}

	// 检测是否为关系类型
	nodeCount, _ := p.countNodes(ctx, driver, database, collection)
	var cypher string
	if nodeCount == 0 {
		relCount, _ := p.countRelationships(ctx, driver, database, collection)
		if relCount > 0 {
			cypher = fmt.Sprintf(
				"MATCH ()-[r:`%s`]->() RETURN properties(r) AS props SKIP %d LIMIT %d",
				escapeCypherLabel(collection), offset, limit,
			)
		}
	}
	if cypher == "" {
		cypher = fmt.Sprintf(
			"MATCH (n:`%s`) RETURN properties(n) AS props SKIP %d LIMIT %d",
			escapeCypherLabel(collection), offset, limit,
		)
	}

	result, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(database),
		neo4jdriver.ExecuteQueryWithReadersRouting(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read Neo4j nodes: %w", err)
	}

	records := make([]map[string]interface{}, 0, len(result.Records))
	for _, record := range result.Records {
		propsVal, _ := record.Get("props")
		props, ok := propsVal.(map[string]interface{})
		if !ok {
			records = append(records, map[string]interface{}{})
			continue
		}
		row := make(map[string]interface{}, len(props))
		for k, v := range props {
			row[k] = convertNeo4jValue(v)
		}
		records = append(records, row)
	}

	return records, nil
}

// countRelationships 查询指定类型的关系总数
func (p *Neo4jCollectionParser) countRelationships(ctx context.Context, driver neo4jdriver.DriverWithContext, database, relType string) (int64, error) {
	cypher := fmt.Sprintf("MATCH ()-[r:`%s`]->() RETURN count(r) AS total", escapeCypherLabel(relType))

	result, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(database),
		neo4jdriver.ExecuteQueryWithReadersRouting(),
	)
	if err != nil || len(result.Records) == 0 {
		return 0, err
	}

	totalVal, _ := result.Records[0].Get("total")
	total, _ := totalVal.(int64)
	return total, nil
}

// countNodes 查询指定标签的节点总数
func (p *Neo4jCollectionParser) countNodes(ctx context.Context, driver neo4jdriver.DriverWithContext, database, label string) (int64, error) {
	cypher := fmt.Sprintf("MATCH (n:`%s`) RETURN count(n) AS total", escapeCypherLabel(label))

	result, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, nil,
		neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase(database),
		neo4jdriver.ExecuteQueryWithReadersRouting(),
	)
	if err != nil || len(result.Records) == 0 {
		return 0, err
	}

	totalVal, _ := result.Records[0].Get("total")
	total, _ := totalVal.(int64)
	return total, nil
}

// escapeCypherLabel 对 label 中的反引号进行转义，防止 Cypher 注入
func escapeCypherLabel(label string) string {
	result := ""
	for _, ch := range label {
		if ch == '`' {
			result += "``"
		} else {
			result += string(ch)
		}
	}
	return result
}

// detectNeo4jType 检测 Neo4j 属性值类型
func detectNeo4jType(value interface{}) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case string:
		return "string"
	case int64:
		return "int64"
	case float64:
		return "float64"
	case bool:
		return "bool"
	case time.Time:
		return "datetime"
	case neo4jdriver.Date:
		return "date"
	case neo4jdriver.LocalTime, neo4jdriver.Time:
		return "time"
	case neo4jdriver.LocalDateTime:
		return "datetime"
	case neo4jdriver.Duration:
		return "duration"
	case neo4jdriver.Point2D, neo4jdriver.Point3D:
		return "point"
	case []interface{}:
		return "array"
	default:
		return "string"
	}
}

// neo4jMapToFieldType 将 Neo4j 类型字符串映射为标准 FieldType
func neo4jMapToFieldType(t string) format.FieldType {
	switch t {
	case "string":
		return format.FieldTypeString
	case "int64":
		return format.FieldTypeInt
	case "float64":
		return format.FieldTypeDouble
	case "bool":
		return format.FieldTypeBool
	case "datetime", "date", "time":
		return format.FieldTypeTimestamp
	case "array":
		return format.FieldTypeArray
	case "mixed":
		return format.FieldTypeMixed
	default:
		return format.FieldTypeString
	}
}

// convertNeo4jValue 将 Neo4j 属性值转换为可 JSON 序列化的值
func convertNeo4jValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		return v.Format(time.RFC3339)
	case neo4jdriver.Date:
		return v.Time().Format("2006-01-02")
	case neo4jdriver.LocalDateTime:
		return v.Time().Format(time.RFC3339)
	case neo4jdriver.LocalTime:
		return v.Time().Format("15:04:05.999999999")
	case neo4jdriver.Time:
		return v.Time().Format(time.RFC3339)
	case neo4jdriver.Duration:
		return v.String()
	case neo4jdriver.Point2D:
		return map[string]interface{}{"x": v.X, "y": v.Y, "srid": v.SpatialRefId}
	case neo4jdriver.Point3D:
		return map[string]interface{}{"x": v.X, "y": v.Y, "z": v.Z, "srid": v.SpatialRefId}
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = convertNeo4jValue(item)
		}
		return result
	default:
		return v
	}
}
