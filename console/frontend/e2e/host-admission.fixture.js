import { backend, identity, node, permissions } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'
export const createdID = '10000000-0000-4000-8000-000000000007'
export const proposal = () => ({ schema: 'addp.host-admission/v2', host: { display_name: 'Desktop VM proposal', node_kind: 'virtual', addresses: [], allowed_module_bindings: [] }, collection: { monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://127.0.0.1:19091/metrics' } } })
export const frameOf = page => page.frameLocator('iframe[data-testid=module-iframe]')
export const upload = (page, value = proposal()) => frameOf(page).getByTestId('admission-file').setInputFiles({ name: 'host-admission.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(value)) })
export async function admissionBackend(page, options = {}) {
  const hosts = options.hosts || [{ node_id: node, display_name: 'Existing VM', node_kind: 'virtual', addresses: ['node.test'], enabled: true, version: 1, allowed_module_bindings: [] }]
  const state = await backend(page, { ...options, empty: true, hosts, identity: options.identity || identity([...permissions, 'platform.host_node.create']) })
  const hostWrites = []
  let release, finished
  const held = options.holdHost ? new Promise(resolve => { release = resolve }) : null
  const done = new Promise(resolve => { finished = resolve })
  await page.route('**/api/v1/system/platform/host_nodes', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    const input = route.request().postDataJSON(); hostWrites.push(input)
    const saved = { ...input, node_id: createdID, version: 1 }
    hosts.push(saved)
    if (held) await held
    await route.fulfill({ status: options.hostStatus || 201, json: options.hostStatus ? { error: 'Host save outcome unknown' } : saved })
    finished()
  })
  return { ...state, hostWrites, release: () => release?.(), hostFinished: done }
}
