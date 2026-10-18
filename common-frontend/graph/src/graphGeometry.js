export function graphEdgeType(source, target) {
  return source === target ? 'loop' : 'quadratic'
}
