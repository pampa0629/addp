import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const viewSource = readFileSync(
  new URL('../../src/views/Model3DTiles.vue', import.meta.url),
  'utf8'
)
const apiSource = readFileSync(
  new URL('../../src/api/quickView.js', import.meta.url),
  'utf8'
)
const previewPanelSource = readFileSync(
  new URL('../../src/components/explorer/PreviewPanel.vue', import.meta.url),
  'utf8'
)

describe('model3d tiles management deletion', () => {
  it('provides independent task and result delete actions', () => {
    expect(viewSource).toContain('@click="deleteTask(row)"')
    expect(viewSource).toContain('@click="deleteResult(row)"')
    expect(viewSource).toContain('deleteModel3DTilesTask(task.id)')
    expect(viewSource).toContain('deleteModel3DTilesResult(result.id)')
  })

  it('uses the canonical result DELETE endpoint', () => {
    expect(apiSource).toContain('deleteModel3DTilesResult(id)')
    expect(apiSource).toContain('request.delete(`/manager/model3d_tiles/${id}`)')
  })

  it('allows an existing task to generate again through the canonical execute endpoint', () => {
    expect(viewSource).toContain('@click="executeTask(row)"')
    expect(viewSource).toContain('taskExecutionActive(row)')
    expect(viewSource).toContain('useCurrentResultConfirmation')
    expect(viewSource).toContain('executeWithCurrentResultConfirmation(payload => quickViewAPI.executeModel3DTilesTask(task.id, payload))')
    expect(apiSource).toContain('request.post(`/manager/tasks/model3d_tiles_generation/${id}/execute`')
  })

  it('maps the shared confirmation payload to the quick-view action contract', () => {
    expect(previewPanelSource).toContain('const executeConfirmedQuickViewAction')
    expect(previewPanelSource).toContain('executeWithCurrentResultConfirmation(payload =>')
    expect(previewPanelSource).toContain('toQuickViewExistingResultPayload(payload)')
    expect(apiSource).toContain('executeQuickViewAction(locator, action, payload = {})')
  })

  it('guards URL restoration and result loading against stale async responses', () => {
    expect(viewSource).toContain('let workspaceRestoreSequence = 0')
    expect(viewSource).toContain('const restoreSequence = ++workspaceRestoreSequence')
    expect(viewSource).toContain('if (restoreSequence !== workspaceRestoreSequence) return')
    expect(viewSource).toContain('let resultsRequestSequence = 0')
    expect(viewSource).toContain('if (requestSequence !== resultsRequestSequence) return')
    expect(viewSource).toContain('routeDataReady = true\n  await Promise.all([loadQuickViewEngines(), restoreWorkspaceFromRoute()])')
  })

  it('restores a read-only task definition on the task tab', () => {
    expect(viewSource).toContain("tasks: ['task_id']")
    expect(viewSource).toContain("results: ['task_id']")
    expect(viewSource).toContain('@click="requestTaskDetail(row)"')
    expect(viewSource).toContain("history: 'push'")
    expect(viewSource).toContain('taskDetailVisible')
    expect(viewSource).toContain('getModel3DTilesTask(taskID)')
    expect(viewSource).toContain('@closed="clearTaskDetailRoute"')
  })

  it('does not expose direct model 3D Tiles task create or update API methods', () => {
    expect(apiSource).not.toContain('createModel3DTilesTask(')
    expect(apiSource).not.toContain('updateModel3DTilesTask(')
  })
})
