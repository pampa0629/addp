// G6 caches state styles separately from models. Reapply active states after
// updating the base appearance so selected/invalid/hover colors remain current.
export function updateDAGItemTheme(graph, item, appearance) {
  const states = [...item.getStates()]
  graph.clearItemStates(item)
  graph.updateItem(item, appearance)
  for (const state of states) graph.setItemState(item, state, true)
}
