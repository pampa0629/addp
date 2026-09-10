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
    expect(derivedTasksSource).toContain('QuickViewTaskCreator')
    expect(derivedTasksSource).not.toContain('openDataExplorer')
    expect(derivedTasksSource).toContain('@closed="clearTaskDetailRoute"')
    expect(derivedTasksSource).toContain('@closed="clearEditorRoute"')
  })

  it('does not register the removed task-type page routes', () => {
    for (const routeName of ['GaussianSplatKSplat', 'Model3DGLB', 'PointCloudCOPC', 'RasterMosaic', 'TileCache', 'VectorMaterializedView', 'VectorTileSet', 'RasterCOG', 'Model3DTiles']) {
      expect(routerSource).not.toContain(`name: '${routeName}'`)
    }
  })

  it('uses the shared cross-module monitor route and canonical resource-tree locator', () => {
    expect(derivedTasksSource).toContain('openMonitorExecution(row.last_execution_id)')
    expect(derivedTasksSource).not.toContain("navigateConsoleModuleRoute(router, 'monitor'")
    expect(derivedTasksSource).toContain("path: '/data-explorer', query: { locator: sourceLocator(row) }")
  })

  it('shows engine names and a user-oriented task detail instead of internal identifiers', () => {
    expect(derivedTasksSource).toContain('useQuickViewResourceDisplay(t)')
    expect(derivedTasksSource).not.toContain('`#${engineID}`')
    expect(derivedTasksSource).not.toContain('selectedTask.semantic_key')
    expect(derivedTasksSource).not.toContain('JSON.stringify(selectedTask?.config')
  })

  it('integrates vectorization into the canonical data-task route while preserving workspace history', () => {
    expect(routerSource).not.toContain("path: 'vectorization-tasks'")
    expect(derivedTasksSource).toContain("name=\"embedding\"")
    expect(derivedTasksSource).toContain('<VectorizationTasks')
    expect(vectorizationSource).toContain("tasks: ['category', 'create', 'task_id']")
    expect(vectorizationSource).toContain("history: 'push'")
    expect(vectorizationSource).toContain('requestCreateDialog')
    expect(vectorizationSource).toContain('requestEditTask')
  })

  it('does not expose Manager infra quick-view results as task actions', () => {
    expect(derivedTasksSource).not.toContain('viewTaskResult')
    expect(derivedTasksSource).not.toContain("manager.derivedTasks.viewResult")
    expect(derivedTasksSource).not.toContain("manager.derivedTasks.result')")
  })
})
