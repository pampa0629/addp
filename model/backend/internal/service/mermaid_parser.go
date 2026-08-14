package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var mermaidIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var mermaidDataTypes = map[string]struct{}{
	"string": {}, "int": {}, "bigint": {}, "float": {}, "decimal": {},
	"date": {}, "datetime": {}, "bool": {}, "json": {}, "text": {}, "geometry": {},
}

// MermaidERParser Mermaid ER图解析器
type MermaidERParser struct {
	Entities  []EntityDefinition
	Relations []RelationDefinition
}

// EntityDefinition 实体定义
type EntityDefinition struct {
	Name        string
	DisplayName string
	DomainID    *int64
	Description string
	Attributes  []AttributeDefinition
}

// AttributeDefinition 属性定义
type AttributeDefinition struct {
	Type        string
	Name        string
	DisplayName string
	IsPK        bool
	IsFK        bool
	Nullable    bool
	ElementID   *int64
	Description string
	SortOrder   int
}

type mermaidEntityMetadata struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	DomainID    *int64 `json:"domain_id"`
	Description string `json:"description"`
}
type mermaidAttributeMetadata struct {
	Entity      string `json:"entity"`
	Column      string `json:"column"`
	Name        string `json:"name"`
	Nullable    bool   `json:"nullable"`
	ElementID   *int64 `json:"element_id"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

type mermaidRelationMetadata struct {
	Source       string `json:"source"`
	Target       string `json:"target"`
	RelationType string `json:"relation_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
}

// RelationDefinition 关系定义
type RelationDefinition struct {
	Source      string
	Target      string
	Symbol      string
	Label       string
	Description string
}

// ParseMermaidER 解析Mermaid ER图代码
func ParseMermaidER(code string) (*MermaidERParser, error) {
	parser := &MermaidERParser{
		Entities:  []EntityDefinition{},
		Relations: []RelationDefinition{},
	}

	lines := strings.Split(code, "\n")
	var currentEntity *EntityDefinition
	entityMetadata := map[string]mermaidEntityMetadata{}
	attributeMetadata := map[string]mermaidAttributeMetadata{}
	var relationMetadata *mermaidRelationMetadata

	headerSeen := false
	for lineNumber, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "%% addp:entity ") {
			var metadata mermaidEntityMetadata
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "%% addp:entity ")), &metadata); err != nil || metadata.Code == "" || metadata.Name == "" {
				return nil, fmt.Errorf("第 %d 行 ADDP 实体元数据无效", lineNumber+1)
			}
			if _, exists := entityMetadata[metadata.Code]; exists {
				return nil, fmt.Errorf("第 %d 行 ADDP 实体元数据重复", lineNumber+1)
			}
			entityMetadata[metadata.Code] = metadata
			continue
		}
		if strings.HasPrefix(line, "%% addp:attribute ") {
			var metadata mermaidAttributeMetadata
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "%% addp:attribute ")), &metadata); err != nil || metadata.Entity == "" || metadata.Column == "" || metadata.Name == "" || metadata.SortOrder < 0 {
				return nil, fmt.Errorf("第 %d 行 ADDP 属性元数据无效", lineNumber+1)
			}
			key := metadata.Entity + "." + metadata.Column
			if _, exists := attributeMetadata[key]; exists {
				return nil, fmt.Errorf("第 %d 行 ADDP 属性元数据重复", lineNumber+1)
			}
			attributeMetadata[key] = metadata
			continue
		}
		if strings.HasPrefix(line, "%% addp:relation ") {
			if relationMetadata != nil {
				return nil, fmt.Errorf("第 %d 行前一条 ADDP 关系元数据未被关系定义消费", lineNumber+1)
			}
			var metadata mermaidRelationMetadata
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "%% addp:relation ")), &metadata); err != nil ||
				metadata.Source == "" || metadata.Target == "" || !validMermaidRelationType(metadata.RelationType) {
				return nil, fmt.Errorf("第 %d 行 ADDP 关系元数据无效", lineNumber+1)
			}
			relationMetadata = &metadata
			continue
		}
		// 跳过空行和普通 Mermaid 注释
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}

		// 跳过erDiagram标记
		if line == "erDiagram" {
			if headerSeen {
				return nil, fmt.Errorf("第 %d 行重复声明 erDiagram", lineNumber+1)
			}
			headerSeen = true
			continue
		}
		if !headerSeen {
			return nil, fmt.Errorf("第 %d 行必须位于 erDiagram 声明之后", lineNumber+1)
		}

		// 解析实体定义开始: CUSTOMER {
		if strings.HasSuffix(line, "{") && !strings.Contains(line, "||") {
			if currentEntity != nil {
				return nil, fmt.Errorf("第 %d 行出现嵌套实体定义", lineNumber+1)
			}
			entityName := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			if !mermaidIdentifierPattern.MatchString(entityName) {
				return nil, fmt.Errorf("第 %d 行实体标识符无效: %s", lineNumber+1, entityName)
			}
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
			if attr == nil {
				return nil, fmt.Errorf("第 %d 行属性定义无效", lineNumber+1)
			}
			if _, ok := mermaidDataTypes[attr.Type]; !ok {
				return nil, fmt.Errorf("第 %d 行属性类型不受支持: %s", lineNumber+1, attr.Type)
			}
			if !mermaidIdentifierPattern.MatchString(attr.Name) {
				return nil, fmt.Errorf("第 %d 行属性标识符无效: %s", lineNumber+1, attr.Name)
			}
			if attr.IsFK {
				return nil, fmt.Errorf("第 %d 行不支持 FK 标记，请使用显式关系定义", lineNumber+1)
			}
			currentEntity.Attributes = append(currentEntity.Attributes, *attr)
			continue
		}

		// 解析关系: CUSTOMER ||--o{ ORDER : "places"
		relation := parseRelation(line)
		if relation == nil {
			return nil, fmt.Errorf("第 %d 行关系定义无效", lineNumber+1)
		}
		if ConvertRelationType(relation.Symbol) == "" {
			return nil, fmt.Errorf("第 %d 行关系符号不受支持: %s", lineNumber+1, relation.Symbol)
		}
		if relationMetadata != nil {
			if relationMetadata.Source != relation.Source || relationMetadata.Target != relation.Target ||
				relationMetadata.RelationType != ConvertRelationType(relation.Symbol) || relationMetadata.Name != relation.Label {
				return nil, fmt.Errorf("第 %d 行关系定义与 ADDP 元数据不一致", lineNumber+1)
			}
			relation.Description = relationMetadata.Description
			relationMetadata = nil
		}
		parser.Relations = append(parser.Relations, *relation)
	}
	if !headerSeen {
		return nil, fmt.Errorf("缺少 erDiagram 声明")
	}
	if currentEntity != nil {
		return nil, fmt.Errorf("实体 %s 缺少结束括号", currentEntity.Name)
	}
	if relationMetadata != nil {
		return nil, fmt.Errorf("ADDP 关系元数据缺少对应关系定义")
	}
	if err := validateMermaidER(parser); err != nil {
		return nil, err
	}
	for entityIndex := range parser.Entities {
		entity := &parser.Entities[entityIndex]
		entity.DisplayName = entity.Name
		if metadata, ok := entityMetadata[entity.Name]; ok && metadata.Name != "" {
			entity.DisplayName = metadata.Name
			entity.DomainID = metadata.DomainID
			entity.Description = metadata.Description
			delete(entityMetadata, entity.Name)
		}
		for attributeIndex := range entity.Attributes {
			attribute := &entity.Attributes[attributeIndex]
			attribute.DisplayName = attribute.Name
			attribute.Nullable = !attribute.IsPK
			if metadata, ok := attributeMetadata[entity.Name+"."+attribute.Name]; ok {
				if metadata.Name != "" {
					attribute.DisplayName = metadata.Name
				}
				attribute.Nullable = metadata.Nullable
				attribute.ElementID = metadata.ElementID
				attribute.Description = metadata.Description
				attribute.SortOrder = metadata.SortOrder
				delete(attributeMetadata, entity.Name+"."+attribute.Name)
			}
		}
	}
	if len(entityMetadata) > 0 || len(attributeMetadata) > 0 {
		return nil, fmt.Errorf("ADDP 元数据引用了不存在的实体或属性")
	}
	if err := validateMermaidStorageLengths(parser); err != nil {
		return nil, err
	}
	return parser, nil
}

