package scanruntime

import (
	"strings"

	"github.com/addp/meta/internal/models"
)

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
