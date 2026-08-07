package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

var (
	cypherAlgos  = []string{"degree_centrality", "khop_neighbors", "multi_path"}
	gdsAlgos     = []string{"pagerank", "louvain", "wcc", "betweenness"}
	spatialAlgos = []string{"nearby_nodes", "within_area"}

	algoNames = map[string]string{
		"degree_centrality": "度中心性",
		"khop_neighbors":    "K跳邻居",
		"multi_path":        "多路最短路径",
		"pagerank":          "PageRank",
		"louvain":           "Louvain 社区发现",
		"wcc":               "弱连通分量",
		"betweenness":       "介数中心性",
		"nearby_nodes":      "邻近节点",
		"within_area":       "区域内节点",
	}
)

// AnalysisService 图算法分析服务
type AnalysisService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	systemClient *commonClient.SystemServiceClient
	neo4jSvc     *Neo4jService

	// GDS 能力缓存（实例级）
	gdsChecked bool
	gdsAvail   bool
	gdsVersion string
	gdsCacheMu sync.RWMutex

	// Spatial 能力缓存（实例级）
	spatialChecked bool
	spatialAvail   bool
	spatialCacheMu sync.RWMutex
}

type analysisNodeShapeFilter struct {
	Name   string
	Labels []string
}

func NewAnalysisService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	systemClient *commonClient.SystemServiceClient,
	neo4jSvc *Neo4jService,
) *AnalysisService {
	return &AnalysisService{
		graphRepo:    graphRepo,
		ontologyRepo: ontologyRepo,
		systemClient: systemClient,
		neo4jSvc:     neo4jSvc,
	}
}

func analysisNodeShapeFilters(req *models.AlgorithmRunRequest) []analysisNodeShapeFilter {
	if req == nil {
		return nil
	}
	filters := make([]analysisNodeShapeFilter, 0, len(req.NodeShapes))
	for _, shape := range req.NodeShapes {
		labels := normalizedStringSet(shape.Labels)
		if len(labels) == 0 && strings.TrimSpace(shape.Name) != "" {
			labels = entityTypeLabels(shape.Name)
		}
		if len(labels) == 0 {
			continue
		}
		name := strings.TrimSpace(shape.Name)
		if name == "" {
			name = endpointShapeName(labels)
		}
		filters = append(filters, analysisNodeShapeFilter{Name: name, Labels: labels})
	}
	if len(filters) > 0 {
		return filters
	}
	return nil
}

func nodeShapeWhereClause(nodeVar string, filters []analysisNodeShapeFilter) string {
	if len(filters) == 0 {
		return ""
	}
	clauses := make([]string, 0, len(filters))
	for _, filter := range filters {
		clauses = append(clauses, fmt.Sprintf("all(label IN %s WHERE label IN labels(%s))", cypherStringList(filter.Labels), nodeVar))
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
}

func nodeShapeQueryPredicate(nodeVar string, filters []analysisNodeShapeFilter) string {
	businessPredicate := businessNodePredicate(nodeVar)
	clause := nodeShapeWhereClause(nodeVar, filters)
	if clause == "" {
		return businessPredicate
	}
	return businessPredicate + " AND " + clause
}

// getGraphAndEngine 与 Neo4jService 复用相同逻辑
func (s *AnalysisService) getGraphAndEngine(graphID, tenantID uint) (*models.KnowledgeGraph, *commonmodels.Engine, error) {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("knowledge graph not found: %w", err)
	}
	engine, err := s.systemClient.WithTenantID(tenantID).GetEngine(context.Background(), kg.EngineID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get engine (id=%d): %w", kg.EngineID, err)
	}
	engineCopy := *engine
	connCopy := make(commonmodels.ConnectionInfo, len(engine.ConnectionInfo))
	for k, v := range engine.ConnectionInfo {
		connCopy[k] = v
	}
	if kg.Database != "" {
		connCopy["database"] = kg.Database
	}
	engineCopy.ConnectionInfo = connCopy
	return kg, &engineCopy, nil
}

