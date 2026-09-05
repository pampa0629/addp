package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	commonQuery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var relationParameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type relationParameterBinding struct {
	Name           string
	DefaultLocator string
}

type relationReplacement struct {
	start       int
	end         int
	replacement string
}

type relationInspector struct {
	query        string
	bindings     map[string]relationParameterBinding
	replacements map[string]string
	used         map[string]struct{}
	patches      []relationReplacement
}

func relationParameterBindings(definitions []models.QueryParameterDefinition) ([]relationParameterBinding, bool, error) {
	bindings := make([]relationParameterBinding, 0)
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.Type != "relation" {
			continue
		}
		if !relationParameterNamePattern.MatchString(definition.Name) {
			return nil, true, fmt.Errorf("relation 查询参数名称必须是小写 SQL 标识符: %s", definition.Name)
		}
		if _, duplicate := seen[definition.Name]; duplicate {
			return nil, true, fmt.Errorf("relation 查询参数名称重复: %s", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		defaultLocator := ""
		if definition.Default != nil {
			normalized, err := normalizeRelationParameterValue(definition.Default)
			if err != nil {
				return nil, true, fmt.Errorf("relation 查询参数 %s 的默认资源无效: %w", definition.Name, err)
			}
			defaultLocator, _ = normalized["locator"].(string)
		}
		bindings = append(bindings, relationParameterBinding{Name: definition.Name, DefaultLocator: defaultLocator})
	}
	return bindings, len(bindings) > 0, nil
}

func relationParameterBindingsFromContent(content map[string]interface{}) ([]relationParameterBinding, bool, error) {
	definitions, err := queryParameterDefinitions(content)
	if err != nil {
		return nil, false, err
	}
	return relationParameterBindings(definitions)
}

func validateRelationResultSource(query string, bindings []relationParameterBinding) error {
	_, err := compileRelationParameters(query, bindings, nil)
	return err
}

func compileExistingTableResultQuery(
	task *models.DevTask,
	relationLocators map[string]string,
	targetLocator string,
	engineType string,
) (*models.DevTask, error) {
	if task == nil {
		return nil, fmt.Errorf("existing-table result task is incomplete")
	}
	queryText, _ := task.Content["query"].(string)
	analysis, err := AnalyzeQuery("sql", queryText)
	if err != nil || analysis.Statement != "SELECT" || analysis.Effect != string(SQLExecutionEffectRead) {
		return nil, fmt.Errorf("existing-table result source must be one read-only SELECT")
	}
	definitions, err := queryParameterDefinitions(task.Content)
	if err != nil {
		return nil, err
	}
	bindings, _, err := relationParameterBindings(definitions)
	if err != nil {
		return nil, err
	}
	dialect := commonQuery.ForDialect(engineType)
	if !dialect.IsPostgreSQL() {
		return nil, fmt.Errorf("relation 查询参数写入仅支持 PostgreSQL")
	}
	inputTables, engineID, err := relationParameterTables(bindings, relationLocators, dialect)
	if err != nil {
		return nil, err
	}
	compiledSource, err := compileRelationParameters(queryText, bindings, inputTables)
	if err != nil {
		return nil, err
	}
	locator, err := resourcetree.ParseURI(strings.TrimSpace(targetLocator))
	if err != nil || locator.EngineID != engineID || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
		return nil, fmt.Errorf("target_locator 必须是与 relation 查询参数同引擎的 PostgreSQL 表")
	}
	content := models.DevTaskContent{}
	for key, value := range task.Content {
		content[key] = value
	}
	content["query"] = "INSERT INTO " + dialect.QualifiedTable(locator.Path[0], locator.Path[1]) + " " + strings.TrimSpace(compiledSource)
	compiled := *task
	compiled.Content = content
	return &compiled, nil
}

func compileRelationPreviewQuery(
	task *models.DevTask,
	effectiveInputs map[string]interface{},
	engineType string,
) (*models.DevTask, error) {
	if task == nil {
		return nil, fmt.Errorf("relation 查询预览任务不完整")
	}
	queryText, _ := task.Content["query"].(string)
	definitions, err := queryParameterDefinitions(task.Content)
	if err != nil {
		return nil, err
	}
	bindings, hasRelationParameters, err := relationParameterBindings(definitions)
	if err != nil || !hasRelationParameters {
		return task, err
	}
	dialect := commonQuery.ForDialect(engineType)
	if !dialect.IsPostgreSQL() {
		return nil, fmt.Errorf("relation 查询参数预览仅支持 PostgreSQL")
	}
	relationLocators, err := relationLocatorsFromInputs(bindings, effectiveInputs)
	if err != nil {
		return nil, err
	}
	inputTables, engineID, err := relationParameterTables(bindings, relationLocators, dialect)
	if err != nil {
		return nil, err
	}
	taskEngineID := task.GetEngineID()
	if taskEngineID == nil || *taskEngineID != engineID {
		return nil, fmt.Errorf("所有 relation 查询参数必须与查询 Runtime 位于同一引擎")
	}
	compiledSource, err := compileRelationParameters(queryText, bindings, inputTables)
	if err != nil {
		return nil, err
	}
	content := models.DevTaskContent{}
	for key, value := range task.Content {
		content[key] = value
	}
	content["query"] = strings.TrimSpace(compiledSource)
	compiled := *task
	compiled.Content = content
	return &compiled, nil
}

