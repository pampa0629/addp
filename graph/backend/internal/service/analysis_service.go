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
	cypherAlgos = []string{"degree_centrality", "khop_neighbors", "multi_path"}
	gdsAlgos    = []string{"pagerank", "louvain", "wcc", "betweenness"}

	algoNames = map[string]string{
		"degree_centrality": "度中心性",
		"khop_neighbors":    "K跳邻居",
		"multi_path":        "多路最短路径",
		"pagerank":          "PageRank",
		"louvain":           "Louvain 社区发现",
		"wcc":               "弱连通分量",
		"betweenness":       "介数中心性",
	}
)

// AnalysisService 图算法分析服务
type AnalysisService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	ontologyRepo *repository.OntologyRepository
	systemClient *commonClient.SystemClient

	// GDS 能力缓存（实例级）
	gdsChecked  bool
	gdsAvail    bool
	gdsVersion  string
	gdsCacheMu  sync.RWMutex
}

func NewAnalysisService(
	graphRepo *repository.KnowledgeGraphRepository,
	ontologyRepo *repository.OntologyRepository,
	systemClient *commonClient.SystemClient,
) *AnalysisService {
	return &AnalysisService{
		graphRepo:    graphRepo,
		ontologyRepo: ontologyRepo,
		systemClient: systemClient,
	}
}

// getGraphAndEngine 与 Neo4jService 复用相同逻辑
func (s *AnalysisService) getGraphAndEngine(graphID, tenantID uint) (*models.KnowledgeGraph, *commonmodels.Engine, error) {
	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("knowledge graph not found: %w", err)
	}
	engine, err := s.systemClient.GetEngine(kg.EngineID)
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

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, "CALL gds.version() YIELD version RETURN version")
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

// CheckCapabilities 探测算法能力
func (s *AnalysisService) CheckCapabilities(ctx context.Context, graphID, tenantID uint) (*models.AlgorithmCapabilities, error) {
	_, engine, err := s.getGraphAndEngine(graphID, tenantID)
	if err != nil {
		return nil, err
	}
	gdsAvail, gdsVer := s.checkGDS(ctx, engine)
	caps := &models.AlgorithmCapabilities{
		GDSAvailable: gdsAvail,
		GDSVersion:   gdsVer,
		CypherAlgos:  cypherAlgos,
		GDSAlgos:     []string{},
	}
	if gdsAvail {
		caps.GDSAlgos = gdsAlgos
	}
	return caps, nil
}

// RunAlgorithm 执行指定算法
func (s *AnalysisService) RunAlgorithm(ctx context.Context, graphID, tenantID uint, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
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

	switch req.Algorithm {
	case "degree_centrality":
		return s.runDegreeCentrality(ctx, engine, req)
	case "khop_neighbors":
		return s.runKhopNeighbors(ctx, graphID, tenantID, engine, req)
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
	default:
		return nil, fmt.Errorf("UNKNOWN_ALGO:%s", req.Algorithm)
	}
}

// runDegreeCentrality 度中心性（Cypher）
func (s *AnalysisService) runDegreeCentrality(ctx context.Context, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	labelFilter := ""
	if len(req.NodeLabels) > 0 {
		quoted := make([]string, len(req.NodeLabels))
		for i, l := range req.NodeLabels {
			quoted[i] = fmt.Sprintf("'%s'", escapeCypher(l))
		}
		labelFilter = fmt.Sprintf("WHERE any(lbl IN labels(n) WHERE lbl IN [%s])", strings.Join(quoted, ","))
	}

	cypher := fmt.Sprintf(`
MATCH (n) %s
OPTIONAL MATCH (n)-[r]-()
WITH n, count(r) AS degree
RETURN elementId(n) AS node_id,
       coalesce(n.name, n.title, n.label, elementId(n)) AS display_name,
       labels(n)[0] AS entity_type,
       toFloat(degree) AS score
ORDER BY degree DESC
LIMIT %d`, labelFilter, req.Limit)

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("degree centrality failed: %w", err)
	}

	scores := rowsToNodeScores(result.Rows)
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

