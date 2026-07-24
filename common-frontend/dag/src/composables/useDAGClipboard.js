import { ref } from 'vue'
import { cloneDAGNodeForPaste, cloneDAGValue } from '../utils/editing.js'

export function useDAGClipboard(graph, { createNodeId, offset = 24 } = {}) {
  const copiedNode = ref(null)
  let pasteCount = 0

  function copy(item) {
    if (!item || item.getType?.() !== 'node') return false
    copiedNode.value = cloneDAGValue(item.getModel())
    pasteCount = 0
    return true
  }

  function paste({ position = null } = {}) {
    if (!graph.value || !copiedNode.value || typeof createNodeId !== 'function') return null
    pasteCount += 1
    const model = cloneDAGNodeForPaste(copiedNode.value, {
      id: createNodeId(copiedNode.value),
      offset: offset * pasteCount,
      position
    })
    if (!model) return null
    return graph.value.addItem('node', model)
  }

  function clear() {
    copiedNode.value = null
    pasteCount = 0
  }

  return {
    copiedNode,
    copy,
    paste,
    clear
  }
}
