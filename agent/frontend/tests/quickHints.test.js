import { describe, expect, it } from 'vitest'

import en from '../src/i18n/en.json'
import zhCN from '../src/i18n/zh-cn.json'

const supportedHintKeys = [
  'listWorkflowEngines',
  'listGeoPythonOperators',
  'listSparkOperators',
  'listPointCloudOperators'
]

describe('new chat capability examples', () => {
  it('keeps both locales aligned with the supported workflow-analysis scenarios', () => {
    expect(Object.keys(zhCN.agent.quickHints)).toEqual(supportedHintKeys)
    expect(Object.keys(en.agent.quickHints)).toEqual(supportedHintKeys)
  })

  it('does not advertise capabilities without an Agent Skill or Tool', () => {
    const hints = Object.values(zhCN.agent.quickHints).join('\n')

    expect(hints).not.toMatch(/数据目录|数据源|搜索|预览|导入|Shapefile|SQL|发布|执行工作流/)
    expect(hints).toContain('工作流引擎')
    expect(hints).toContain('GeoPython Workflow')
    expect(hints).toContain('Spark 工作流引擎')
    expect(hints).toContain('PointCloud 工作流引擎')
  })
})
