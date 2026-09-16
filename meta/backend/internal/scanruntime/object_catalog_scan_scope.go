package scanruntime

import (
	"strings"

	"github.com/addp/meta/internal/models"
)

// objectCatalogScanState 保存本次真正覆盖的节点与未截断的失败路径。
// 执行报告的失败样本有数量和长度限制，不能作为目录状态的事实来源。
type objectCatalogScanState struct {
	nodes    map[uint]*models.MetaNode
	failures map[string]error
}

func newObjectCatalogScanState() *objectCatalogScanState {
	return &objectCatalogScanState{nodes: map[uint]*models.MetaNode{}, failures: map[string]error{}}
}

func (s *objectCatalogScanState) record(chain []*models.MetaNode, prefix string, includeBucket bool) {
	if s != nil {
		recordObjectCatalogScanNodes(s.nodes, chain, prefix, includeBucket)
	}
}

func (s *objectCatalogScanState) fail(fullPath string, err error) {
	if s != nil && err != nil {
		s.failures[strings.Trim(fullPath, "/")] = err
	}
}

func (s *objectCatalogScanState) errorFor(fullName string) error {
	for target, err := range s.failures {
		if target == fullName || strings.HasPrefix(target, fullName+"/") || strings.HasPrefix(fullName, target+"/") {
			// 节点只保留一个失败原因；完整失败计数与样本仍归 execution。
			return err
		}
	}
	return nil
}

// recordObjectCatalogScanNodes 只记录完整枚举范围内节点，用于完成扫描状态，不计算目录统计。
func recordObjectCatalogScanNodes(scannedNodes map[uint]*models.MetaNode, chain []*models.MetaNode, scanPrefix string, includeBucket bool) {
	if scannedNodes == nil || len(chain) == 0 {
		return
	}
	scope := chain[0].FullName
	if prefix := strings.Trim(scanPrefix, "/"); prefix != "" {
		scope += "/" + prefix
	}
	for i, node := range chain {
		if i == 0 && !includeBucket {
			continue
		}
		if node.FullName != scope && !strings.HasPrefix(node.FullName, scope+"/") {
			continue
		}
		scannedNodes[node.ID] = node
	}
}
