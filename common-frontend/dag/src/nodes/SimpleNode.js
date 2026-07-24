/**
 * 简单矩形节点定义（用于 Orchestrator 编排）
 */

/**
 * 根据标识符生成一致的颜色
 */
export function generateColor(identifier) {
  const style = typeof document === 'undefined' ? null : getComputedStyle(document.documentElement)
  const color = variable => style?.getPropertyValue(variable).trim()
  if (!identifier) return color('--el-color-primary')

  const predefinedColors = {
    meta: color('--addp-module-meta'),
    transfer: color('--addp-module-transfer'),
    manager: color('--addp-module-manager'),
    develop: color('--addp-module-develop'),
    orchestrator: color('--addp-module-orchestrator')
  }

  // 从 identifier 提取模块名（如 "meta.scanner.default" → "meta"）
  const moduleName = identifier.split('.')[0]
  if (predefinedColors[moduleName]) {
    return predefinedColors[moduleName]
  }

  return color('--el-color-primary')
}
