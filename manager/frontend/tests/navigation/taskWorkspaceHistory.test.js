import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const taskWorkspaceViews = [
  'CADPreviewManagement.vue',
  'GaussianSplatKSplat.vue',
  'Model3DGLB.vue',
  'PointCloudCOPC.vue',
  'RasterMosaic.vue',
  'TileCache.vue',
  'VectorMaterializedView.vue',
  'VectorTileSet.vue',
  'VectorizationTasks.vue'
]

const functionUsesPushHistory = (source, functionName) => new RegExp(
  `const ${functionName} = async \\([^)]*\\) => \\{[\\s\\S]*?history: 'push'[\\s\\S]*?\\n\\}`
).test(source)

describe('Manager task workspace history', () => {
  for (const viewName of taskWorkspaceViews) {
    it(`${viewName} pushes create and edit states into browser history`, () => {
      const source = readFileSync(
        new URL(`../../src/views/${viewName}`, import.meta.url),
        'utf8'
      )

      expect(functionUsesPushHistory(source, 'requestCreateDialog')).toBe(true)
      expect(functionUsesPushHistory(source, 'requestEditTask')).toBe(true)
    })
  }
})
