import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const derivedTasksSource = readFileSync(new URL('../../src/views/DerivedTasks.vue', import.meta.url), 'utf8')
const vectorizationSource = readFileSync(new URL('../../src/views/VectorizationTasks.vue', import.meta.url), 'utf8')
const routerSource = readFileSync(new URL('../../src/router/index.js', import.meta.url), 'utf8')

describe('Manager task workspace history', () => {
  it('uses one canonical derived task route for filters, details, and spatial creation', () => {
    expect(routerSource).toContain("path: 'derived-tasks'")
    expect(derivedTasksSource).toContain("await syncRoute({ task_id: String(row.id) }, 'push')")
    expect(derivedTasksSource).toContain("await syncRoute({ create: '1'")
    expect(derivedTasksSource).toContain('@closed="clearTaskDetailRoute"')
    expect(derivedTasksSource).toContain('@closed="clearEditorRoute"')
  })

  it('does not register the removed task-type page routes', () => {
    for (const routeName of ['GaussianSplatKSplat', 'Model3DGLB', 'PointCloudCOPC', 'RasterMosaic', 'TileCache', 'VectorMaterializedView', 'VectorTileSet', 'RasterCOG', 'Model3DTiles']) {
      expect(routerSource).not.toContain(`name: '${routeName}'`)
    }
  })

  it('keeps the independent vectorization workspace history behavior', () => {
    expect(vectorizationSource).toContain("history: 'push'")
    expect(vectorizationSource).toContain('requestCreateDialog')
    expect(vectorizationSource).toContain('requestEditTask')
  })
})
