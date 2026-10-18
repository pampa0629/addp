<template>
  <section v-if="inConsole && canRead" class="host-admission" data-testid="host-admission">
    <h2>{{ t('system.hostAdmission.title') }}</h2>
    <el-button @click="navigate('/host-nodes')">{{ t('system.hostNodes.title') }}</el-button>
    <input ref="fileInput" type="file" accept="application/json,.json" hidden data-testid="admission-file" @change="importFile" />
    <el-button :disabled="saving || reading || blocked || Boolean(recorded)" @click="fileInput?.click()">{{ t('system.hostAdmission.import') }}</el-button>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon data-testid="admission-error" />
    <el-alert v-if="blocked" :title="t('system.hostAdmission.hostUnknown')" type="warning" :closable="false" show-icon />
    <template v-if="manifest && !recorded">
      <h3>{{ t('system.hostAdmission.hostFacts') }}</h3>
      <dl>
        <dt>{{ t('system.hostNodes.name') }}</dt><dd>{{ manifest.host.display_name }}</dd>
        <dt>{{ t('system.hostNodes.kind') }}</dt><dd>{{ t(`system.hostNodes.kinds.${manifest.host.node_kind}`) }}</dd>
        <dt>{{ t('system.hostNodes.addresses') }}</dt><dd>{{ manifest.host.addresses.join(', ') || '—' }}</dd>
        <dt>{{ t('system.hostNodes.bindings') }}</dt><dd>{{ manifest.host.allowed_module_bindings.map(row => row.module_name).join(', ') || '—' }}</dd>
        <dt>{{ t('system.hostAdmission.collection') }}</dt><dd>{{ manifest.collection?.source.endpoint || t('system.hostAdmission.noCollection') }}</dd>
      </dl>
      <el-radio-group v-model="mode" :disabled="saving || blocked" :aria-label="t('system.hostAdmission.mode')">
        <el-radio value="existing">{{ t('system.hostAdmission.existing') }}</el-radio>
        <el-radio v-if="canCreate && !route.query.node_id" value="new">{{ t('system.hostAdmission.new') }}</el-radio>
      </el-radio-group>
      <p>{{ t(mode === 'new' ? 'system.hostAdmission.newHint' : 'system.hostAdmission.existingHint') }}</p>
      <HostNodeSelect v-if="mode === 'existing'" v-model="selectedID" :selected="selected" :disabled="saving || blocked" :allowed="canRead" :identity="identity" :api="pickerAPI" :label="t('system.hostNodes.name')" :placeholder="t('system.hostAdmission.search')" :more-label="t('system.hostAdmission.more')" :error-message="t('system.hostAdmission.readFailed')" @selected="selected = $event" />
      <el-button v-if="canUse" type="primary" :loading="saving" data-testid="admission-confirm" @click="confirmHost">{{ t('system.hostAdmission.confirm') }}</el-button>
    </template>
    <section v-if="recorded" data-testid="admission-created">
      <h3>{{ recorded.display_name }}</h3>
      <p>{{ recorded.node_id }}</p>
      <p>{{ t('system.hostAdmission.deploymentHint') }}</p>
      <el-input :model-value="`ADDP_HOST_NODE_ID=${recorded.node_id}`" readonly data-testid="admission-deployment" />
      <el-button @click="navigate(`/host-nodes/${recorded.node_id}`)">{{ t('system.hostNodes.details') }}</el-button>
      <template v-if="manifest?.collection">
        <p>{{ t('system.hostAdmission.collectionPending') }}</p>
        <el-button v-if="canCollect" type="primary" data-testid="admission-collect" @click="openCollection">{{ t('system.hostAdmission.configure') }}</el-button>
        <p v-else>{{ t('system.hostAdmission.collectionDenied') }}</p>
      </template>
    </section>
  </section>
</template>
<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import HostNodeSelect from '../../../../common-frontend/basic/src/components/HostNodeSelect.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { parseHostAdmission } from '../../../../common-frontend/basic/src/utils/hostAdmission.mjs'
import { hostNodesAPI } from '../api/hostNodes'
import { useAuthStore } from '../store/auth'
import { navigateSystemRoute } from '../utils/moduleNavigation'

