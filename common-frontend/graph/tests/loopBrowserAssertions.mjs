// Exercise real G6 shapes: a label/arrow on a collapsed quadratic path is not a loop.
export async function expectVisibleGraphLoop(view, label, expect) {
  await expect.poll(() => view.evaluate((element, label) => {
    const graph = element.__vueParentComponent.setupState.graphInstance
    const edge = graph?.getEdges().find(edge => edge.getModel().label === label)
    if (!edge || edge.getModel().type !== 'loop') return false
    const model = edge.getModel(), node = graph.findById(model.source)
    const shape = edge.getKeyShape(), line = shape.getCanvasBBox(), box = node.getKeyShape().getCanvasBBox()
    const text = edge.getContainer().find(shape => shape.get('name') === 'text-shape')?.getCanvasBBox()
    return Boolean(shape.attr('endArrow') && text && line.width > 0 && line.height > 0 &&
      (line.minY < box.minY || line.maxY > box.maxY || line.minX < box.minX || line.maxX > box.maxX) &&
      (text.maxY < box.minY || text.minY > box.maxY || text.maxX < box.minX || text.minX > box.maxX))
  }, label)).toBe(true)
}
