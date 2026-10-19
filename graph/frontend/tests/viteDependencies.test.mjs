import { expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'

it('prebundles lazy Graph dependencies before any module navigation', async () => {
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'development' },
    fileURLToPath(new URL('../vite.config.js', import.meta.url))
  )
  const optimization = loaded.config.optimizeDeps
  const dependencies = Object.keys(JSON.parse(readFileSync(new URL('../package.json', import.meta.url))).dependencies)
  expect(optimization?.noDiscovery).toBe(true)
  expect(optimization.include).toEqual(expect.arrayContaining([...dependencies, 'element-plus/es']))
  // Auto-resolved components must use the startup stylesheet, without adding
  // new JS style entries when a lazy detail page is first opened.
  const components = loaded.config.plugins.flat().find(plugin => plugin?.name === 'unplugin-vue-components')
  expect(components).toBeDefined()
  const source = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8')
  expect(source).toContain("import 'element-plus/dist/index.css'")
})