func relationLocatorsFromInputs(
	bindings []relationParameterBinding,
	effectiveInputs map[string]interface{},
) (map[string]string, error) {
	relationLocators := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		value, exists := effectiveInputs[binding.Name]
		if !exists {
			return nil, fmt.Errorf("查询参数 %s 缺少数据表绑定", binding.Name)
		}
		normalized, err := normalizeRelationParameterValue(value)
		if err != nil {
			return nil, fmt.Errorf("查询参数 %s 无效: %w", binding.Name, err)
		}
		relationLocators[binding.Name], _ = normalized["locator"].(string)
	}
	return relationLocators, nil
}

func relationParameterTables(
	bindings []relationParameterBinding,
	relationLocators map[string]string,
	dialect commonQuery.Dialect,
) (map[string]string, uint, error) {
	if len(bindings) == 0 || len(relationLocators) != len(bindings) {
		return nil, 0, fmt.Errorf("所有 relation 查询参数都必须精确绑定 ResourceLocator")
	}
	tables := make(map[string]string, len(bindings))
	var engineID uint
	for _, binding := range bindings {
		locatorText, exists := relationLocators[binding.Name]
		locator, err := resourcetree.ParseURI(strings.TrimSpace(locatorText))
		if !exists || err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
			return nil, 0, fmt.Errorf("查询参数 %s 必须绑定 PostgreSQL 表 ResourceLocator", binding.Name)
		}
		if engineID == 0 {
			engineID = locator.EngineID
		} else if engineID != locator.EngineID {
			return nil, 0, fmt.Errorf("所有 relation 查询参数和目标必须位于同一引擎")
		}
		tables[binding.Name] = dialect.QualifiedTable(locator.Path[0], locator.Path[1])
	}
	return tables, engineID, nil
}

func compileRelationParameters(
	query string,
	bindings []relationParameterBinding,
	replacements map[string]string,
) (string, error) {
	bindingMap := make(map[string]relationParameterBinding, len(bindings))
	for _, binding := range bindings {
		bindingMap[binding.Name] = binding
	}
	masked, err := maskPostgreSQLNamedParameters(query)
	if err != nil {
		return "", err
	}
	parsed, err := pgquery.Parse(masked)
	if err != nil || len(parsed.GetStmts()) != 1 || parsed.GetStmts()[0].GetStmt().GetSelectStmt() == nil {
		return "", fmt.Errorf("relation 查询参数查询必须是 PostgreSQL 单条 SELECT")
	}
	inspector := &relationInspector{
		query: query, bindings: bindingMap, replacements: replacements,
		used: make(map[string]struct{}, len(bindings)),
	}
	if err := inspector.inspectNode(parsed.Stmts[0].Stmt, nil); err != nil {
		return "", err
	}
	for _, binding := range bindings {
		if _, used := inspector.used[binding.Name]; !used {
			return "", fmt.Errorf("relation 查询参数未被 SQL 引用: %s", binding.Name)
		}
	}
	if replacements == nil {
		return query, nil
	}
	sort.Slice(inspector.patches, func(left, right int) bool {
		return inspector.patches[left].start > inspector.patches[right].start
	})
	compiled := query
	for _, patch := range inspector.patches {
		compiled = compiled[:patch.start] + patch.replacement + compiled[patch.end:]
	}
	return compiled, nil
}

func (i *relationInspector) inspectNode(node *pgquery.Node, scope map[string]struct{}) error {
	if node == nil {
		return nil
	}
	if selectStmt := node.GetSelectStmt(); selectStmt != nil {
		return i.inspectSelect(selectStmt, scope)
	}
	if relation := node.GetRangeVar(); relation != nil {
		return i.inspectRangeVar(relation, scope)
	}
	if node.GetInsertStmt() != nil || node.GetUpdateStmt() != nil || node.GetDeleteStmt() != nil || node.GetMergeStmt() != nil {
		return fmt.Errorf("relation 查询参数查询不允许数据修改子语句")
	}
	if node.GetRangeFunction() != nil || node.GetRangeTableFunc() != nil || node.GetJsonTable() != nil {
		return fmt.Errorf("relation 查询参数查询不允许表函数数据源")
	}
	return i.inspectMessage(node.ProtoReflect(), scope, "")
}

