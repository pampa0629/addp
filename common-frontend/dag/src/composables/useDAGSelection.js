/**
 * DAG 节点/边选中管理 Composable
 */
import { ref } from 'vue'

export function useDAGSelection(graph) {
  const selectedItem = ref(null)

  /**
   * 初始化选中事件监听
   */
  function initSelectionListener() {
    graph.value.on('nodeselectchange', (evt) => {
      const selectedItems = evt.selectedItems
      if (selectedItems.nodes && selectedItems.nodes.length > 0) {
        selectedItem.value = selectedItems.nodes[0]
      } else if (selectedItems.edges && selectedItems.edges.length > 0) {
        selectedItem.value = selectedItems.edges[0]
      } else {
        selectedItem.value = null
      }
    })
  }

  /**
   * 删除选中的节点或边
   */
  function deleteSelected() {
    if (selectedItem.value) {
      graph.value.removeItem(selectedItem.value)
      selectedItem.value = null
      return true
    }
    return false
  }

  /**
   * 清空画布
   */
  function clearGraph() {
    graph.value.clear()
    selectedItem.value = null
  }

  return {
    selectedItem,
    initSelectionListener,
    deleteSelected,
    clearGraph
  }
}
