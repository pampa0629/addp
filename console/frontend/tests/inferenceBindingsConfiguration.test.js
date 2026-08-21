import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  new URL('../src/components/configuration/InferenceBindingsConfiguration.vue', import.meta.url),
  'utf8'
)

describe('inference bindings configuration', () => {
  it('uses one page-level save action for all changed bindings', () => {
    expect(source).toContain('@click="saveAll"')
    expect(source).toContain('Promise.all(changedRows.value.map')
    expect(source).toContain('row.modelProfileId !== row.originalModelProfileId')
    expect(source).not.toContain('@click="save(row)"')
    expect(source).not.toContain('row.saving')
  })
})
