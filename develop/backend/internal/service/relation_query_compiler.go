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

const relationInputSchema = "addp_input"

var relationInputNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type relationInputBinding struct {
	Name string
}

type relationReplacement struct {
	start       int
	end         int
	replacement string
}

type relationInspector struct {
	query        string
	bindings     map[string]relationInputBinding
	replacements map[string]string
	used         map[string]struct{}
	patches      []relationReplacement
}

func relationInputBindings(content map[string]interface{}) ([]relationInputBinding, bool, error) {
	if content == nil {
		return nil, false, nil
	}
	raw, exists := content["relation_inputs"]
	if !exists {
		return nil, false, nil
	}
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 {
		return nil, true, fmt.Errorf("content.relation_inputs 必须是非空数组")
	}
	bindings := make([]relationInputBinding, 0, len(items))
	seenNames := make(map[string]struct{}, len(items))
	for _, rawItem := range items {
		name, ok := rawItem.(string)
		name = strings.TrimSpace(name)
		if !ok || !relationInputNamePattern.MatchString(name) {
			return nil, true, fmt.Errorf("content.relation_inputs 每项必须是小写 SQL 标识符")
		}
		if _, duplicate := seenNames[name]; duplicate {
			return nil, true, fmt.Errorf("content.relation_inputs 不得重复: %s", name)
		}
		seenNames[name] = struct{}{}
		bindings = append(bindings, relationInputBinding{Name: name})
	}
	return bindings, true, nil
}

func validateRelationResultSource(query string, bindings []relationInputBinding) error {
	_, err := compileRelationInputs(query, bindings, nil)
	return err
}

func compileExistingTableResultQuery(
	task *models.DevTask,
	inputLocators map[string]string,
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
	bindings, _, err := relationInputBindings(task.Content)
	if err != nil {
		return nil, err
	}
	dialect := commonQuery.ForEngine(engineType)
	if !dialect.IsPostgreSQL() {
		return nil, fmt.Errorf("关系输入写入仅支持 PostgreSQL")
	}
	inputTables, engineID, err := relationInputTables(bindings, inputLocators, dialect)
	if err != nil {
		return nil, err
	}
	compiledSource, err := compileRelationInputs(queryText, bindings, inputTables)
	if err != nil {
		return nil, err
	}
	locator, err := resourcetree.ParseURI(strings.TrimSpace(targetLocator))
	if err != nil || locator.EngineID != engineID || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
		return nil, fmt.Errorf("target_locator 必须是与关系输入同引擎的 PostgreSQL 表")
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

func relationInputTables(
	bindings []relationInputBinding,
	inputLocators map[string]string,
	dialect commonQuery.Dialect,
) (map[string]string, uint, error) {
	if len(bindings) == 0 || len(inputLocators) != len(bindings) {
		return nil, 0, fmt.Errorf("input_locators 必须精确绑定全部 relation_inputs")
	}
	tables := make(map[string]string, len(bindings))
	var engineID uint
	for _, binding := range bindings {
		locatorText, exists := inputLocators[binding.Name]
		locator, err := resourcetree.ParseURI(strings.TrimSpace(locatorText))
		if !exists || err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
			return nil, 0, fmt.Errorf("input_locators.%s 必须是 PostgreSQL 表 locator", binding.Name)
		}
		if engineID == 0 {
			engineID = locator.EngineID
		} else if engineID != locator.EngineID {
			return nil, 0, fmt.Errorf("所有关系输入和目标必须位于同一引擎")
		}
		tables[binding.Name] = dialect.QualifiedTable(locator.Path[0], locator.Path[1])
	}
	return tables, engineID, nil
}