// checkGDS 检测 GDS 可用性（实例级缓存）
func (s *AnalysisService) checkGDS(ctx context.Context, engine *commonmodels.Engine) (bool, string) {
	s.gdsCacheMu.RLock()
	if s.gdsChecked {
		avail, ver := s.gdsAvail, s.gdsVersion
		s.gdsCacheMu.RUnlock()
		return avail, ver
	}
	s.gdsCacheMu.RUnlock()

	s.gdsCacheMu.Lock()
	defer s.gdsCacheMu.Unlock()
	// double-check
	if s.gdsChecked {
		return s.gdsAvail, s.gdsVersion
	}

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, "RETURN gds.version() AS version")
	s.gdsChecked = true
	if err != nil || len(result.Rows) == 0 {
		s.gdsAvail = false
		s.gdsVersion = ""
		return false, ""
	}
	s.gdsAvail = true
	s.gdsVersion = fmt.Sprintf("%v", result.Rows[0]["version"])
	return true, s.gdsVersion
}

// checkSpatial 检测 Neo4j Spatial 插件可用性（实例级缓存）
func (s *AnalysisService) checkSpatial(ctx context.Context, engine *commonmodels.Engine) bool {
	s.spatialCacheMu.RLock()
	if s.spatialChecked {
		avail := s.spatialAvail
		s.spatialCacheMu.RUnlock()
		return avail
	}
	s.spatialCacheMu.RUnlock()

	s.spatialCacheMu.Lock()
	defer s.spatialCacheMu.Unlock()
	if s.spatialChecked {
		return s.spatialAvail
	}

	// 直接调用 spatial.procedures()：安装了插件才会成功，否则报错
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL spatial.procedures() YIELD signature RETURN count(signature) AS cnt")
	s.spatialChecked = true
	if err != nil {
		s.spatialAvail = false
		return false
	}
	if len(result.Rows) == 0 {
		s.spatialAvail = false
		return false
	}
	cnt := toInt64(result.Rows[0]["cnt"])
	s.spatialAvail = cnt > 0
	return s.spatialAvail
}

// getAllEffectiveSpatialTypes 从本体中获取所有有效空间实体类型（含继承）
// 返回 map[layerName] -> SpatialLayerMapping。
func getAllEffectiveSpatialTypes(ontology *models.Ontology) map[string]SpatialLayerMapping {
	if ontology == nil {
		return map[string]SpatialLayerMapping{}
	}
	return buildSpatialLayerMappingsByLayerName(ontology.EntityTypes)
}

// CheckCapabilities 探测算法能力
func (s *AnalysisService) CheckCapabilities(ctx context.Context, graphID, tenantID uint) (*models.AlgorithmCapabilities, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	gdsAvail, gdsVer := s.checkGDS(ctx, engine)
	spatialAvail := s.checkSpatial(ctx, engine)
	caps := &models.AlgorithmCapabilities{
		GDSAvailable:     gdsAvail,
		GDSVersion:       gdsVer,
		SpatialAvailable: spatialAvail,
		SpatialLayers:    []models.SpatialLayerInfo{},
		PendingLayers:    []string{},
		CypherAlgos:      cypherAlgos,
		GDSAlgos:         []string{},
		SpatialAlgos:     []string{},
	}
	if gdsAvail {
		caps.GDSAlgos = gdsAlgos
	}
	if spatialAvail {
		caps.SpatialAlgos = spatialAlgos

		// 从本体中获取所有有效空间类型（含继承）
		ontology, _ := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
		allSpatialTypes := getAllEffectiveSpatialTypes(ontology)

		// 查询 Neo4j 中已有的空间图层
		existingLayerNames := make(map[string]bool)
		layersResult, err := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
		if err == nil && layersResult != nil {
			for _, row := range layersResult.Rows {
				name, ok := row["name"].(string)
				if !ok || name == "" {
					continue
				}
				existingLayerNames[name] = true
				info := models.SpatialLayerInfo{Name: name}
				if spatialType, ok := allSpatialTypes[name]; ok {
					info = spatialLayerInfoFromMapping(name, spatialType)
				}
				caps.SpatialLayers = append(caps.SpatialLayers, info)
			}
		}

		// 计算 pending_layers：本体中定义了空间类型但 Neo4j 中尚无图层
		for name := range allSpatialTypes {
			if !existingLayerNames[name] {
				caps.PendingLayers = append(caps.PendingLayers, name)
			}
		}
	}
	return caps, nil
}