func validateMermaidStorageLengths(parser *MermaidERParser) error {
	for _, entity := range parser.Entities {
		if utf8.RuneCountInString(entity.Name) > 100 || utf8.RuneCountInString(entity.DisplayName) > 200 {
			return fmt.Errorf("实体名称超过存储长度限制: %s", entity.Name)
		}
		for _, attribute := range entity.Attributes {
			if utf8.RuneCountInString(attribute.Name) > 200 || utf8.RuneCountInString(attribute.DisplayName) > 200 {
				return fmt.Errorf("实体 %s 的属性名称超过存储长度限制: %s", entity.Name, attribute.Name)
			}
		}
	}
	for _, relation := range parser.Relations {
		if utf8.RuneCountInString(relation.Label) > 200 {
			return fmt.Errorf("关系名称超过存储长度限制: %s", relation.Label)
		}
	}
	return nil
}

func validateMermaidER(parser *MermaidERParser) error {
	entities := make(map[string]struct{}, len(parser.Entities))
	for _, entity := range parser.Entities {
		if _, exists := entities[entity.Name]; exists {
			return fmt.Errorf("实体重复: %s", entity.Name)
		}
		entities[entity.Name] = struct{}{}
		attributes := make(map[string]struct{}, len(entity.Attributes))
		for _, attribute := range entity.Attributes {
			if _, exists := attributes[attribute.Name]; exists {
				return fmt.Errorf("实体 %s 的属性重复: %s", entity.Name, attribute.Name)
			}
			attributes[attribute.Name] = struct{}{}
		}
	}
	for _, relation := range parser.Relations {
		if _, exists := entities[relation.Source]; !exists {
			return fmt.Errorf("关系引用不存在的源实体: %s", relation.Source)
		}
		if _, exists := entities[relation.Target]; !exists {
			return fmt.Errorf("关系引用不存在的目标实体: %s", relation.Target)
		}
		if relation.Source == relation.Target {
			return fmt.Errorf("关系不能连接同一实体: %s", relation.Source)
		}
	}
	return nil
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
	parts := strings.SplitN(line, ":", 2)
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
		if strings.HasPrefix(label, "\"") {
			unquoted, err := strconv.Unquote(label)
			if err != nil {
				return nil
			}
			label = unquoted
		}
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
		return ""
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
		return ""
	}
}

func validMermaidRelationType(relationType string) bool {
	return ConvertToMermaidSymbol(relationType) != ""
}
