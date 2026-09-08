import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const viewSource = readFileSync(new URL('../../src/views/DerivedTasks.vue', import.meta.url), 'utf8')
const derivedTaskAPISource = readFileSync(new URL('../../src/api/derivedTasks.js', import.meta.url), 'utf8')
const quickViewAPISource = readFileSync(new URL('../../src/api/quickView.js', import.meta.url), 'utf8')
const previewPanelSource = readFileSync(new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url), 'utf8')

describe('model3d tiles management', () => {
  it('manages task definitions through the unified task API', () => {
    expect(viewSource).toContain('deleteDerivedTask(row.task_type, row.id)')
    expect(viewSource).toContain('executeDerivedTask(row.task_type, row.id')
    expect(derivedTaskAPISource).toContain('client.delete(`/manager/tasks/${encodeURIComponent(taskType)}/${id}`)')
    expect(derivedTaskAPISource).toContain('client.post(`/manager/tasks/${encodeURIComponent(taskType)}/${id}/execute`')
  })

  it('keeps model 3D Tiles results on their independent canonical endpoint', () => {
    expect(quickViewAPISource).toContain('deleteModel3DTilesResult(id)')
    expect(quickViewAPISource).toContain('request.delete(`/manager/model3d_tiles/${id}`)')
  })

  it('maps shared current-result confirmation to source-driven quick-view actions', () => {
    expect(previewPanelSource).toContain('const executeConfirmedQuickViewAction')
    expect(previewPanelSource).toContain('toQuickViewExistingResultPayload(payload)')
    expect(quickViewAPISource).toContain('executeQuickViewAction(locator, action, payload = {})')
  })

  it('restores a read-only task detail through the canonical task_id route state', () => {
    expect(viewSource).toContain('route.query.task_id')
    expect(viewSource).toContain('getDerivedTask(row.task_type, row.id)')
    expect(viewSource).toContain('@closed="clearTaskDetailRoute"')
  })

  it('does not expose direct model 3D Tiles task create or update API methods', () => {
    expect(quickViewAPISource).not.toContain('createModel3DTilesTask(')
    expect(quickViewAPISource).not.toContain('updateModel3DTilesTask(')
  })
})
