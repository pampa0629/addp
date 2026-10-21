// Browser-side observations of real G6/Element Plus components in isolated T3 fixtures.
export async function stableGraphFrame(element, expectedIDs) {
  const graph = element.__vueParentComponent.setupState.graphInstance
  let previous = '', stable = 0
  const deadline = performance.now() + 10_000
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    const nodes = graph.getNodes().map(node => {
      const model = node.getModel(), point = graph.getCanvasByPoint(model.x, model.y)
      // G6's cached canvas bbox can precede the latest focus transform.
      const shape = node.getKeyShape().getBBox()
      const first = graph.getCanvasByPoint(model.x + shape.minX, model.y + shape.minY)
      const last = graph.getCanvasByPoint(model.x + shape.maxX, model.y + shape.maxY)
      return { id: model._meta.id, x: point.x, y: point.y,
        box: [first.x, first.y, last.x, last.y], visible: node.isVisible(),
        labelVisible: node.getContainer().find(shape => shape.get('type') === 'text')?.get('visible') === true,
        matched: node.hasState('search-match') }
    })
    const width = graph.getWidth(), height = graph.getHeight()
    const valid = nodes.length === expectedIDs.length && expectedIDs.every(id => nodes.some(node => node.id === id)) &&
      nodes.every(node => node.visible && node.labelVisible && node.matched && node.box.every(Number.isFinite) &&
        node.box[0] >= 0 && node.box[1] >= 0 && node.box[2] <= width && node.box[3] <= height)
    const signature = JSON.stringify({ matrix: graph.get('group').getMatrix(), nodes })
    stable = valid && signature === previous ? stable + 1 : 0
    previous = signature
    if (stable >= 10) return { width, height, nodes, matrix: graph.get('group').getMatrix(), stableFrames: stable }
  }
  throw new Error(`Search nodes did not reach a visible stable frame: ${previous}`)
}

export async function stableEmptyReviewSelection(element) {
  const deadline = performance.now() + 10_000
  let previous = '', stable = 0
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    const boxes = [...element.querySelectorAll('.el-checkbox__input')]
    const states = boxes.map(box => {
      const input = box.querySelector('input'), inner = box.querySelector('.el-checkbox__inner')
      const reference = document.createElement('span')
      reference.className = 'el-checkbox__inner'
      reference.style.position = 'absolute'
      reference.style.visibility = 'hidden'
      box.append(reference)
      const actual = getComputedStyle(inner), expected = getComputedStyle(reference)
      const value = { checked: input.checked, indeterminate: input.indeterminate,
        checkedClass: box.classList.contains('is-checked'), mixedClass: box.classList.contains('is-indeterminate'),
        background: actual.backgroundColor, emptyBackground: expected.backgroundColor,
        tick: getComputedStyle(inner, '::after').transform, emptyTick: getComputedStyle(reference, '::after').transform,
        animating: inner.getAnimations({ subtree: true }).some(animation => ['running', 'pending'].includes(animation.playState)) }
      reference.remove()
      return value
    })
    const valid = states.length >= 2 && states.every(state => !state.checked && !state.indeterminate && !state.checkedClass &&
      !state.mixedClass && !state.animating && state.background === state.emptyBackground && state.tick === state.emptyTick)
    const signature = JSON.stringify(states)
    stable = valid && signature === previous ? stable + 1 : 0
    previous = signature
    if (stable >= 3) return states
  }
  throw new Error('Row/header checkboxes did not settle to their unchecked visual state')
}
