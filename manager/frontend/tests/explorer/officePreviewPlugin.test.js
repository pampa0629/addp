import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import vm from 'node:vm'
import { describe, expect, it } from 'vitest'

const frontendRoot = resolve(import.meta.dirname, '../..')

function loadOfficePlugin() {
  const source = readFileSync(resolve(frontendRoot, 'public/plugins/office-preview.js'), 'utf8')
  const component = {}
  const context = {
    console: { log() {}, warn() {} },
    window: {
      DataExplorerPluginComponents: { OfficePreview: component },
      DataExplorerPlugins: []
    }
  }
  vm.runInNewContext(source, context)
  return { component, plugin: context.window.DataExplorerPlugins[0] }
}

describe('Office preview runtime plugin', () => {
  it('registers one renderer for the canonical office semantic', () => {
    const { component, plugin } = loadOfficePlugin()

    expect(plugin.name).toBe('office')
    expect(plugin.component).toBe(component)
    expect(plugin.canHandle({
      object: { content: { kind: 'wps', frontend_renderer: 'office', preview_material: 'url' } }
    })).toBe(true)
    expect(plugin.canHandle({
      object: { content: { kind: 'docx', frontend_renderer: 'docx', preview_material: 'url' } }
    })).toBe(false)
  })

  it('is the only Word-family script in the runtime manifest', () => {
    const manifest = JSON.parse(readFileSync(resolve(frontendRoot, 'public/plugins/manifest.json'), 'utf8'))

    expect(manifest.scripts).toContain('/plugins/office-preview.js')
    expect(manifest.scripts).not.toContain('/plugins/docx-preview.js')
    expect(manifest.scripts).not.toContain('/plugins/wps-preview.js')
  })
})