func compileRelationInputs(
	query string,
	bindings []relationInputBinding,
	replacements map[string]string,
) (string, error) {
	bindingMap := make(map[string]relationInputBinding, len(bindings))
	for _, binding := range bindings {
		bindingMap[binding.Name] = binding
	}
	masked, err := maskPostgreSQLNamedParameters(query)
	if err != nil {
		return "", err
	}
	parsed, err := pgquery.Parse(masked)
	if err != nil || len(parsed.GetStmts()) != 1 || parsed.GetStmts()[0].GetStmt().GetSelectStmt() == nil {
		return "", fmt.Errorf("关系输入查询必须是 PostgreSQL 单条 SELECT")
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
			return "", fmt.Errorf("关系输入未被 SQL 引用: %s.%s", relationInputSchema, binding.Name)
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
		return fmt.Errorf("关系输入查询不允许数据修改子语句")
	}
	if node.GetRangeFunction() != nil || node.GetRangeTableFunc() != nil || node.GetJsonTable() != nil {
		return fmt.Errorf("关系输入查询不允许表函数数据源")
	}
	return i.inspectMessage(node.ProtoReflect(), scope, "")
}

func (i *relationInspector) inspectSelect(stmt *pgquery.SelectStmt, inherited map[string]struct{}) error {
	if stmt.GetIntoClause() != nil || len(stmt.GetLockingClause()) != 0 {
		return fmt.Errorf("关系输入查询不允许 SELECT INTO 或行锁")
	}
	scope := cloneStringSet(inherited)
	withClause := stmt.GetWithClause()
	if withClause != nil && withClause.GetRecursive() {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || strings.TrimSpace(cte.GetCtename()) == "" {
				return fmt.Errorf("关系输入查询包含无效 CTE")
			}
			scope[cte.GetCtename()] = struct{}{}
		}
	}
	if withClause != nil {
		for _, rawCTE := range withClause.GetCtes() {
			cte := rawCTE.GetCommonTableExpr()
			if cte == nil || cte.GetCtequery() == nil {
				return fmt.Errorf("关系输入查询包含无效 CTE")
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
		return fmt.Errorf("关系输入查询不允许跨数据库关系")
	}
	if relation.GetSchemaname() == "" {
		if _, cte := scope[relation.GetRelname()]; cte {
			return nil
		}
		return fmt.Errorf("关系输入查询只能读取已声明输入，禁止物理关系: %s", relation.GetRelname())
	}
	if relation.GetSchemaname() != relationInputSchema {
		return fmt.Errorf("关系输入查询只能读取 %s 下的已声明输入", relationInputSchema)
	}
	binding, exists := i.bindings[relation.GetRelname()]
	if !exists {
		return fmt.Errorf("SQL 引用了未声明的关系输入: %s.%s", relationInputSchema, relation.GetRelname())
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
		return fmt.Errorf("关系输入缺少执行期 locator: %s", binding.Name)
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
	if start < 0 || start >= len(query) || !strings.HasPrefix(query[start:], relationInputSchema) {
		return 0, 0, fmt.Errorf("关系输入必须使用未加引号的 %s.<name> 语法", relationInputSchema)
	}
	index := start + len(relationInputSchema)
	for index < len(query) && (query[index] == ' ' || query[index] == '\t' || query[index] == '\n' || query[index] == '\r') {
		index++
	}
	if index >= len(query) || query[index] != '.' {
		return 0, 0, fmt.Errorf("关系输入必须使用 %s.<name> 语法", relationInputSchema)
	}
	index++
	for index < len(query) && (query[index] == ' ' || query[index] == '\t' || query[index] == '\n' || query[index] == '\r') {
		index++
	}
	if !strings.HasPrefix(query[index:], bindingName) {
		return 0, 0, fmt.Errorf("关系输入名称与解析结果不一致")
	}
	end := index + len(bindingName)
	if end < len(query) && (query[end] == '_' || query[end] == '$' || query[end] >= '0' && query[end] <= '9' || query[end] >= 'a' && query[end] <= 'z' || query[end] >= 'A' && query[end] <= 'Z') {
		return 0, 0, fmt.Errorf("关系输入标识符边界无效")
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
