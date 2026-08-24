import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  new URL('../src/views/ConfigurationManagement.vue', import.meta.url),
  'utf8'
)

describe('configuration management module availability', () => {
  it('refreshes availability while the entry list is visible', () => {
    expect(source).toContain('const ENTRY_REFRESH_INTERVAL_MS = 10000')
    expect(source).toContain('window.setInterval(refreshEntries, ENTRY_REFRESH_INTERVAL_MS)')
    expect(source).toContain("!selectedOwner.value && document.visibilityState !== 'hidden'")
    expect(source).toContain('loadEntries({ silent: true })')
  })

  it('keeps the last known entries on a transient background failure', () => {
    expect(source).toContain('if (!silent) {')
    expect(source).toContain('entries.value = []')
    expect(source).toContain("ElMessage.error(t('console.configuration.loadFailed'))")
  })

  it('cleans up the availability timer when the page is unmounted', () => {
    expect(source).toContain('onBeforeUnmount(() => {')
    expect(source).toContain('window.clearInterval(refreshTimer)')
  })
})
