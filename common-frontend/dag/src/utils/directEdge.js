function shapeName(event) {
  return event?.shape?.get?.('name') || event?.shape?.cfg?.name || ''
}

export function isDAGPortEvent(event) {
  const shape = event?.shape
  return Boolean(
    shape?.cfg?.portType ||
    shape?.get?.('isAnchorPoint') ||
    shape?.cfg?.isAnchorPoint ||
    shapeName(event).startsWith('link-point-')
  )
}

export function createDAGDragNodeBehavior() {
  return {
    type: 'drag-node',
    shouldBegin: event => !isDAGPortEvent(event)
  }
}

export function validateDAGConnection({ graph, sourceId, targetId, hasLoop, isDuplicate } = {}) {
  if (!sourceId || !targetId || sourceId === targetId || hasLoop?.(sourceId, targetId)) {
    return 'loop'
  }
  const duplicated = (graph?.getEdges?.() || []).some(edge => {
    const model = edge.getModel()
    return typeof isDuplicate === 'function'
      ? isDuplicate(model)
      : model.source === sourceId && model.target === targetId
  })
  return duplicated ? 'duplicate' : true
}

export function createDAGDirectEdgeBehavior({
  resolveSource,
  resolveTarget,
  canConnect = () => true,
  buildEdgeConfig = () => ({}),
  onRejected = () => {}
} = {}) {
  if (typeof resolveSource !== 'function' || typeof resolveTarget !== 'function') {
    throw new Error('direct edge behavior requires resolveSource and resolveTarget')
  }

  return {
    type: 'create-edge',
    getEvents() {
      return {
        'node:mousedown': 'onPortMouseDown',
        mousemove: 'onPortMouseMove',
        'node:mouseup': 'onPortMouseUp',
        mouseup: 'onPortMouseUp',
        mouseleave: 'onPortMouseLeave'
      }
    },
    onPortMouseDown(event) {
      const source = resolveSource(event)
      if (!source) return

      cancelTemporaryEdge(this)
      const sourceId = itemId(event?.item)
      if (!sourceId) return

      this.__addpSourceId = sourceId
      this.__addpSourcePort = source
      this.__addpEdge = this.graph.addItem('edge', {
        source: sourceId,
        target: eventPoint(event),
        ...(buildEdgeConfig(connectionContext(this, event)) || {})
      }, false)
      this.__addpEdge?.getKeyShape?.()?.set?.('capture', false)
    },
    onPortMouseMove(event) {
      if (!this.__addpEdge) return
      this.graph.updateItem(this.__addpEdge, { target: eventPoint(event) }, false)
    },
    onPortMouseUp(event) {
      if (!this.__addpEdge) return
      const target = resolveTarget(event)
      if (!target) {
        onRejected({ reason: 'invalid_target', event })
        cancelTemporaryEdge(this)
        return
      }

      const context = connectionContext(this, event, target)
      const result = canConnect(context)
      if (result !== true) {
        onRejected({
          reason: typeof result === 'string' ? result : 'connection_rejected',
          event,
          context
        })
        cancelTemporaryEdge(this)
        return
      }

      const edge = this.__addpEdge
      this.graph.emit('beforecreateedge', {})
      this.graph.updateItem(edge, {
        target: context.targetId,
        ...(buildEdgeConfig(context) || {})
      }, false)
      edge?.getKeyShape?.()?.set?.('capture', true)
      clearTemporaryEdgeState(this)
      this.graph.emit('aftercreateedge', { edge })
    },
    onPortMouseLeave() {
      cancelTemporaryEdge(this)
    }
  }
}

function connectionContext(behavior, event, targetPort = null) {
  const graph = behavior.graph
  const sourceId = behavior.__addpSourceId
  const targetId = itemId(event?.item)
  return {
    graph,
    sourceId,
    targetId,
    sourceItem: graph?.findById?.(sourceId) || null,
    targetItem: graph?.findById?.(targetId) || event?.item || null,
    sourcePort: behavior.__addpSourcePort,
    targetPort
  }
}

function cancelTemporaryEdge(behavior) {
  if (behavior.__addpEdge && !behavior.__addpEdge.destroyed) {
    behavior.graph?.removeItem?.(behavior.__addpEdge, false)
  }
  clearTemporaryEdgeState(behavior)
}

function clearTemporaryEdgeState(behavior) {
  behavior.__addpEdge = null
  behavior.__addpSourceId = null
  behavior.__addpSourcePort = null
}

function itemId(item) {
  return item?.getID?.() || item?.getModel?.()?.id || null
}

function eventPoint(event) {
  return {
    x: Number(event?.x) || 0,
    y: Number(event?.y) || 0
  }
}

export function linkPointPort(event, direction) {
  const name = shapeName(event)
  if (name !== `link-point-${direction}`) return null
  return { name: direction }
}
