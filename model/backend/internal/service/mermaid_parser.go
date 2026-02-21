package service

import (
	"strings"
)

// MermaidERParser Mermaid ER图解析器
type MermaidERParser struct {
	Entities  []EntityDefinition
	Relations []RelationDefinition
}

// EntityDefinition 实体定义
type EntityDefinition struct {
	Name       string
	Attributes []AttributeDefinition
}

// AttributeDefinition 属性定义
type AttributeDefinition struct {
	Type string
	Name string
	IsPK bool
	IsFK bool
}

// RelationDefinition 关系定义
type RelationDefinition struct {
	Source string
	Target string
	Symbol string
	Label  string
}

// ParseMermaidER 解析Mermaid ER图代码
func ParseMermaidER(code string) (*MermaidERParser, error) {
	parser := &MermaidERParser{
		Entities:  []EntityDefinition{},
		Relations: []RelationDefinition{},
	}

	lines := strings.Split(code, "\n")
	var currentEntity *EntityDefinition

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// 跳过erDiagram标记
		if line == "erDiagram" {
			continue
		}

		// 解析实体定义开始: CUSTOMER {
		if strings.HasSuffix(line, "{") && !strings.Contains(line, "||") {
			entityName := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			currentEntity = &EntityDefinition{
				Name:       entityName,
				Attributes: []AttributeDefinition{},
			}
			continue
		}

		// 解析实体定义结束: }
		if line == "}" && currentEntity != nil {
			parser.Entities = append(parser.Entities, *currentEntity)
			currentEntity = nil
			continue
		}

		// 解析属性: string customer_id PK FK
		if currentEntity != nil {
			attr := parseAttribute(line)
			if attr != nil {
				currentEntity.Attributes = append(currentEntity.Attributes, *attr)
			}
			continue
		}

		// 解析关系: CUSTOMER ||--o{ ORDER : "places"
		if strings.Contains(line, "||") || strings.Contains(line, "}o") {
			relation := parseRelation(line)
			if relation != nil {
				parser.Relations = append(parser.Relations, *relation)
			}
		}
	}

	return parser, nil
}

// parseAttribute 解析属性行
func parseAttribute(line string) *AttributeDefinition {
	// 正则匹配：type name [PK] [FK]
	// 示例: string customer_id PK FK
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}

	attr := &AttributeDefinition{
		Type: parts[0],
		Name: parts[1],
		IsPK: false,
		IsFK: false,
	}

	// 检查是否有PK/FK标记
	for i := 2; i < len(parts); i++ {
		if parts[i] == "PK" {
			attr.IsPK = true
		}
		if parts[i] == "FK" {
			attr.IsFK = true
		}
	}

	return attr
}

// parseRelation 解析关系行
func parseRelation(line string) *RelationDefinition {
	// 正则匹配关系定义
	// CUSTOMER ||--o{ ORDER : "places"
	// 或 CUSTOMER ||--o{ ORDER : places

	// 分割冒号获取标签
	parts := strings.Split(line, ":")
	if len(parts) < 1 {
		return nil
	}

	// 解析关系部分（冒号前）
	relationPart := strings.TrimSpace(parts[0])
	fields := strings.Fields(relationPart)
	if len(fields) < 3 {
		return nil
	}

	relation := &RelationDefinition{
		Source: fields[0],
		Symbol: fields[1],
		Target: fields[2],
		Label:  "",
	}

	// 解析标签（冒号后）
	if len(parts) > 1 {
		label := strings.TrimSpace(parts[1])
		label = strings.Trim(label, "\"")
		relation.Label = label
	}

	return relation
}

// ConvertRelationType 转换Mermaid符号为数据库关系类型
func ConvertRelationType(symbol string) string {
	switch symbol {
	case "||--||":
		return "one_to_one"
	case "||--o{", "||..o{":
		return "one_to_many"
	case "}o--o{", "}o..o{":
		return "many_to_many"
	default:
		return "one_to_many"
	}
}

// ConvertToMermaidSymbol 转换数据库关系类型为Mermaid符号
func ConvertToMermaidSymbol(relationType string) string {
	switch relationType {
	case "one_to_one":
		return "||--||"
	case "one_to_many":
		return "||--o{"
	case "many_to_many":
		return "}o--o{"
	default:
		return "||--o{"
	}
}