func (i *relationInspector) inspectSelect(stmt *pgquery.SelectStmt, inherited map[string]struct{}) error {
	if stmt.GetIntoClause() != nil || len(stmt.GetLockingClause()) != 0 {
		return fmt.Errorf("relation 查询参数查询不允许 SELECT INTO 或行锁")
	}
	scope := cloneStringSet(inherited)
	withClause := stmt.GetWithClause()
	if withClause != nil && withClause.GetRecursive() {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || strings.TrimSpace(cte.GetCtename()) == "" {
				return fmt.Errorf("relation 查询参数查询包含无效 CTE")
			}
			if _, conflict := i.bindings[cte.GetCtename()]; conflict {
				return fmt.Errorf("CTE 名称与 relation 查询参数重名: %s", cte.GetCtename())
			}
			scope[cte.GetCtename()] = struct{}{}
		}
	}
	if withClause != nil {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || cte.GetCtequery() == nil {
				return fmt.Errorf("relation 查询参数查询包含无效 CTE")
			}
			if _, conflict := i.bindings[cte.GetCtename()]; conflict {
				return fmt.Errorf("CTE 名称与 relation 查询参数重名: %s", cte.GetCtename())
			}
			if err := i.inspectNode(cte.GetCtequery(), scope); err != nil {
				return err
			}
			if !withClause.GetRecursive() {
				scope[cte.GetCtename()] = struct{}{}
			}
		}
	}
	return i.inspectMessage(stmt.ProtoReflect(), scope, "with_clause")
}

func (i *relationInspector) inspectRangeVar(relation *pgquery.RangeVar, scope map[string]struct{}) error {
	if relation.GetCatalogname() != "" {
		return fmt.Errorf("relation 查询参数查询不允许跨数据库关系")
	}
	if relation.GetSchemaname() != "" {
		return fmt.Errorf("relation 查询参数查询不允许 schema 限定关系: %s.%s", relation.GetSchemaname(), relation.GetRelname())
	}
	if _, cte := scope[relation.GetRelname()]; cte {
		if _, conflict := i.bindings[relation.GetRelname()]; conflict {
			return fmt.Errorf("CTE 名称与 relation 查询参数重名: %s", relation.GetRelname())
		}
		return nil
	}
	binding, exists := i.bindings[relation.GetRelname()]
	if !exists {
		return fmt.Errorf("relation 查询参数查询只能读取已声明参数，禁止物理关系: %s", relation.GetRelname())
	}
	i.used[binding.Name] = struct{}{}
	start, end, err := relationSpan(i.query, int(relation.GetLocation()), binding.Name)
	if err != nil {
		return err
	}
	if i.replacements == nil {
		return nil
	}
	replacement, exists := i.replacements[binding.Name]
	if !exists || strings.TrimSpace(replacement) == "" {
		return fmt.Errorf("relation 查询参数缺少执行期 ResourceLocator: %s", binding.Name)
	}
	i.patches = append(i.patches, relationReplacement{start: start, end: end, replacement: replacement})
	return nil
}

func (i *relationInspector) inspectMessage(message protoreflect.Message, scope map[string]struct{}, skipField string) error {
	if !message.IsValid() {
		return nil
	}
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if string(field.Name()) == skipField || !message.Has(field) || field.Kind() != protoreflect.MessageKind {
			continue
		}
		value := message.Get(field)
		if field.IsList() {
			list := value.List()
			for itemIndex := 0; itemIndex < list.Len(); itemIndex++ {
				if err := i.inspectReflectedMessage(list.Get(itemIndex).Message(), scope); err != nil {
					return err
				}
			}
			continue
		}
		if err := i.inspectReflectedMessage(value.Message(), scope); err != nil {
			return err
		}
	}
	return nil
}

func (i *relationInspector) inspectReflectedMessage(message protoreflect.Message, scope map[string]struct{}) error {
	switch typed := message.Interface().(type) {
	case *pgquery.Node:
		return i.inspectNode(typed, scope)
	case *pgquery.SelectStmt:
		return i.inspectSelect(typed, scope)
	default:
		return i.inspectMessage(message, scope, "")
	}
}

func relationSpan(query string, start int, bindingName string) (int, int, error) {
	if start < 0 || start >= len(query) || !strings.HasPrefix(query[start:], bindingName) {
		return 0, 0, fmt.Errorf("relation 查询参数必须使用未加引号的裸参数名: %s", bindingName)
	}
	end := start + len(bindingName)
	if end < len(query) && (query[end] == '_' || query[end] == '$' || query[end] >= '0' && query[end] <= '9' || query[end] >= 'a' && query[end] <= 'z' || query[end] >= 'A' && query[end] <= 'Z') {
		return 0, 0, fmt.Errorf("relation 查询参数标识符边界无效")
	}
	return start, end, nil
}

func cloneStringSet(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source)+4)
	for value := range source {
		cloned[value] = struct{}{}
	}
	return cloned
}
