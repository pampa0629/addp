/**
 * DAG 节点/边选中管理 Composable
 */
import { ref } from 'vue'
import { findAdjacentDAGNode } from '../utils/keyboard.js'

export function useDAGSelection(graph, { focusTarget = null } = {}) {
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
      if (selectedItem.value) focusSelectionTarget()
    })
  }

  function focusSelectionTarget() {
    const target = focusTarget?.value || focusTarget
    target?.focus?.({ preventScroll: true })
  }

  function selectItem(item) {
    if (!item || !graph.value) return null
    const items = [
      ...(graph.value.getNodes?.() || []),
      ...(graph.value.getEdges?.() || [])
    ]
    items.forEach(candidate => {
      graph.value.setItemState?.(candidate, 'selected', candidate === item)
    })
    selectedItem.value = item
    focusSelectionTarget()
    return item
  }

  function selectAdjacentNode(offset) {
    const item = findAdjacentDAGNode(
      graph.value?.getNodes?.() || [],
      selectedItem.value,
      offset
    )
    return selectItem(item)
  }

  function selectPreviousNode() {
    return selectAdjacentNode(-1)
  }

  function selectNextNode() {
    return selectAdjacentNode(1)
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

  function clearSelection() {
    if (!selectedItem.value) return false
    graph.value?.setItemState?.(selectedItem.value, 'selected', false)
    selectedItem.value = null
    return true
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
    selectItem,
    selectPreviousNode,
    selectNextNode,
    deleteSelected,
    clearSelection,
    clearGraph
  }
}