const route = useRoute(), router = useRouter(), auth = useAuthStore(), { t } = useI18n()
const inConsole = window.parent !== window
if (!inConsole) void openConsoleRoute('/system/host-admission')
const canRead = computed(() => inConsole && auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('platform.host_node.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('platform.host_node.create'))
const canCollect = computed(() => canRead.value && auth.hasPermission('monitor.monitoring_target.read') && auth.hasPermission('monitor.monitoring_target.create'))
const identity = computed(() => JSON.stringify([auth.authContext?.principal, auth.authContext?.context, auth.authContext?.delegation, auth.permissions]))
const manifest = ref(null), mode = ref('existing'), selectedID = ref(''), selected = ref(null), recorded = ref(null)
const reading = ref(false), saving = ref(false), blocked = ref(false), error = ref(''), fileInput = ref(null)
const requests = createLatestRequestScope()
const pickerAPI = { nodes: hostNodesAPI.list, node: hostNodesAPI.get }
const canUse = computed(() => manifest.value && !reading.value && !blocked.value && (mode.value === 'new' ? canCreate.value && !route.query.node_id : canRead.value && Boolean(selected.value)))
const navigate = (path, query = {}, history = 'push') => navigateSystemRoute(router, { path, query }, { history })
const validID = value => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(value) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/u.test(value)
function validHost(host) {
  if (!validID(host?.node_id) || typeof host.display_name !== 'string' || !host.display_name.trim() || !['physical', 'virtual'].includes(host.node_kind) || host.enabled !== true || !Number.isSafeInteger(host.version) || host.version < 1 || !Array.isArray(host.addresses) || !Array.isArray(host.allowed_module_bindings)) throw new Error('host_mismatch')
  return host
}
async function importFile(event) {
  if (!canRead.value || reading.value || saving.value || blocked.value || recorded.value) return
  const ticket = requests.invalidate(), file = event.target.files?.[0]
  manifest.value = null; error.value = ''; reading.value = true
  try {
    if (!file || file.size > 16384) throw new Error('invalid_manifest')
    const value = parseHostAdmission(await file.text())
    if (requests.current(ticket) && canRead.value) manifest.value = value
  } catch { if (requests.current(ticket)) error.value = t('system.hostAdmission.invalid') }
  finally { event.target.value = ''; if (requests.current(ticket)) reading.value = false }
}
async function confirmHost() {
  if (!canUse.value || saving.value || recorded.value) return
  const ticket = requests.invalidate(), creating = mode.value === 'new'
  saving.value = true; error.value = ''
  try {
    const host = validHost(await requests.request(config => creating ? hostNodesAPI.create({ ...manifest.value.host, enabled: true }, config) : hostNodesAPI.get(selectedID.value, config)))
    if (!requests.current(ticket)) return
    if ((!creating && host.node_id !== selectedID.value) || host.node_kind !== manifest.value.host.node_kind || manifest.value.host.allowed_module_bindings.some(binding => !host.allowed_module_bindings.some(actual => actual.module_name === binding.module_name && actual.client_id === binding.client_id))) throw new Error('host_mismatch')
    recorded.value = host
    await navigate('/host-admission', { node_id: host.node_id }, 'replace')
  } catch (failure) {
    if (!requests.current(ticket)) return
    if (creating && (!failure.response || failure.response.status >= 500 || failure.response.status === 409)) blocked.value = true
    error.value = failure.message === 'host_mismatch' ? t('system.hostAdmission.hostMismatch') : failure.response?.data?.error || t('system.hostAdmission.readFailed')
  } finally { if (requests.current(ticket)) saving.value = false }
}
async function openCollection() {
  if (!canCollect.value || !recorded.value || !manifest.value?.collection) return
  try {
    await openConsoleRoute(`/monitor/monitoring-targets/new?node_id=${recorded.value.node_id}`, { draft: { kind: 'addp.host-collection/v1', node_id: recorded.value.node_id, source: manifest.value.collection.source } })
  } catch { error.value = t('system.hostAdmission.readFailed') }
}
watch(identity, () => {
  requests.invalidate(); manifest.value = null; selectedID.value = ''; selected.value = null; recorded.value = null
  mode.value = 'existing'; reading.value = false; saving.value = false; blocked.value = false; error.value = ''
}, { flush: 'sync' })
watch([() => route.query.node_id, identity], async ([id]) => {
  if (!canRead.value || !id || recorded.value?.node_id === id) return
  const ticket = requests.invalidate()
  try {
    if (!validID(id)) throw new Error('invalid_host')
    const host = validHost(await requests.request(config => hostNodesAPI.get(id, config)))
    if (requests.current(ticket) && host.node_id === id) { recorded.value = host; selectedID.value = id }
  } catch { if (requests.current(ticket)) error.value = t('system.hostAdmission.readFailed') }
}, { immediate: true })
onBeforeUnmount(() => requests.dispose())
</script>
<style scoped>
.host-admission { padding: 20px; color: var(--addp-text-primary); }
.host-admission > *, .host-admission section > * { margin-bottom: 12px; }
dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; }
dd { margin: 0; overflow-wrap: anywhere; }
@media (max-width: 600px) {
  dl { display: block; }
  dt { color: var(--addp-text-secondary); margin-top: 12px; }
  dd { margin-top: 4px; }
}
</style>