// SyncAllSpatialLayers 将本体中所有有效空间类型（含继承）的图层同步到 Neo4j，并注册已有节点
// 幂等操作：图层已存在则跳过创建；节点注册用 MERGE 语义（spatial.addNode 本身幂等）
func (s *AnalysisService) SyncAllSpatialLayers(ctx context.Context, graphID, tenantID uint) ([]string, error) {
	kg, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("ontology not found: %w", err)
	}

	allSpatialTypes := getAllEffectiveSpatialTypes(ontology)
	if len(allSpatialTypes) == 0 {
		return []string{}, nil
	}

	// 查询已有图层
	existingLayers := make(map[string]bool)
	layersResult, _ := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL spatial.layers() YIELD name")
	if layersResult != nil {
		for _, row := range layersResult.Rows {
			if name, ok := row["name"].(string); ok && name != "" {
				existingLayers[name] = true
			}
		}
	}

	var synced []string
	for _, spatialType := range allSpatialTypes {
		cfg := spatialType.Config
		if cfg == nil {
			continue
		}
		layerName := cfg.LayerName

		// 1. 创建图层（若不存在）
		if !existingLayers[layerName] {
			var createCypher string
			switch cfg.GeometryType {
			case "wkt":
				createCypher = fmt.Sprintf(
					"CALL spatial.addWKTLayer('%s', '%s')",
					escapeCypher(layerName), escapeCypher(cfg.GeomField),
				)
			default: // "point" 或未指定
				createCypher = fmt.Sprintf(
					"CALL spatial.addPointLayerXY('%s', '%s', '%s')",
					escapeCypher(layerName), escapeCypher(cfg.LonField), escapeCypher(cfg.LatField),
				)
			}
			if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, createCypher); err != nil {
				return synced, fmt.Errorf("创建空间图层 %q 失败: %w", layerName, err)
			}
		}

		// 2. 将已有节点注册到空间图层
		var addNodesCypher string
		switch cfg.GeometryType {
		case "wkt":
			addNodesCypher = fmt.Sprintf(
				"MATCH (n%s) WHERE n.`%s` IS NOT NULL "+
					"CALL spatial.addNode('%s', n) YIELD node RETURN count(node) AS cnt",
				nodeLabelsPattern(spatialType.NodeLabels), escapeCypher(cfg.GeomField),
				escapeCypher(layerName),
			)
		default:
			addNodesCypher = fmt.Sprintf(
				"MATCH (n%s) WHERE n.`%s` IS NOT NULL AND n.`%s` IS NOT NULL "+
					"CALL spatial.addNode('%s', n) YIELD node RETURN count(node) AS cnt",
				nodeLabelsPattern(spatialType.NodeLabels), escapeCypher(cfg.LonField), escapeCypher(cfg.LatField),
				escapeCypher(layerName),
			)
		}
		if _, err := dbbridge.ExecuteGraphQuery(ctx, engine, addNodesCypher); err != nil {
			return synced, fmt.Errorf("注册节点到空间图层 %q 失败: %w", layerName, err)
		}

		synced = append(synced, layerName)
	}
	return synced, nil
}

