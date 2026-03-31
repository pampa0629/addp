/**
 * 简单矩形节点定义（用于 Orchestrator 编排）
 */

/**
 * 根据标识符生成一致的颜色
 */
export function generateColor(identifier) {
  if (!identifier) return '#5B8FF9'

  const predefinedColors = {
    'meta': '#5AD8A6',
    'transfer': '#5B8FF9',
    'manager': '#F6BD16',
    'develop': '#6DC8EC',
    'orchestrator': '#9270CA'
  }

  // 从 identifier 提取模块名（如 "meta.scanner.default" → "meta"）
  const moduleName = identifier.split('.')[0]
  if (predefinedColors[moduleName]) {
    return predefinedColors[moduleName]
  }

  // Hash 生成颜色
  let hash = 0
  for (let i = 0; i < identifier.length; i++) {
    hash = identifier.charCodeAt(i) + ((hash << 5) - hash)
  }
  const h = hash % 360
  return `hsl(${h}, 70%, 60%)`
}
