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
  const services = readSource('../src/api/services.js')
  const applications = readSource('../src/api/dataApplications.js')

  assert.match(client, /baseURL:\s*['"]['"]/, 'Workbench API client must not prefix canonical paths')
  assert.match(
    client,
    /extractData:\s*false/,
    'Workbench pages need response data and headers for lists, queries, and exports'
  )
  assert.match(
    services,
    /['"]\/api\/v1\/service\/consumer\/services['"]/,
    'Service Consumer APIs must keep the canonical route'
  )
  assert.match(services, /client\.post\(queryOperation\.path/)
  assert.match(applications, /['"]\/api\/v1\/workbench\/data_applications['"]/)
  assert.match(applications, /\/runtime`/)
})

test('localizes the standalone module name instead of using one language for every locale', () => {
  const zhCn = JSON.parse(readSource('../src/i18n/zh-cn.json'))
  const en = JSON.parse(readSource('../src/i18n/en.json'))

  assert.equal(zhCn.workbench.title, '工作台')
  assert.equal(zhCn.workbench.login.title, '登录工作台')
  assert.equal(en.workbench.title, 'Workbench')
  assert.equal(en.workbench.login.title, 'Sign in to Workbench')
  assert.equal(zhCn.workbench.dataApplications, '数据应用')
  assert.equal(en.workbench.dataApplications, 'Data Applications')
  assert.equal(zhCn.workbench.chartTypes.bar, '柱状图')
  assert.equal(en.workbench.chartTypes.bar, 'Bar')
  assert.equal(zhCn.workbench.renderers.value, '数值卡片')
  assert.equal(en.workbench.renderers.value, 'Value cards')
  assert.equal(zhCn.workbench.noData, '暂无数据')
  assert.equal(en.workbench.noData, 'No data')
  assert.equal(zhCn.workbench.spatialWizard.open, '从空间探索向导创建')
  assert.equal(en.workbench.spatialWizard.open, 'Create from spatial exploration wizard')
})

test('spatial exploration wizard compiles existing application concepts without a template runtime path', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')
  const wizard = readSource('../src/components/SpatialExplorationWizard.vue')
  const compiler = readSource('../src/utils/spatialExplorationDraft.mjs')

  assert.match(editor, /application\.snapshot\.components\.length > 0/)
  assert.match(wizard, /listConsumerServices\(\{ service_type: ['"]query['"]/)
  assert.match(wizard, /getConsumerDescriptor\(service\.ref\)/)
  assert.match(compiler, /buildComponentConfiguration/)
  assert.doesNotMatch(compiler, /template_type|template_id|spatial_exploration/)
})

test('data application components own service selection, rendering, parameters, and preview', () => {
  const editor = readSource('../src/components/ApplicationComponentEditor.vue')
  const runtime = readSource('../src/views/DataApplicationRuntime.vue')
  const rendererHost = readSource('../src/components/WorkbenchRendererHost.vue')
  const draft = readSource('../src/utils/componentDraft.mjs')
  assert.match(editor, /listConsumerServices\(\{ service_type: ['"]query['"]/) // catalog is capability-scoped
  assert.match(editor, /getConsumerDescriptor\(props\.component\.service_ref\)/)
  assert.match(draft, /field\?\.type === ['"]bool['"]\) return ['"]select['"]/) // boolean values preserve an unset state
  assert.match(editor, /:disabled="parameterizableFields\.length === 0"/)
  assert.match(draft, /export function createParameterDraft/)
  assert.match(editor, /executeDescriptorOperation\(operation, buildQueryRequest/)
  assert.match(editor, /:result-ready="queryCompleted"/)
  assert.match(runtime, /:result-ready="state\(placement\.component_id\)\.query_completed"/)
  assert.match(rendererHost, /rendererType === 'value' && !resultReady/)
})

test('data application editor clones persisted reactive components through their raw value', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')

  assert.match(editor, /import\s*\{[^}]*\btoRaw\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(editor, /editingComponent\.value\s*=\s*structuredClone\(toRaw\(component\)\)/)
})

test('published data applications open the canonical Console runtime directly', () => {
  const navigation = readSource('../src/utils/moduleNavigation.js')
  assert.match(navigation, /resolveConsoleRouteUrl\(`\/data-apps\/\$\{encodeURIComponent\(id\)\}`/)
  assert.match(navigation, /window\.open\(url,\s*['_"]_blank['_"],\s*['_"]noopener,noreferrer['_"]\)/)

  for (const relative of [
    '../src/views/DataApplicationList.vue',
    '../src/views/DataApplicationEditor.vue',
  ]) {
    const source = readSource(relative)
    assert.match(source, /openDataApplicationRuntime/)
    assert.doesNotMatch(source, /window\.open\(`\/data-apps\//)
  }
})

test('resolves shared map runtime peers from the Workbench dependency tree', () => {
  const packageManifest = JSON.parse(readSource('../package.json'))
  const viteConfig = readSource('../vite.config.js')

  assert.equal(packageManifest.dependencies['@amap/amap-jsapi-loader'], '1.0.1')
  assert.match(
    viteConfig,
    /['"]@amap\/amap-jsapi-loader['"]:\s*resolve\(__dirname,\s*['"]node_modules\/@amap\/amap-jsapi-loader['"]\)/,
    'common-frontend/map sources must resolve the peer from Workbench node_modules in a clean checkout'
  )
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
