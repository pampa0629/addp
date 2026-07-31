const EDITABLE_TARGET_SELECTOR = [
  'input',
  'textarea',
  'select',
  '[contenteditable="true"]',
  '[role="textbox"]'
].join(',')

export function isDAGKeyboardEventFromEditableTarget(event) {
  const target = event?.target
  if (!target) return false
  if (target.isContentEditable) return true
  return Boolean(target.closest?.(EDITABLE_TARGET_SELECTOR))
}

export function createDAGKeyboardHandler(actions = {}) {
  return event => {
    if (!event || event.defaultPrevented || isDAGKeyboardEventFromEditableTarget(event)) return false

    const actionName = resolveDAGKeyboardAction(event)
    const action = actionName ? actions[actionName] : null
    if (typeof action !== 'function') return false

    event.preventDefault?.()
    action(event)
    return true
  }
}

function resolveDAGKeyboardAction(event) {
  const key = String(event.key || '').toLowerCase()
  const modifier = Boolean(event.metaKey || event.ctrlKey)

  if (modifier && key === 'z') return event.shiftKey ? 'redo' : 'undo'
  if (modifier && key === 'y') return 'redo'
  if (modifier && key === 'c') return 'copy'
  if (modifier && key === 'v') return 'paste'
  if (modifier && key === 'd') return 'duplicate'
  if (!modifier && (key === 'delete' || key === 'backspace')) return 'delete'
  if (!modifier && key === 'escape') return 'cancelSelection'
  if (!modifier && !event.altKey && (key === 'arrowleft' || key === 'arrowup')) return 'selectPreviousNode'
  if (!modifier && !event.altKey && (key === 'arrowright' || key === 'arrowdown')) return 'selectNextNode'
  if (!modifier && !event.altKey && key === 'enter') return 'activateSelection'
  return null
}

export function sortDAGNodesSpatially(nodes = []) {
  return [...nodes].sort((left, right) => {
    const leftModel = left?.getModel?.() || left || {}
    const rightModel = right?.getModel?.() || right || {}
    const xDifference = numericCoordinate(leftModel.x) - numericCoordinate(rightModel.x)
    if (xDifference !== 0) return xDifference
    const yDifference = numericCoordinate(leftModel.y) - numericCoordinate(rightModel.y)
    if (yDifference !== 0) return yDifference
    return String(leftModel.id || '').localeCompare(String(rightModel.id || ''))
  })
}

export function findAdjacentDAGNode(nodes, currentNode, offset) {
  const orderedNodes = sortDAGNodesSpatially(nodes)
  if (!orderedNodes.length) return null
  const currentIndex = orderedNodes.indexOf(currentNode)
  const nextIndex = currentIndex < 0
    ? (offset < 0 ? orderedNodes.length - 1 : 0)
    : (currentIndex + offset + orderedNodes.length) % orderedNodes.length
  return orderedNodes[nextIndex]
}

function numericCoordinate(value) {
  const number = Number(value)
  return Number.isFinite(number) ? number : 0
}
