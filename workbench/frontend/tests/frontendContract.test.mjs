import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { extname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const readSource = (relative) => readFileSync(new URL(relative, import.meta.url), 'utf8')
const productionExtensions = new Set(['.go', '.js', '.mjs', '.vue', '.json', '.yaml', '.yml', '.toml', '.sql'])

function productionFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (['dist', 'docs', 'node_modules', 'tests'].includes(entry.name)) return []
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) return productionFiles(path)
    return productionExtensions.has(extname(entry.name)) ? [path] : []
  })
}

test('uses full Axios responses with the same-origin root for canonical API and Descriptor operation paths', () => {
  const client = readSource('../src/api/client.js')
  const views = readSource('../src/api/views.js')
  const services = readSource('../src/api/services.js')

  assert.match(client, /baseURL:\s*['"]['"]/, 'Workbench API client must not prefix canonical paths')
  assert.match(
    client,
    /extractData:\s*false/,
    'Workbench pages need response data and headers for lists, queries, and exports'
  )
  assert.match(
    views,
    /['"]\/api\/v1\/workbench\/views['"]/,
    'Workbench View APIs must keep the canonical route'
  )
  assert.match(
    services,
    /['"]\/api\/v1\/service\/consumer\/services['"]/,
    'Service Consumer APIs must keep the canonical route'
  )
  assert.match(services, /client\.post\(queryOperation\.path/)
})

test('localizes the standalone module name instead of using one language for every locale', () => {
  const zhCn = JSON.parse(readSource('../src/i18n/zh-cn.json'))
  const en = JSON.parse(readSource('../src/i18n/en.json'))

  assert.equal(zhCn.workbench.title, '工作台')
  assert.equal(zhCn.workbench.login.title, '登录工作台')
  assert.equal(en.workbench.title, 'Workbench')
  assert.equal(en.workbench.login.title, 'Sign in to Workbench')
  assert.equal(zhCn.workbench.chartTypes.bar, '柱状图')
  assert.equal(en.workbench.chartTypes.bar, 'Bar')
})

test('edit loading does not depend on enumerating the entire Service Consumer Catalog', () => {
  const editor = readSource('../src/views/ViewEditor.vue')
  assert.match(editor, /if \(isEdit\.value\) await loadExisting\(\)\s*else await loadServices\(\)/)
  assert.match(editor, /field\?\.type === ['"]bool['"]\) return ['"]select['"]/)
  assert.match(editor, /:disabled="filterableFields\.length === 0"/)
})

test('production Workbench code and configuration do not embed acceptance-domain facts', () => {
  const workbenchRoot = resolve(fileURLToPath(new URL('..', import.meta.url)), '..')
  const forbidden = [
    'farmland', 'SmID', 'SmUserID', 'SmArea', 'SmPerimete', 'SHAPE_Leng', 'SHAPE_Area',
    'commerce-order-analysis', 'active_customer', 'customer_name', 'order_count', 'outdoor'
  ]

  for (const path of productionFiles(workbenchRoot)) {
    const source = readFileSync(path, 'utf8')
    for (const fact of forbidden) {
      assert.equal(source.includes(fact), false, `${path} must not embed acceptance-domain fact ${fact}`)
    }
  }
})
