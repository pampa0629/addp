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
    expect(viewSource).toContain('executeModel3DTilesTask(task.id)')
    expect(apiSource).toContain('request.post(`/manager/tasks/model3d_tiles_generation/${id}/execute`')
  })
})
