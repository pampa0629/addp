import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

const taskWorkspace = readFileSync(new URL('../../src/views/DerivedTasks.vue', import.meta.url), 'utf8')

describe('Manager execution monitoring ownership', () => {
  it('uses the shared Monitor entry from every data task category', () => {
    expect(taskWorkspace).toContain('MonitorExecutionsButton')
    expect(taskWorkspace).toContain('module="manager"')
    expect(taskWorkspace).toContain(':task-type="monitorTaskType"')
    expect(taskWorkspace).not.toMatch(/path:\s*['"]\/executions['"]/)
  })
})
