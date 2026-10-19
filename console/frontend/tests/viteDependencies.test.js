import { expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'

it('prepares lazy Console component dependencies before module navigation', async () => {
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'development' },
    fileURLToPath(new URL('../vite.config.js', import.meta.url))
  )
  const dependencies = Object.keys(JSON.parse(readFileSync(new URL('../package.json', import.meta.url))).dependencies)
  expect(loaded.config.optimizeDeps?.noDiscovery).toBe(true)
  expect(loaded.config.optimizeDeps.include).toEqual(expect.arrayContaining([...dependencies, 'element-plus/es']))
  expect(loaded.config.plugins.flat().some(plugin => plugin?.name === 'unplugin-vue-components')).toBe(true)
  // Lazy components keep the complete startup stylesheet.
  const main = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8')
  expect(main).toContain("import 'element-plus/dist/index.css'")
  expect(main).toContain("import 'element-plus/theme-chalk/dark/css-vars.css'")
})
