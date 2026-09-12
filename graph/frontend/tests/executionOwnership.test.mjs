import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const manager = readFileSync(new URL('../src/views/BuildManager.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/BuildTaskDetail.vue', import.meta.url), 'utf8')

describe('Graph execution monitoring ownership', () => {
  it('uses the shared Monitor entry at module and task scope', () => {
    expect(manager).toMatch(/MonitorExecutionsButton[^>]+module="graph"[^>]+task-type="kg_build"/)
    expect(detail).toContain('MonitorExecutionsButton')
    expect(detail).toContain(':source-task-id="taskId"')
    expect(detail).toContain('scope="task"')
  })
})
