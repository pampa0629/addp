/**
 * DAG 连线模式管理 Composable
 */
import { ref } from 'vue'

export function useDAGEdgeMode(graph) {
  const isAddEdgeMode = ref(false)

  /**
   * 切换连线模式
   */
  function toggleAddEdgeMode() {
    isAddEdgeMode.value = !isAddEdgeMode.value

    if (isAddEdgeMode.value) {
      graph.value.setMode('addEdge')
    } else {
      graph.value.setMode('default')
    }

    return isAddEdgeMode.value
  }

  /**
   * 退出连线模式
   */
  function exitAddEdgeMode() {
    if (isAddEdgeMode.value) {
      isAddEdgeMode.value = false
      graph.value.setMode('default')
    }
  }

  return {
    isAddEdgeMode,
    toggleAddEdgeMode,
    exitAddEdgeMode
  }
}
