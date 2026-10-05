<template>
  <section class="pipeline" data-testid="log-pipeline">
    <div class="pipeline-heading">
      <strong>{{ t('system.module.pipeline.title') }}</strong>
      <el-tag :type="tagType" data-testid="pipeline-health">{{ t(`system.module.pipeline.health.${health}`) }}</el-tag>
      <el-button :loading="loading" @click="load">{{ t('system.module.refresh') }}</el-button>
      <template v-if="!compact">
        <el-button v-if="canUpdate" @click="editPolicy">{{ t('system.module.pipeline.policy') }}</el-button>
        <el-button v-if="canReadNotifications" @click="openNotifications">{{ t('system.module.pipeline.notifications') }}</el-button>
      </template>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <p v-if="result && result.deployment_state !== 'enabled'" data-testid="pipeline-paused">{{ t(health === 'disabled' ? 'system.module.pipeline.paused' : 'system.module.pipeline.deploymentUnconfigured') }}</p>
    <p v-if="result?.deployment_state === 'enabled' && !result.configured">{{ t('system.module.pipeline.unconfigured') }}</p>
    <p v-if="compact && result?.configured && !nodeMatches">{{ t('system.module.pipeline.nodeUnconfirmed') }}</p>
    <template v-if="result?.node && nodeMatches">
      <el-descriptions v-if="result.deployment_state === 'enabled'" :column="compact ? 2 : 3" border>
        <el-descriptions-item :label="t('system.module.pipeline.node')">{{ result.node.node }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.observed')">{{ date(result.node.received_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.delay')">{{ result.node.observation.probe_delivered ? `${result.node.observation.probe_delay_ms} ms` : '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.probe')">{{ fact(result.node.observation.probe_delivered) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.registry')">{{ fact(result.node.registry_valid) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.notifications')">{{ t(`system.module.pipeline.notificationState.${result.notifications}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.sourceUsage')">{{ result.node.observation.sources_valid ? `${result.node.observation.source_bytes} / ${result.node.observation.source_limit_bytes}` : '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.collectorCounters')">{{ result.node.observation.collector?.valid ? `${result.node.observation.collector.retries} / ${result.node.observation.collector.dropped}` : '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.pipeline.earlyCleaning')">{{ result.node.observation.housekeeping_valid ? result.node.observation.source_files_early_cleaned : '—' }}</el-descriptions-item>
      </el-descriptions>
      <p v-if="result.deployment_state === 'enabled'" class="pipeline-hint">{{ t('system.module.pipeline.coverage') }}</p>
      <el-table :data="incidents" data-testid="pipeline-incidents">
        <el-table-column :label="t('system.module.pipeline.signal')" min-width="180"><template #default="{ row }">{{ signalLabel(row.signal) }}</template></el-table-column>
        <el-table-column v-if="!compact" prop="instance_id" :label="t('system.module.instances.id')" min-width="220" show-overflow-tooltip />
        <el-table-column :label="t('system.module.pipeline.severity')" width="100"><template #default="{ row }"><el-tag :type="row.severity === 'critical' ? 'danger' : 'warning'">{{ t(`system.module.pipeline.severities.${row.severity}`) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.status')" width="130"><template #default="{ row }">{{ t(`system.module.pipeline.statuses.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.openedAt')" min-width="160"><template #default="{ row }">{{ date(row.opened_at) }}</template></el-table-column>
        <el-table-column v-if="!compact && canUpdate" :label="t('system.module.pipeline.actions')" width="190"><template #default="{ row }">
          <el-button size="small" :disabled="row.status === 'acknowledged' || busy" @click="manage(row, 'acknowledge')">{{ t('system.module.pipeline.acknowledge') }}</el-button>
          <el-button size="small" :disabled="busy" @click="manage(row, 'suppress')">{{ t('system.module.pipeline.suppress') }}</el-button>
          <small v-if="row.suppressed_until">{{ t('system.module.pipeline.suppressedUntil', { time: date(row.suppressed_until) }) }}</small>
        </template></el-table-column>
      </el-table>
    </template>
    <el-dialog v-model="policyOpen" :title="t('system.module.pipeline.policy')" width="min(600px, 95vw)" :close-on-click-modal="false">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form v-if="policyDraft" label-position="top">
        <el-form-item v-for="field in policyFields" :key="field.key" :label="t(`system.module.pipeline.fields.${field.key}`)">
          <el-input-number v-model="policyDraft[field.key]" :min="field.min" :max="field.max" :step="field.step || 1" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="editPolicy">{{ t('system.module.reload') }}</el-button><el-button :loading="busy" type="primary" @click="savePolicy">{{ t('system.module.pipeline.save') }}</el-button></template>
    </el-dialog>
    <el-drawer v-model="notificationsOpen" :title="t('system.module.pipeline.notifications')" size="min(1100px, 95vw)">
      <el-alert v-if="notificationError" :title="notificationError" type="error" :closable="false" />
      <el-button v-if="canUpdateNotifications" @click="editDestination()">{{ t('system.module.pipeline.newDestination') }}</el-button>
      <el-button @click="loadNotifications">{{ t('system.module.refresh') }}</el-button>
      <el-table :data="destinations">
        <el-table-column prop="name" :label="t('system.module.pipeline.name')" />
        <el-table-column :label="t('system.module.pipeline.channel')"><template #default="{ row }">{{ channelLabel(row.channel) }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.target')" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.channel === 'email' ? row.recipients.join(', ') : row.url }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.enabled')" width="90"><template #default="{ row }">{{ fact(row.enabled) }}</template></el-table-column>
        <el-table-column v-if="canUpdateNotifications" :label="t('system.module.pipeline.actions')" width="260"><template #default="{ row }">
          <el-button size="small" @click="editDestination(row)">{{ t('system.module.pipeline.edit') }}</el-button>
          <el-button size="small" :loading="busy" @click="testDestination(row)">{{ t('system.module.pipeline.test') }}</el-button>
          <el-button size="small" @click="deleteDestination(row)">{{ t('system.module.pipeline.delete') }}</el-button>
        </template></el-table-column>
      </el-table>
      <h3>{{ t('system.module.pipeline.deliveries') }}</h3>
      <el-table :data="deliveries.data" data-testid="pipeline-deliveries">
        <el-table-column type="expand" width="60">
          <template #default="{ row }">
            <el-descriptions :column="2" border class="delivery-diagnostics" data-testid="delivery-diagnostics">
              <el-descriptions-item :label="t('system.module.pipeline.eventID')">{{ row.event_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.targetID')">{{ row.destination_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.eventType')">{{ row.event_type }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.occurredAt')">{{ date(row.occurred_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.incidentID')">{{ row.incident_id }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.incidentStatus')">{{ t(`system.module.pipeline.statuses.${row.incident_status}`) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.nextAttemptAt')">{{ date(row.next_attempt_at) }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.module.pipeline.deliveredAt')">{{ date(row.delivered_at) }}</el-descriptions-item>
            </el-descriptions>
          </template>
        </el-table-column>
        <el-table-column prop="id" :label="t('system.module.pipeline.deliveryID')" min-width="160" show-overflow-tooltip />
        <el-table-column :label="t('system.module.pipeline.target')" min-width="160" show-overflow-tooltip><template #default="{ row }">{{ destinationLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.channel')"><template #default="{ row }">{{ channelLabel(row.channel) }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.status')"><template #default="{ row }">{{ t(`system.module.pipeline.deliveryStatuses.${row.status}`) }}</template></el-table-column>
        <el-table-column prop="attempt_count" :label="t('system.module.pipeline.attempts')" />
        <el-table-column prop="cycle_attempt_count" :label="t('system.module.pipeline.cycleAttempts')" />
        <el-table-column prop="manual_retry_count" :label="t('system.module.pipeline.manualRetries')" />
        <el-table-column :label="t('system.module.pipeline.deliveryError')" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ deliveryError(row.last_error) }}</template></el-table-column>
        <el-table-column :label="t('system.module.pipeline.createdAt')" min-width="160"><template #default="{ row }">{{ date(row.created_at) }}</template></el-table-column>
        <el-table-column v-if="canUpdateNotifications" :label="t('system.module.pipeline.actions')" width="110" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status === 'dead'" size="small" :disabled="busy || !row.destination_version || (row.suppressed_until && new Date(row.suppressed_until) > new Date())" @click="retryDelivery(row)">{{ t('system.module.pipeline.retry') }}</el-button>
        </template></el-table-column>
      </el-table>
      <el-pagination layout="total, prev, pager, next" :total="deliveries.total" :page-size="20" :current-page="deliveryPage" @current-change="changeDeliveryPage" />
    </el-drawer>
    <el-dialog v-model="destinationOpen" :title="t('system.module.pipeline.destination')" width="min(640px, 95vw)" :close-on-click-modal="false">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top">
        <el-form-item :label="t('system.module.pipeline.name')"><el-input v-model="destinationDraft.name" maxlength="100" /></el-form-item>
        <el-form-item :label="t('system.module.pipeline.channel')"><el-select v-model="destinationDraft.channel" :disabled="!!editingID"><el-option label="Webhook" value="webhook" /><el-option :label="t('system.module.pipeline.wecom')" value="wecom" /><el-option :label="t('system.module.pipeline.email')" value="email" /></el-select></el-form-item>
        <el-form-item v-if="destinationDraft.channel === 'webhook'" :label="t('system.module.pipeline.url')"><el-input v-model="destinationDraft.url" maxlength="2048" /></el-form-item>
        <el-form-item v-else-if="destinationDraft.channel === 'email'" :label="t('system.module.pipeline.recipients')"><el-input v-model="recipientsText" /></el-form-item>
        <el-form-item v-if="destinationDraft.channel !== 'email'" :label="t(destinationDraft.channel === 'wecom' ? 'system.module.pipeline.wecomCredential' : 'system.module.pipeline.secret')"><el-input v-model="secret" type="password" show-password autocomplete="new-password" /></el-form-item>
        <el-form-item :label="t('system.module.pipeline.events')"><el-checkbox-group v-model="destinationDraft.event_types"><el-checkbox v-for="event in ['opened','resolved']" :key="event" :value="event">{{ t(`system.module.pipeline.eventsLabel.${event}`) }}</el-checkbox></el-checkbox-group></el-form-item>
        <el-form-item :label="t('system.module.pipeline.enabled')"><el-switch v-model="destinationDraft.enabled" /></el-form-item>
      </el-form>
      <template #footer><el-button :loading="busy" type="primary" @click="saveDestination">{{ t('system.module.pipeline.save') }}</el-button></template>
    </el-dialog>
  </section>
</template>
<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { logPipelineAPI } from '../api/logPipeline'
const props = defineProps({ compact: Boolean, instance: { type: Object, default: null } })
const { t } = useI18n(), auth = useAuthStore()
const canUpdate = computed(() => auth.hasPermission('monitor.log_pipeline.update'))
const canReadNotifications = computed(() => auth.hasPermission('monitor.log_notification.read'))
const canUpdateNotifications = computed(() => auth.hasPermission('monitor.log_notification.update'))
const loading = ref(false), busy = ref(false), result = ref(null), error = ref(''), editError = ref('')
const policyOpen = ref(false), policyDraft = ref(null), notificationsOpen = ref(false), destinationOpen = ref(false)
const destinations = ref([]), notificationError = ref(''), deliveries = ref({ data: [], total: 0 }), deliveryPage = ref(1)
const editingID = ref(0), destinationDraft = ref({}), recipientsText = ref(''), secret = ref('')
let timer, controller, generation = 0, disposed = false
const nodeMatches = computed(() => !props.instance || (!!props.instance.host_node_name && props.instance.host_node_name === result.value?.node?.node))
const health = computed(() => {
  if (error.value || !result.value) return 'unknown'
  if (result.value.deployment_state !== 'enabled') return result.value.deployment_state
  return nodeMatches.value ? result.value.health : 'unknown'
})
const tagType = computed(() => ({ healthy: 'success', alert: 'danger', unknown: 'info', disabled: 'info', unconfigured: 'warning' })[health.value])
const incidents = computed(() => (result.value?.incidents || []).filter(row => !props.instance || !row.instance_id || row.instance_id === props.instance.instance_id))
const policyFields = [
  { key: 'failure_samples', min: 1, max: 10 }, { key: 'recovery_samples', min: 1, max: 10 },
  { key: 'stale_seconds', min: 90, max: 600 }, { key: 'delay_ms', min: 1000, max: 20000, step: 1000 },
  { key: 'capacity_percent', min: 50, max: 95 }, { key: 'recovery_percent', min: 20, max: 94 }
]
const date = v => v ? new Date(v).toLocaleString() : '—'
const channelLabel = channel => channel === 'wecom' ? t('system.module.pipeline.wecom') : channel === 'email' ? t('system.module.pipeline.email') : 'Webhook'
const destinationLabel = row => row.destination_name || t('system.module.pipeline.unavailableTarget', { id: row.destination_id })
const deliveryError = code => {
  if (!code) return '—'
  const known = ['notification_send_failed', 'notification_attempt_limit', 'wecom_rate_limited', 'wecom_rejected', 'wecom_invalid_response', 'wecom_network_failed', 'wecom_http_failed']
  return t(`system.module.pipeline.deliveryErrors.${known.includes(code) ? code : 'unknown'}`)
}
const fact = v => t(`system.module.pipeline.facts.${v ? 'yes' : 'no'}`)
const signalLabel = v => t(`system.module.pipeline.signals.${v.split(':')[0]}`)
const errorText = e => e.response?.data?.error || t('system.module.pipeline.failed')
async function load() {
  if (disposed || document.hidden) return
  controller?.abort(); controller = new AbortController(); const id = ++generation
  loading.value = true; error.value = ''
  try { const data = await logPipelineAPI.summary(controller.signal); if (id === generation) result.value = data }
  catch (e) { if (id === generation && e.code !== 'ERR_CANCELED') error.value = errorText(e) }
  finally { if (id === generation) loading.value = false }
}
async function manage(row, action) {
  busy.value = true
  try { if (action === 'acknowledge') await logPipelineAPI.acknowledge(row); else await logPipelineAPI.suppress(row, new Date(Date.now() + 3600000).toISOString()); await load() }
  catch (e) { ElMessage.error(errorText(e)) } finally { busy.value = false }
}
async function editPolicy() { try { policyDraft.value = await logPipelineAPI.policy(); editError.value = ''; policyOpen.value = true } catch (e) { ElMessage.error(errorText(e)) } }
async function savePolicy() {
  busy.value = true; editError.value = ''
  const { version, ...rest } = policyDraft.value
  const data = { version }; for (const f of policyFields) data[f.key] = rest[f.key]
  try { policyDraft.value = await logPipelineAPI.updatePolicy(data); policyOpen.value = false; await load() }
  catch (e) { editError.value = errorText(e) } finally { busy.value = false }
}
async function loadNotifications() {
  notificationError.value = ''
  try { const [targets, attempts] = await Promise.all([logPipelineAPI.destinations(), logPipelineAPI.deliveries(deliveryPage.value)]); destinations.value = targets; deliveries.value = attempts }
  catch (e) { notificationError.value = errorText(e) }
}
function openNotifications() { notificationsOpen.value = true; loadNotifications() }
function changeDeliveryPage(page) { deliveryPage.value = page; loadNotifications() }
function editDestination(row) {
  editingID.value = row?.id || 0
  destinationDraft.value = row ? { version: row.version, name: row.name, channel: row.channel, url: row.channel === 'webhook' ? row.url || '' : '', recipients: [...row.recipients], event_types: [...row.event_types], enabled: row.enabled } : { name: '', channel: 'webhook', url: '', recipients: [], event_types: ['opened','resolved'], enabled: false }
  recipientsText.value = destinationDraft.value.recipients.join(', '); secret.value = ''; editError.value = ''; destinationOpen.value = true
}
async function saveDestination() {
  busy.value = true; editError.value = ''
  try {
    const data = { ...destinationDraft.value, recipients: destinationDraft.value.channel === 'email' ? recipientsText.value.split(',').map(v => v.trim()).filter(Boolean) : [], url: destinationDraft.value.channel === 'webhook' ? destinationDraft.value.url : '' }
    // Credential-bearing channels are created disabled before the dedicated credential write.
    const enabled = data.enabled
    if (!editingID.value && data.channel !== 'email') data.enabled = false
    let row
    if (editingID.value && secret.value && data.channel !== 'email') {
      row = await logPipelineAPI.credential({ id: editingID.value, version: data.version }, secret.value)
      secret.value = ''; destinationDraft.value.version = row.version; data.version = row.version
    }
    row = editingID.value ? await logPipelineAPI.updateDestination(editingID.value, data) : await logPipelineAPI.createDestination(data)
    editingID.value = row.id; destinationDraft.value.version = row.version
    if (secret.value && row.channel !== 'email') { row = await logPipelineAPI.credential(row, secret.value); secret.value = ''; destinationDraft.value.version = row.version }
    if (enabled && !row.enabled) { row = await logPipelineAPI.updateDestination(row.id, { ...data, version: row.version, enabled }); destinationDraft.value.version = row.version }
    destinationOpen.value = false; await loadNotifications(); await load()
  } catch (e) { editError.value = errorText(e) } finally { busy.value = false }
}
async function testDestination(row) { busy.value = true; try { await logPipelineAPI.testDestination(row); ElMessage.success(t('system.module.pipeline.testSent')) } catch (e) { ElMessage.error(errorText(e)) } finally { busy.value = false } }
async function retryDelivery(row) {
  if (busy.value) return
  busy.value = true
  try {
    const history = row.incident_status === 'resolved' ? `${t('system.module.pipeline.retryHistorical')} ` : ''
    await ElMessageBox.confirm(history + t('system.module.pipeline.retryConfirm', { target: destinationLabel(row), event: row.event_type, time: date(row.occurred_at), status: t(`system.module.pipeline.statuses.${row.incident_status}`) }), t('system.module.pipeline.retry'))
    notificationError.value = ''
    await logPipelineAPI.retryDelivery(row)
    ElMessage.success(t('system.module.pipeline.requeued'))
    await loadNotifications(); await load()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') notificationError.value = errorText(e)
  } finally { busy.value = false }
}
async function deleteDestination(row) {
  try { await ElMessageBox.confirm(t('system.module.pipeline.deleteConfirm', { name: row.name }), t('system.module.pipeline.delete')); await logPipelineAPI.deleteDestination(row); await loadNotifications(); await load() }
  catch (e) { if (e !== 'cancel' && e !== 'close') ElMessage.error(errorText(e)) }
}
onMounted(() => { load(); timer = setInterval(load, 30000); document.addEventListener('visibilitychange', load) })
onUnmounted(() => { disposed = true; generation++; controller?.abort(); clearInterval(timer); document.removeEventListener('visibilitychange', load); secret.value = '' })
</script>
<style scoped>
.pipeline { margin: 16px 0; }
.pipeline-heading { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
.pipeline-hint, small { color: var(--addp-text-secondary); }
.delivery-diagnostics { margin: 12px; }
small { display: block; margin-top: 6px; }
</style>
