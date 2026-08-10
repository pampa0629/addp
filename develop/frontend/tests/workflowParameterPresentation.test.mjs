import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const sourcePath = resolve('src/utils/workflowParameterPresentation.js')
const resourceLocatorURL = pathToFileURL(resolve('../../common-frontend/basic/src/types/resourceLocator.js')).href
const inputBindingsURL = pathToFileURL(resolve('src/utils/workflowInputBindings.js')).href
const resourceBindingsURL = pathToFileURL(resolve('src/utils/workflowResourceBindings.js')).href
const source = (await readFile(sourcePath, 'utf8'))
  .replace("'@addp/common-frontend'", `'${resourceLocatorURL}'`)
  .replace("'./workflowInputBindings'", `'${inputBindingsURL}'`)
  .replace("'./workflowResourceBindings'", `'${resourceBindingsURL}'`)
const mod = await import(`data:text/javascript;charset=utf-8,${encodeURIComponent(source)}`)

const resourceParameter = {
  name: '数据源',
  display_name: '数据源',
  ui_type: 'resource_tree_picker',
  ui_config: {
    resource_binding: {
      mode: 'existing',
      locator_param: 'locator',
      geometry_column_param: 'geometry_column',
      type_param: 'source_type'
    }
  }
}
const params = {
  locator: 'addp://engine/2/path/public/railway?type=table&item_id=15',
  geometry_column: 'geom',
  source_type: 'table',
  distance: 100,
  input_gdf: { $ref: 'load_1' }
}
const enginesById = {
  2: { id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql' }
}

assert.deepEqual(
  mod.workflowResourcePresentation(resourceParameter, params, enginesById),
  {
    configured: true,
    locator: params.locator,
    engineId: 2,
    engineName: '业务 PostgreSQL',
    engineType: 'postgresql',
    resourceName: 'railway',
    path: 'public.railway',
    fullLabel: 'public.railway · 业务 PostgreSQL',
    type: 'table'
  }
)

assert.deepEqual(
  mod.workflowExternalParameterSummaries([
    { name: 'input_gdf', display_name: '输入数据', type: 'geodataframe', param_type: 'input', required: true },
    resourceParameter,
    { name: 'locator', type: 'string', param_type: 'resource' },
    { name: 'geometry_column', display_name: '几何列', type: 'string' },
    { name: 'source_type', type: 'string' },
    { name: 'distance', display_name: '缓冲距离', type: 'number', required: true }
  ], params, enginesById, { emptyLabel: '未配置' }),
  [
    {
      key: '数据源',
      label: '数据源',
      kind: 'resource',
      configured: true,
      engineName: '业务 PostgreSQL',
      resourceName: 'railway',
      path: 'public.railway',
      value: 'public.railway · 业务 PostgreSQL'
    },
    {
      key: 'distance',
      label: '缓冲距离',
      kind: 'value',
      configured: true,
      value: '100'
    }
  ]
)

assert.deepEqual(
  mod.workflowExternalParameterSummaries([
    resourceParameter,
    { name: 'distance', display_name: '缓冲距离', type: 'number', required: true }
  ], {}, enginesById, { emptyLabel: '未配置' }),
  [
    {
      key: '数据源',
      label: '数据源',
      kind: 'resource',
      configured: false,
      engineName: '',
      resourceName: '',
      path: '',
      value: '未配置'
    },
    {
      key: 'distance',
      label: '缓冲距离',
      kind: 'value',
      configured: false,
      value: '未配置'
    }
  ]
)

console.log('workflow parameter presentation tests passed')
