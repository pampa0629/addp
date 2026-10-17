// G6 recomputes the fit from unscaled graph bounds; small graphs stay at 1×.
export function fitGraphToViewport(graph) {
  graph?.fitView(undefined, { onlyOutOfViewPort: true })
}