// RunAlgorithm 执行指定算法
func (s *AnalysisService) RunAlgorithm(ctx context.Context, graphID, tenantID uint, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
	}
	if req.Algorithm == "khop_neighbors" {
		return s.runKhopNeighbors(ctx, graphID, tenantID, req)
	}

	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}

	// 检查 GDS 算法是否可用
	isGDS := false
	for _, a := range gdsAlgos {
		if a == req.Algorithm {
			isGDS = true
			break
		}
	}
	if isGDS {
		avail, _ := s.checkGDS(ctx, engine)
		if !avail {
			return nil, fmt.Errorf("GDS_UNAVAILABLE")
		}
	}

	// 检查 Spatial 算法是否可用
	isSpatial := false
	for _, a := range spatialAlgos {
		if a == req.Algorithm {
			isSpatial = true
			break
		}
	}
	if isSpatial {
		avail := s.checkSpatial(ctx, engine)
		if !avail {
			return nil, fmt.Errorf("SPATIAL_UNAVAILABLE")
		}
	}

	switch req.Algorithm {
	case "degree_centrality":
		return s.runDegreeCentrality(ctx, graphID, tenantID, engine, req)
	case "multi_path":
		return s.runMultiPath(ctx, graphID, tenantID, engine, req)
	case "pagerank":
		return s.runGDSAlgo(ctx, engine, graphID, tenantID, "pagerank", req)
	case "louvain":
		return s.runGDSAlgo(ctx, engine, graphID, tenantID, "louvain", req)
	case "wcc":
		return s.runGDSAlgo(ctx, engine, graphID, tenantID, "wcc", req)
	case "betweenness":
		return s.runGDSAlgo(ctx, engine, graphID, tenantID, "betweenness", req)
	case "nearby_nodes":
		return s.runNearbyNodes(ctx, graphID, tenantID, engine, req)
	case "within_area":
		return s.runWithinArea(ctx, graphID, tenantID, engine, req)
	default:
		return nil, fmt.Errorf("UNKNOWN_ALGO:%s", req.Algorithm)
	}
}

// runDegreeCentrality 度中心性（Cypher）
func (s *AnalysisService) runDegreeCentrality(ctx context.Context, graphID, tenantID uint, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	labelFilter := ""
	if clause := nodeShapeQueryPredicate("n", analysisNodeShapeFilters(req)); clause != "" {
		labelFilter = "WHERE " + clause
	}

	cypher := fmt.Sprintf(`
MATCH (n) %s
CALL {
  WITH n
  OPTIONAL MATCH (n)-[r]-()
  WHERE NOT (`+internalRelationshipTypePredicate+`)
  RETURN count(r) AS degree
}
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       toFloat(degree) AS score
ORDER BY degree DESC
LIMIT %d`, labelFilter, req.Limit)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("degree centrality failed: %w", err)
	}

	scores := rowsToNodeScores(result.Rows, s.analysisOntologySemantics(graphID, tenantID))
	return &models.AlgorithmResult{
		Algorithm:     "degree_centrality",
		AlgorithmName: "度中心性",
		NodeScores:    scores,
		Subgraph:      nil,
		Metadata: map[string]interface{}{
			"elapsed_ms": time.Since(start).Milliseconds(),
			"node_count": len(scores),
		},
	}, nil
}

