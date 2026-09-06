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
  assert.equal(zhCn.workbench.previewApplication, '预览应用')
  assert.equal(en.workbench.previewApplication, 'Preview application')
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

test('spatial exploration wizard isolates catalog and role descriptor request sessions', () => {
  const wizard = readSource('../src/components/SpatialExplorationWizard.vue')

  assert.match(wizard, /import\s*\{[^}]*\bcomputed\b[^}]*\bonBeforeUnmount\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(wizard, /createLatestRequestCoordinator/)
  assert.match(wizard, /catalogRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(wizard, /aggregateDescriptorRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(wizard, /spatialDescriptorRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(wizard, /catalogLoading\s*=\s*ref\(false\)/)
  assert.match(wizard, /aggregateLoading\s*=\s*ref\(false\)/)
  assert.match(wizard, /spatialLoading\s*=\s*ref\(false\)/)
  assert.match(wizard, /loading\s*=\s*computed\(\(\)\s*=>\s*catalogLoading\.value\s*\|\|\s*aggregateLoading\.value\s*\|\|\s*spatialLoading\.value\)/)
  assert.match(wizard, /@close="invalidateWizardRequests"/)
  assert.match(wizard, /onBeforeUnmount\(invalidateWizardRequests\)/)

  const initializeSource = wizard.slice(wizard.indexOf('async function initialize()'), wizard.indexOf('async function selectAggregateService'))
  assert.ok(initializeSource.indexOf('invalidateWizardRequests()') < initializeSource.indexOf('catalogRequests.begin('))
  assert.match(initializeSource, /commitWizardRequest\(catalogRequests, request/)
  assert.match(initializeSource, /catch[\s\S]*commitWizardRequest\(catalogRequests, request/)
  assert.match(initializeSource, /finally[\s\S]*commitWizardRequest\(catalogRequests, request/)

  const aggregateSource = wizard.slice(wizard.indexOf('async function selectAggregateService'), wizard.indexOf('function aggregateFilterChanged'))
  assert.ok(aggregateSource.indexOf('resetAggregateSelection()') < aggregateSource.indexOf('await loadDescriptor('))
  assert.match(aggregateSource, /aggregateDescriptorRequests/)
  const aggregateResetSource = wizard.slice(wizard.indexOf('function resetAggregateSelection'), wizard.indexOf('async function selectAggregateService'))
  assert.match(aggregateResetSource, /aggregateLoading\.value = false/)
  const spatialSource = wizard.slice(wizard.indexOf('async function selectSpatialService'), wizard.indexOf('async function loadDescriptor'))
  assert.ok(spatialSource.indexOf('resetSpatialSelection()') < spatialSource.indexOf('await loadDescriptor('))
  assert.match(spatialSource, /spatialDescriptorRequests/)
  const spatialResetSource = wizard.slice(wizard.indexOf('function resetSpatialSelection'), wizard.indexOf('async function selectSpatialService'))
  assert.match(spatialResetSource, /spatialLoading\.value = false/)

  const descriptorSource = wizard.slice(wizard.indexOf('async function loadDescriptor'), wizard.indexOf('function syncValueItems'))
  assert.match(descriptorSource, /requests\.begin\(key\)/)
  assert.match(descriptorSource, /commitWizardRequest\(requests, request, currentKey\(\)/)
  assert.match(descriptorSource, /catch[\s\S]*commitWizardRequest\(requests, request, currentKey\(\)/)
  assert.match(descriptorSource, /finally[\s\S]*commitWizardRequest\(requests, request, currentKey\(\)/)
  assert.equal((spatialSource.match(/draft\.mapLabelField\s*=/g) || []).length, 1)
})

test('data application components own service selection, rendering, parameters, and preview', () => {
  const editor = readSource('../src/components/ApplicationComponentEditor.vue')
  const canvas = readSource('../src/components/DataApplicationCanvas.vue')
  const rendererHost = readSource('../src/components/WorkbenchRendererHost.vue')
  const draft = readSource('../src/utils/componentDraft.mjs')
  assert.match(editor, /listConsumerServices\(\{ service_type: ['"]query['"]/) // catalog is capability-scoped
  assert.match(editor, /getConsumerDescriptor\(sourceComponent\.service_ref\)/)
  assert.match(draft, /field\?\.type === ['"]bool['"]\) return ['"]select['"]/) // boolean values preserve an unset state
  assert.match(editor, /:disabled="parameterizableFields\.length === 0"/)
  assert.match(draft, /export function createParameterDraft/)
  assert.match(editor, /const requestBody = buildQueryRequest/)
  assert.match(editor, /executeDescriptorOperation\(operation, requestBody\)/)
  assert.match(editor, /:result-ready="queryCompleted"/)
  assert.match(canvas, /:result-ready="state\(placement\.component_id\)\.query_completed"/)
  assert.match(rendererHost, /rendererType === 'value' && !resultReady/)
  assert.match(editor, /fieldPresentations/)
  assert.match(draft, /export function buildRendererConfig/)
  assert.match(draft, /export function synchronizeFieldPresentations/)
  assert.match(rendererHost, /:presentations="config\.field_presentations \|\| \[\]"/)
})

test('component editor ignores async results from an obsolete service context', () => {
  const editor = readSource('../src/components/ApplicationComponentEditor.vue')

  assert.match(editor, /import\s*\{[^}]*\bonBeforeUnmount\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(editor, /createLatestRequestCoordinator/)
  assert.match(editor, /descriptorRequests\.begin/)
  assert.match(editor, /descriptorRequests\.isCurrent/)
  assert.match(editor, /descriptorRequests\.invalidate\(\)/)
  assert.match(editor, /operationRequests\.begin/)
  assert.match(editor, /operationRequests\.isCurrent/)
  assert.match(editor, /operationRequests\.invalidate\(\)/)
  assert.match(editor, /@close="invalidateEditorRequests"/)
  assert.match(editor, /onBeforeUnmount\(invalidateEditorRequests\)/)
})

test('component editor commits cursor pagination only after the target page succeeds', () => {
  const editor = readSource('../src/components/ApplicationComponentEditor.vue')

  assert.match(editor, /async function executeAtCursor\(cursor, nextCursorIndex = cursorIndex\.value, nextCursors = cursors\.value\)/)
  assert.match(editor, /cursorIndex\.value = nextCursorIndex/)
  assert.match(editor, /cursors\.value = nextCursors/)
  assert.match(editor, /await executeAtCursor\(nextCursor, nextIndex, nextCursors\)/)
  assert.match(editor, /await executeAtCursor\(cursors\.value\[previousIndex\], previousIndex, cursors\.value\)/)
  assert.doesNotMatch(editor, /cursorIndex\.value \+= 1/)
  assert.doesNotMatch(editor, /cursorIndex\.value -= 1/)
})

test('draft preview and published runtime reuse one Workbench application canvas', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')
  const runtime = readSource('../src/views/DataApplicationRuntime.vue')
  const canvas = readSource('../src/components/DataApplicationCanvas.vue')
  const router = readSource('../src/router/index.js')

  assert.match(editor, /<DataApplicationCanvas[^>]*:application="draftPreviewApplication"[^>]*mode="draft-preview"/)
  assert.match(editor, /buildDataApplicationPreview\(application\)/)
  assert.match(runtime, /<DataApplicationCanvas[^>]*:application="application"/)
  assert.doesNotMatch(runtime, /WorkbenchRendererHost|executeDescriptorOperation|getConsumerDescriptor/)
  assert.match(canvas, /WorkbenchRendererHost/)
  assert.match(canvas, /executeDescriptorOperation/)
  assert.match(canvas, /getConsumerDescriptor/)
  assert.equal((canvas.match(/WorkbenchRendererHost/g) || []).length >= 2, true)
  assert.doesNotMatch(router, /\/preview/)
})

test('published runtime queries once after descriptors and schedules later wallboard refreshes', () => {
  const canvas = readSource('../src/components/DataApplicationCanvas.vue')
  const mounted = canvas.slice(canvas.indexOf('onMounted(async () => {'), canvas.indexOf('\nonBeforeUnmount(', canvas.indexOf('onMounted(async () => {')))
  const publishedBranch = mounted.slice(mounted.indexOf("if (props.mode === 'published')"), mounted.indexOf('} else {'))
  const draftBranch = mounted.slice(mounted.indexOf('} else {'))

  assert.match(canvas, /canRunPublishedApplicationInitialQuery/)
  assert.match(mounted, /await loadDescriptors\(\)/)
  assert.match(publishedBranch, /canRunPublishedApplicationInitialQuery\([\s\S]*await queryAll\(\)[\s\S]*scheduleAutomaticRefresh\(\)/)
  assert.doesNotMatch(publishedBranch, /refreshAndSchedule/)
  assert.match(draftBranch, /await refreshAndSchedule\(\)/)
})

test('application runtime retries transient failures without bypassing contract drift', () => {
  const canvas = readSource('../src/components/DataApplicationCanvas.vue')
  const runtime = readSource('../src/utils/dataApplicationRuntime.mjs')

  assert.match(canvas, /current\.descriptor_error\s*=/)
  assert.match(canvas, /contract_error: t\('workbench\.runtimeContractChanged'\)/)
  assert.match(canvas, /contract_error: ''/)
  assert.match(canvas, /current\.query_error\s*=/)
  assert.match(canvas, /descriptorRequests: createLatestRequestCoordinator\(\)/)
  assert.match(canvas, /current\.descriptorRequests\.begin\(item\.id\)/)
  assert.match(canvas, /commitLatestComponentDescriptorState/)
  assert.match(runtime, /current\.descriptorRequests\.isCurrent\(request, componentID\)/)
  assert.match(canvas, /!componentStates\[item\.id\]\?\.descriptor && !componentStates\[item\.id\]\?\.contract_error/) // query-all reloads a temporarily unavailable descriptor
  assert.match(canvas, /canExecuteComponentQuery\(componentStates\[item\.id\]\)/)
  assert.doesNotMatch(canvas, /Boolean\(state\(placement\.component_id\)\.error\)/)
})

test('application parameter changes invalidate only their bound component requests', () => {
  const canvas = readSource('../src/components/DataApplicationCanvas.vue')
  const runtime = readSource('../src/utils/dataApplicationRuntime.mjs')
  const boundedExport = readSource('../src/utils/boundedExport.mjs')

  assert.match(canvas, /@update:model-value="updateParameterValue\(parameter\.key, \$event\)"/)
  assert.match(runtime, /componentIDsForApplicationParameters/)
  assert.match(runtime, /current\.requests\.invalidate\(\)/)
  assert.match(canvas, /queryAllRequests\.invalidate\(\)/)
  assert.match(canvas, /invalidateApplicationParameterResults/)
  assert.match(canvas, /downloadCurrentBoundedExport/)
  assert.match(boundedExport, /if \(!isCurrent\(\)\) return 'stale'/)
})

test('published runtime reloads the requested application and exposes an in-page retry', () => {
  const runtime = readSource('../src/views/DataApplicationRuntime.vue')
  const runtimeState = readSource('../src/utils/dataApplicationRuntime.mjs')
  const zhCn = JSON.parse(readSource('../src/i18n/zh-cn.json'))
  const en = JSON.parse(readSource('../src/i18n/en.json'))

  assert.match(runtime, /data-testid="runtime-retry-action"/)
  assert.match(runtime, /import\s*\{[^}]*\bwatch\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(runtime, /import\s*\{[^}]*\bonBeforeUnmount\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(runtime, /createLatestRequestCoordinator/)
  assert.match(runtime, /commitLatestDataApplicationLoad/)
  assert.match(runtimeState, /requests\.isCurrent\(request, targetID\)/)
  assert.match(runtime, /watch\(\(\)\s*=>\s*route\.params\.id/)
  assert.match(runtime, /onBeforeUnmount\(\(\)\s*=>\s*requests\.invalidate\(\)\)/)
  assert.doesNotMatch(runtime, /onMounted\(load\)/)
  assert.equal(zhCn.workbench.retry, '重试')
  assert.equal(en.workbench.retry, 'Retry')
})

test('data application editor clones persisted reactive components through their raw value', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')

  assert.match(editor, /import\s*\{[^}]*\btoRaw\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(editor, /editingComponent\.value\s*=\s*structuredClone\(toRaw\(component\)\)/)
})

test('data application editor isolates reused route load and descriptor contexts', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')
  const draft = readSource('../src/utils/dataApplicationDraft.mjs')

  assert.match(editor, /import\s*\{[^}]*\bonBeforeUnmount\b[^}]*\bwatch\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(editor, /createLatestRequestCoordinator/)
  assert.match(editor, /commitLatestDataApplicationRequest/)
  assert.match(draft, /dataApplicationEditorRouteContext/)
  assert.match(draft, /requests\.isCurrent\(request, currentContext\)/)
  assert.match(editor, /watch\(\(\)\s*=>\s*\[route\.name, route\.params\.id\]/)
  assert.match(editor, /onBeforeUnmount\(invalidateEditorContextRequests\)/)
  assert.match(editor, /loadComponentDescriptor\(component, loadRequest/)
  assert.doesNotMatch(editor, /onMounted\(load\)/)
})

test('data application mutations commit only inside their current editor route', () => {
  const editor = readSource('../src/views/DataApplicationEditor.vue')
  const draft = readSource('../src/utils/dataApplicationDraft.mjs')

  assert.match(editor, /v-loading="loading \|\| saving \|\| publishing \|\| offlining"/)
  assert.match(editor, /editorMutationRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(editor, /editorMutationRequests\.invalidate\(\)/)
  assert.match(draft, /dataApplicationEditorMutationContext/)
  assert.match(draft, /commitLatestDataApplicationRequest/)
  assert.doesNotMatch(draft, /commitLatestDataApplicationEditorRequest|commitLatestDataApplicationEditorLoad/)

  for (const [action, apiCall] of [
    ['save', 'createDataApplication'],
    ['publish', 'publishDataApplication'],
    ['offline', 'offlineDataApplication'],
  ]) {
    const start = editor.indexOf(`async function ${action}()`)
    const end = editor.indexOf('\nasync function ', start + 1)
    const source = editor.slice(start, end < 0 ? undefined : end)
    assert.match(source, new RegExp(`const action = ['"]${action}['"][\\s\\S]*beginEditorMutation\\(action\\)`))
    assert.ok(source.indexOf('commitEditorMutation(') >= 0)
    assert.ok(source.indexOf('commitEditorMutation(') < source.indexOf(apiCall))
    assert.match(source, /catch[\s\S]*commitEditorMutation\(/)
    assert.match(source, /finally[\s\S]*commitEditorMutation\(/)
  }

  const publishSource = editor.slice(editor.indexOf('async function publish()'), editor.indexOf('async function offline()'))
  const offlineSource = editor.slice(editor.indexOf('async function offline()'), editor.indexOf('function openRuntime()'))
  assert.ok(publishSource.indexOf('beginEditorMutation(action)') < publishSource.indexOf('confirmDataApplicationAction'))
  assert.ok(offlineSource.indexOf('beginEditorMutation(action)') < offlineSource.indexOf('confirmDataApplicationAction'))

  const removeComponentSource = editor.slice(editor.indexOf('async function removeComponent('), editor.indexOf('function pruneUnusedApplicationParameters()'))
  assert.match(removeComponentSource, /const action = `remove-component:\$\{component\.id\}`/)
  assert.ok(removeComponentSource.indexOf('beginEditorMutation(action)') < removeComponentSource.indexOf('confirmDataApplicationAction'))
  assert.ok(removeComponentSource.indexOf('confirmDataApplicationAction') < removeComponentSource.indexOf('commitEditorMutation('))
  assert.match(removeComponentSource, /commitEditorMutation\(request, action, \(\) => \{[\s\S]*application\.snapshot\.components/)
  assert.doesNotMatch(removeComponentSource, /await ElMessageBox\.confirm/)
})

test('data application list isolates pagination and deletion lifecycle contexts', () => {
  const list = readSource('../src/views/DataApplicationList.vue')
  const draft = readSource('../src/utils/dataApplicationDraft.mjs')
  const zhCn = JSON.parse(readSource('../src/i18n/zh-cn.json'))
  const en = JSON.parse(readSource('../src/i18n/en.json'))

  assert.match(list, /import\s*\{[^}]*\bonBeforeUnmount\b[^}]*\bonMounted\b[^}]*\}\s*from\s*['"]vue['"]/s)
  assert.match(list, /listLoadRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(list, /listDeletionRequests\s*=\s*createLatestRequestCoordinator\(\)/)
  assert.match(list, /v-loading="loading \|\| Boolean\(deletingID\)"/)
  assert.match(draft, /dataApplicationListPageContext/)
  assert.match(draft, /dataApplicationDeletionContext/)
  assert.match(draft, /commitLatestDataApplicationRequest/)

  const loadSource = list.slice(list.indexOf('async function load()'), list.indexOf('async function remove('))
  assert.match(loadSource, /listLoadRequests\.begin\(targetContext\)/)
  assert.match(loadSource, /commitListLoad\(request/)
  assert.match(loadSource, /catch[\s\S]*commitListLoad\(request/)
  assert.match(loadSource, /finally[\s\S]*commitListLoad\(request/)

  const removeSource = list.slice(list.indexOf('async function remove('), list.indexOf('function openRuntime('))
  assert.ok(removeSource.indexOf('listDeletionRequests.begin(targetContext)') < removeSource.indexOf('confirmDataApplicationAction'))
  assert.ok(removeSource.indexOf('confirmDataApplicationAction') < removeSource.indexOf('deleteDataApplication'))
  assert.match(removeSource, /currentDeletionContext\(row\.id\) !== targetContext/)
  assert.match(removeSource, /commitListDeletion\(request, targetContext/)
  assert.match(removeSource, /items\.value\.length === 1 && page\.value > 1/)
  assert.match(removeSource, /catch[\s\S]*commitListDeletion\(request, targetContext/)
  assert.match(removeSource, /finally[\s\S]*commitListDeletion\(request, targetContext/)
  assert.doesNotMatch(removeSource, /await ElMessageBox\.confirm/)
  assert.match(list, /onBeforeUnmount\(invalidateListRequests\)/)
  assert.match(list, /listLoadRequests\.invalidate\(\)/)
  assert.match(list, /listDeletionRequests\.invalidate\(\)/)
  assert.equal(zhCn.workbench.deleteFailed, '删除失败')
  assert.equal(en.workbench.deleteFailed, 'Delete failed')
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

test('keeps optional renderer dependencies out of the Workbench entry chunk', () => {
  const main = readSource('../src/main.js')
  const rendererHost = readSource('../src/components/WorkbenchRendererHost.vue')
  const viteConfig = readSource('../vite.config.js')
  const packageManifest = JSON.parse(readSource('../package.json'))

  assert.doesNotMatch(main, /import\s+ElementPlus\s+from\s+['"]element-plus['"]/)
  assert.doesNotMatch(main, /\.use\(ElementPlus\)/)
  assert.doesNotMatch(main, /import\s+['"]ol\/ol\.css['"]/)
  assert.match(main, /@common-ui-map\/i18n\/zh-cn\.json/)
  assert.match(main, /@common-ui-map\/i18n\/en\.json/)
  assert.match(rendererHost, /await\s+import\(['"]ol\/ol\.css['"]\)/)
  assert.equal(packageManifest.devDependencies['unplugin-vue-components'], '0.28.0')
  assert.match(viteConfig, /unplugin-vue-components\/vite/)
  assert.match(viteConfig, /ElementPlusResolver/)
  assert.match(viteConfig, /ENTRY_CHUNK_LIMIT_BYTES\s*=\s*500\s*\*\s*1024/)
  assert.match(viteConfig, /enforceEntryChunkBudget\(\)/)
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