// runKhopNeighbors K跳邻居分析（Cypher）
func (s *AnalysisService) runKhopNeighbors(ctx context.Context, graphID, tenantID uint, engine *commonmodels.Engine, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
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
	if hops > 4 {
		hops = 4
	}

	cypher := fmt.Sprintf(
		"MATCH path = (start)-[*1..%d]-(n) WHERE elementId(start) = '%s' RETURN path LIMIT %d",
		hops, escapeCypher(nodeID), req.Limit,
	)
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
	if err != nil {
		return nil, fmt.Errorf("khop neighbors failed: %w", err)
	}

	kg, err := s.graphRepo.GetByID(graphID, tenantID)
	nodeColors, edgeColors := map[string]string{}, map[string]string{}
	if err == nil {
		nodeColors, edgeColors = buildColorMapsFromOntology(s.ontologyRepo, kg.OntologyID, tenantID)
	}
	subgraph := buildSubgraph(result, nodeColors, edgeColors)

	return &models.AlgorithmResult{
		Algorithm:     "khop_neighbors",
		AlgorithmName: "K跳邻居",
		NodeScores:    []models.NodeScore{},
		Subgraph:      subgraph,
		Metadata: map[string]interface{}{
			"elapsed_ms":     time.Since(start).Milliseconds(),
			"node_count":     len(subgraph.Nodes),
			"edge_count":     len(subgraph.Edges),
			"hops":           hops,
			"center_node_id": nodeID,
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
	if err == nil {
		nodeColors, edgeColors = buildColorMapsFromOntology(s.ontologyRepo, kg.OntologyID, tenantID)
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
			"MATCH p = allShortestPaths((a)-[*..10]-(b)) WHERE elementId(a) = '%s' AND elementId(b) = '%s' RETURN p LIMIT 5",
			escapeCypher(pair[0]), escapeCypher(pair[1]),
		)
		result, err := dbbridge.ExecuteGraphQuery(ctx, engine, cypher)
		if err != nil {
			continue
		}
		sub := buildSubgraph(result, nodeColors, edgeColors)
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
			"elapsed_ms": time.Since(start).Milliseconds(),
			"node_count": len(merged.Nodes),
			"edge_count": len(merged.Edges),
			"pair_count": len(pairs),
		},
	}, nil
}

// runGDSAlgo 执行 GDS 算法（含投影管理）
func (s *AnalysisService) runGDSAlgo(ctx context.Context, engine *commonmodels.Engine, graphID, tenantID uint, algo string, req *models.AlgorithmRunRequest) (*models.AlgorithmResult, error) {
	start := time.Now()

	projName := fmt.Sprintf("addp_tmp_%d_%d_%d", graphID, tenantID, time.Now().UnixMilli())

	// 构建投影参数
	nodeLabels := "'*'"
	if len(req.NodeLabels) > 0 {
		quoted := make([]string, len(req.NodeLabels))
		for i, l := range req.NodeLabels {
			quoted[i] = fmt.Sprintf("'%s'", escapeCypher(l))
		}
		nodeLabels = "[" + strings.Join(quoted, ",") + "]"
	}
	relTypes := "'*'"
	if len(req.RelTypes) > 0 {
		quoted := make([]string, len(req.RelTypes))
		for i, r := range req.RelTypes {
			quoted[i] = fmt.Sprintf("'%s'", escapeCypher(r))
		}
		relTypes = "[" + strings.Join(quoted, ",") + "]"
	}

	// 创建投影
	projCypher := fmt.Sprintf("CALL gds.graph.project('%s', %s, %s) YIELD graphName, nodeCount, relationshipCount",
		projName, nodeLabels, relTypes)
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
       coalesce(n.name, n.title, n.label, toString(id(n))) AS display_name,
       labels(n)[0] AS entity_type,
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
       coalesce(n.name, n.title, n.label, toString(id(n))) AS display_name,
       labels(n)[0] AS entity_type,
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
       coalesce(n.name, n.title, n.label, toString(id(n))) AS display_name,
       labels(n)[0] AS entity_type,
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
       coalesce(n.name, n.title, n.label, toString(id(n))) AS display_name,
       labels(n)[0] AS entity_type,
       toFloat(componentId) AS score,
       componentId AS community_id
ORDER BY componentId ASC
LIMIT %d`, projName, req.Limit)
	}

	result, err := dbbridge.ExecuteGraphQuery(ctx, engine, algoCypher)
	if err != nil {
		return nil, fmt.Errorf("GDS %s failed: %w", algo, err)
	}

	scores := rowsToNodeScoresWithCommunity(result.Rows)
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

// ============ 内部辅助 ============

func rowsToNodeScores(rows []map[string]interface{}) []models.NodeScore {
	scores := make([]models.NodeScore, 0, len(rows))
	for i, row := range rows {
		ns := models.NodeScore{
			Rank: i + 1,
		}
		if v, ok := row["node_id"]; ok {
			ns.NodeID = fmt.Sprintf("%v", v)
		}
		if v, ok := row["display_name"]; ok && v != nil {
			ns.DisplayName = fmt.Sprintf("%v", v)
		}
		if v, ok := row["entity_type"]; ok && v != nil {
			ns.EntityType = fmt.Sprintf("%v", v)
		}
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

func rowsToNodeScoresWithCommunity(rows []map[string]interface{}) []models.NodeScore {
	scores := rowsToNodeScores(rows)
	for i, row := range rows {
		if i < len(scores) {
			if v, ok := row["community_id"]; ok {
				scores[i].CommunityID = toInt64(v)
			}
		}
	}
	return scores
}

func buildColorMapsFromOntology(ontologyRepo *repository.OntologyRepository, ontologyID, tenantID uint) (nodeColors, edgeColors map[string]string) {
	nodeColors = make(map[string]string)
	edgeColors = make(map[string]string)
	ontology, err := ontologyRepo.GetDetail(ontologyID, tenantID)
	if err != nil {
		return
	}
	for _, et := range ontology.EntityTypes {
		if et.Color != "" {
			nodeColors[et.Name] = et.Color
		}
	}
	for _, rt := range ontology.RelationTypes {
		if rt.Color != "" {
			edgeColors[rt.Name] = rt.Color
		}
	}
	return
}