// runKhopNeighbors 复用探索页的统一双预算展开语义。
func (s *AnalysisService) runKhopNeighbors(ctx context.Context, graphID, tenantID uint, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	nodeID, _ := req.Params["node_id"].(string)
	if nodeID == "" {
		return nil, fmt.Errorf("params.node_id is required for khop_neighbors")
	}
	hops := 2
	if h, ok := req.Params["hops"]; ok {
		switch v := h.(type) {
		case float64:
			hops = int(v)
		case int:
			hops = v
		}
	}
	if hops < 1 {
		hops = 1
	}
	if hops > MaxExpandDepth {
		hops = MaxExpandDepth
	}
	subgraph, err := s.neo4jSvc.Expand(ctx, graphID, tenantID, &models.ExpandRequest{
		Target:            models.ExpandTarget{Kind: "entity", ID: nodeID},
		Depth:             hops,
		NodeLimit:         req.Limit,
		RelationshipLimit: min(req.Limit*2, MaxExpandRelationshipLimit),
	})
	if err != nil {
		return nil, fmt.Errorf("khop neighbors failed: %w", err)
	}

	return &models.AlgorithmResult{
		Algorithm:     "khop_neighbors",
		AlgorithmName: "K跳邻居",
		NodeScores:    []models.NodeScore{},
		Subgraph:      subgraph,
		Metadata: map[string]interface{}{
			"elapsed_ms":         time.Since(start).Milliseconds(),
			"node_count":         len(subgraph.Nodes),
			"relationship_count": len(subgraph.Edges),
			"hops":               hops,
			"center_node_id":     nodeID,
		},
	}, nil
}

// runMultiPath 多路最短路径（Cypher）
func (s *AnalysisService) runMultiPath(ctx context.Context, graphID, tenantID uint, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	// 从 params.pairs 中读取 source-target 对（最多5对）
	var pairs [][2]string
	if rawPairs, ok := req.Params["pairs"]; ok {
		if pairList, ok := rawPairs.([]interface{}); ok {
			for _, p := range pairList {
				if pair, ok := p.([]interface{}); ok && len(pair) == 2 {
					src := fmt.Sprintf("%v", pair[0])
					tgt := fmt.Sprintf("%v", pair[1])
					pairs = append(pairs, [2]string{src, tgt})
				}
				if len(pairs) >= 5 {
					break
				}
			}
		}
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("params.pairs is required for multi_path, e.g. [[srcId, tgtId], ...]")
	}

	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	nodeColors, edgeColors := map[string]string{}, map[string]string{}
	edgeDirections := map[string]bool{}
	semantics := newOntologySemantics(nil)
	if err == nil {
		ontology, ontologyErr := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
		if ontologyErr == nil {
			nodeColors, edgeColors, edgeDirections = buildVisualMaps(ontology)
			semantics = newOntologySemantics(ontology)
		}
	}

	// 合并多对路径结果
	merged := &models.SubgraphResult{
		Nodes: []models.GraphNodeDTO{},
		Edges: []models.GraphEdgeDTO{},
	}
	nodeSet := map[string]bool{}
	edgeSet := map[string]bool{}

	for _, pair := range pairs {
		cypher := fmt.Sprintf(
			"MATCH p = allShortestPaths((a)-[*..10]-(b)) WHERE elementId(a) = '%s' AND elementId(b) = '%s' AND NONE(rel IN relationships(p) WHERE type(rel) IN "+internalRelationshipTypeList+") RETURN p LIMIT 5",
			escapeCypher(pair[0]), escapeCypher(pair[1]),
		)
		result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
		if err != nil {
			continue
		}
		sub := buildSubgraph(result, nodeColors, edgeColors, edgeDirections, semantics)
		for _, n := range sub.Nodes {
			if !nodeSet[n.ID] {
				nodeSet[n.ID] = true
				merged.Nodes = append(merged.Nodes, n)
			}
		}
		for _, e := range sub.Edges {
			if !edgeSet[e.ID] {
				edgeSet[e.ID] = true
				merged.Edges = append(merged.Edges, e)
			}
		}
	}

	return &models.AlgorithmResult{
		Algorithm:     "multi_path",
		AlgorithmName: "多路最短路径",
		NodeScores:    []models.NodeScore{},
		Subgraph:      merged,
		Metadata: map[string]interface{}{
			"elapsed_ms":         time.Since(start).Milliseconds(),
			"node_count":         len(merged.Nodes),
			"relationship_count": len(merged.Edges),
			"pair_count":         len(pairs),
		},
	}, nil
}

// runGDSAlgo 执行 GDS 算法（含投影管理）
func (s *AnalysisService) runGDSAlgo(ctx context.Context, engine *commonmodels.Engine, graphID, tenantID uint, algo string, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	projName := fmt.Sprintf("addp_tmp_%d_%d_%d", graphID, tenantID, time.Now().UnixMilli())

	selectedRelTypes := make([]string, 0, len(req.RelTypes))
	if len(req.RelTypes) > 0 {
		for _, r := range req.RelTypes {
			if isInternalRelationshipType(r) {
				continue
			}
			selectedRelTypes = append(selectedRelTypes, r)
		}
		if len(selectedRelTypes) == 0 {
			return nil, fmt.Errorf("no business relationship types selected")
		}
	}

	filters := analysisNodeShapeFilters(req)
	projCypher := buildGDSProjectionCypher(projName, selectedRelTypes, filters)
	_, err := dbbridge.ExecuteGraphQuery(ctx, engine, projCypher)
	if err != nil {
		return nil, fmt.Errorf("GDS projection failed: %w", err)
	}

	// defer 清理投影
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dropCypher := fmt.Sprintf("CALL gds.graph.drop('%s', false)", projName)
		_, _ = dbbridge.ExecuteGraphQuery(cleanCtx, engine, dropCypher)
	}()

	// 执行算法
	var algoCypher string
	switch algo {
	case "pagerank":
		algoCypher = fmt.Sprintf(`
CALL gds.pageRank.stream('%s', {maxIterations:20, dampingFactor:0.85})
YIELD nodeId, score
MATCH (n) WHERE id(n) = nodeId
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       score,
       0 AS community_id
ORDER BY score DESC
LIMIT %d`, projName, req.Limit)

	case "betweenness":
		algoCypher = fmt.Sprintf(`
CALL gds.betweenness.stream('%s')
YIELD nodeId, score
MATCH (n) WHERE id(n) = nodeId
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       score,
       0 AS community_id
ORDER BY score DESC
LIMIT %d`, projName, req.Limit)

	case "louvain":
		algoCypher = fmt.Sprintf(`
CALL gds.louvain.stream('%s')
YIELD nodeId, communityId
MATCH (n) WHERE id(n) = nodeId
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       toFloat(communityId) AS score,
       communityId AS community_id
ORDER BY communityId ASC
LIMIT %d`, projName, req.Limit)

	case "wcc":
		algoCypher = fmt.Sprintf(`
CALL gds.wcc.stream('%s')
YIELD nodeId, componentId
MATCH (n) WHERE id(n) = nodeId
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       toFloat(componentId) AS score,
       componentId AS community_id
ORDER BY componentId ASC
LIMIT %d`, projName, req.Limit)
	}

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, algoCypher)
	if err != nil {
		return nil, fmt.Errorf("GDS %s failed: %w", algo, err)
	}

	scores := rowsToNodeScoresWithCommunity(result.Rows, s.analysisOntologySemantics(graphID, tenantID))
	metadata := map[string]interface{}{
		"elapsed_ms": time.Since(start).Milliseconds(),
		"node_count": len(scores),
	}
	// 统计社区数（louvain/wcc）
	if algo == "louvain" || algo == "wcc" {
		communitySet := map[int64]bool{}
		for _, ns := range scores {
			communitySet[ns.CommunityID] = true
		}
		metadata["community_count"] = len(communitySet)
	}

	return &models.AlgorithmResult{
		Algorithm:     algo,
		AlgorithmName: algoNames[algo],
		NodeScores:    scores,
		Subgraph:      nil,
		Metadata:      metadata,
	}, nil
}

func buildGDSProjectionCypher(projName string, selectedRelTypes []string, filters []analysisNodeShapeFilter) string {
	relWhereParts := []string{businessRelationshipPredicate("r", "source", "target")}
	if len(selectedRelTypes) > 0 {
		relWhereParts = append(relWhereParts, fmt.Sprintf("type(r) IN %s", cypherStringList(selectedRelTypes)))
	}
	relWhere := strings.Join(relWhereParts, " AND ")
	if len(filters) == 0 {
		return fmt.Sprintf(
			"CALL gds.graph.project.cypher('%s', 'MATCH (n) WHERE %s RETURN id(n) AS id', 'MATCH (source)-[r]->(target) WHERE %s RETURN id(source) AS source, id(target) AS target') YIELD graphName, nodeCount, relationshipCount",
			escapeCypher(projName),
			escapeCypher(businessNodePredicate("n")),
			escapeCypher(relWhere),
		)
	}
	nodeWhere := nodeShapeQueryPredicate("n", filters)
	sourceWhere := nodeShapeQueryPredicate("source", filters)
	targetWhere := nodeShapeQueryPredicate("target", filters)
	return fmt.Sprintf(
		"CALL gds.graph.project.cypher('%s', 'MATCH (n) WHERE %s RETURN id(n) AS id', 'MATCH (source)-[r]->(target) WHERE %s AND %s AND %s RETURN id(source) AS source, id(target) AS target') YIELD graphName, nodeCount, relationshipCount",
		escapeCypher(projName),
		escapeCypher(nodeWhere),
		escapeCypher(relWhere),
		escapeCypher(sourceWhere),
		escapeCypher(targetWhere),
	)
}

// runNearbyNodes 邻近节点查询（Spatial 插件：spatial.withinDistance）
// params: lon(float), lat(float), radius_km(float, 默认10), layer(string, 必须指定)
func (s *AnalysisService) runNearbyNodes(ctx context.Context, graphID, tenantID uint, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	lon := floatParam(req.Params, "lon", 0)
	lat := floatParam(req.Params, "lat", 0)
	radiusKm := floatParam(req.Params, "radius_km", 10)
	layer := stringParam(req.Params, "layer", "")

	if layer == "" {
		return nil, fmt.Errorf("params.layer is required for nearby_nodes, please select a spatial layer")
	}

	cypher := fmt.Sprintf(`
CALL spatial.withinDistance('%s', {lon:%s, lat:%s}, %s) YIELD node AS n, distance
RETURN elementId(n) AS node_id,
       properties(n) AS node_properties,
       labels(n) AS node_labels,
       distance AS score
ORDER BY distance ASC
LIMIT %d`,
		escapeCypher(layer),
		fmt.Sprintf("%g", lon), fmt.Sprintf("%g", lat), fmt.Sprintf("%g", radiusKm),
		req.Limit,
	)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("nearby nodes query failed: %w", err)
	}

	scores := rowsToNodeScores(result.Rows, s.analysisOntologySemantics(graphID, tenantID))
	return &models.AlgorithmResult{
		Algorithm:     "nearby_nodes",
		AlgorithmName: "邻近节点",
		NodeScores:    scores,
		Metadata: map[string]interface{}{
			"elapsed_ms": time.Since(start).Milliseconds(),
			"node_count": len(scores),
			"lon":        lon,
			"lat":        lat,
			"radius_km":  radiusKm,
			"layer":      layer,
			"score_unit": "km",
		},
	}, nil
}

// runWithinArea 区域内节点查询（Spatial 插件：spatial.withinGeometry）
// 取面图层中指定区域节点的 WKT 几何，查询点图层中落在该范围内的所有节点
// params: area_layer(string), area_node_id(string), area_geom_field(string), point_layer(string)
func (s *AnalysisService) runWithinArea(ctx context.Context, graphID, tenantID uint, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	areaLayer := stringParam(req.Params, "area_layer", "")
	areaNodeID := stringParam(req.Params, "area_node_id", "")
	areaGeomField := stringParam(req.Params, "area_geom_field", "wkt")
	pointLayer := stringParam(req.Params, "point_layer", "")

	if areaLayer == "" {
		return nil, fmt.Errorf("params.area_layer is required")
	}
	if areaNodeID == "" {
		return nil, fmt.Errorf("params.area_node_id is required")
	}
	if pointLayer == "" {
		return nil, fmt.Errorf("params.point_layer is required")
	}

	cypher := fmt.Sprintf(
		"MATCH (area) WHERE elementId(area) = '%s'\n"+
			"WITH area.`%s` AS areaGeom\n"+
			"WHERE areaGeom IS NOT NULL\n"+
			"CALL spatial.intersects('%s', areaGeom) YIELD node AS n\n"+
			"RETURN elementId(n) AS node_id,\n"+
			"       properties(n) AS node_properties,\n"+
			"       labels(n) AS node_labels,\n"+
			"       0.0 AS score\n"+
			"LIMIT %d",
		escapeCypher(areaNodeID),
		escapeCypher(areaGeomField),
		escapeCypher(pointLayer),
		req.Limit,
	)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("within area query failed: %w", err)
	}

	scores := rowsToNodeScores(result.Rows, s.analysisOntologySemantics(graphID, tenantID))
	return &models.AlgorithmResult{
		Algorithm:     "within_area",
		AlgorithmName: "区域内节点",
		NodeScores:    scores,
		Metadata: map[string]interface{}{
			"elapsed_ms":  time.Since(start).Milliseconds(),
			"node_count":  len(scores),
			"area_layer":  areaLayer,
			"point_layer": pointLayer,
		},
	}, nil
}

// ============ 内部辅助 ============

func (s *AnalysisService) analysisOntologySemantics(graphID, tenantID uint) *ontologySemantics {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return newOntologySemantics(nil)
	}
	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, tenantID)
	if err != nil {
		return newOntologySemantics(nil)
	}
	return newOntologySemantics(ontology)
}

func rowsToNodeScores(rows []map[string]interface{}, semantics *ontologySemantics) []models.NodeScore {
	scores := make([]models.NodeScore, 0, len(rows))
	for i, row := range rows {
		ns := models.NodeScore{
			Rank: i + 1,
		}
		if v, ok := row["node_id"]; ok {
			ns.NodeID = fmt.Sprintf("%v", v)
		}
		if v, ok := row["entity_type"]; ok && v != nil {
			ns.EntityType = fmt.Sprintf("%v", v)
		}
		var labels []string
		if v, ok := row["node_labels"]; ok && v != nil {
			labels = interfaceToStringSlice(v)
			if len(labels) > 0 {
				ns.EntityType = endpointShapeName(labels)
			}
		}
		properties, _ := row["node_properties"].(map[string]interface{})
		if semantics == nil {
			semantics = newOntologySemantics(nil)
		}
		ns.DisplayName = semantics.displayName(labels, properties, ns.NodeID)
		if v, ok := row["score"]; ok {
			switch sv := v.(type) {
			case float64:
				ns.Score = sv
			case int64:
				ns.Score = float64(sv)
			case int:
				ns.Score = float64(sv)
			}
		}
		scores = append(scores, ns)
	}
	return scores
}

func rowsToNodeScoresWithCommunity(rows []map[string]interface{}, semantics *ontologySemantics) []models.NodeScore {
	scores := rowsToNodeScores(rows, semantics)
	for i, row := range rows {
		if i < len(scores) {
			if v, ok := row["community_id"]; ok {
				scores[i].CommunityID = toInt64(v)
			}
		}
	}
	return scores
}

// floatParam 从 params map 中安全读取 float64，不存在或类型不符时返回 defaultVal
func floatParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if params == nil {
		return defaultVal
	}
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return defaultVal
}

// stringParam 从 params map 中安全读取 string，不存在时返回 defaultVal
func stringParam(params map[string]interface{}, key string, defaultVal string) string {
	if params == nil {
		return defaultVal
	}
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}
