<template>
  <section class="pipeline" data-testid="log-pipeline">
    <div class="pipeline-heading">
      <strong>{{ t('system.module.pipeline.title') }}</strong>
      <el-tag :type="tagType" data-testid="pipeline-health">{{ t(`system.module.pipeline.health.${health}`) }}</el-tag>
      <el-button :loading="loading" @click="load">{{ t('system.module.refresh') }}</el-button>
      <template v-if="!compact">
        <el-button v-if="canUpdate" @click="editPolicy">{{ t('system.module.pipeline.policy') }}</el-button>
        <el-button v-if="canReadAlerts" @click="openAlerts">{{ t('system.module.pipeline.openAlerts') }}</el-button>
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
      <p v-if="!compact">{{ t('system.module.pipeline.activeIncidents', { count: result.active_incidents }) }}</p>
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
  </section>
</template>
<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { openConsoleRoute } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { logPipelineAPI } from '../api/logPipeline'
const props = defineProps({ compact: Boolean, instance: { type: Object, default: null } })
const { t } = useI18n(), auth = useAuthStore()
const canUpdate = computed(() => auth.hasPermission('monitor.log_pipeline.update'))
const canReadAlerts = computed(() => auth.hasPermission('monitor.platform_alert.read') || auth.hasPermission('monitor.platform_alert_notification.read'))
const loading = ref(false), busy = ref(false), result = ref(null), error = ref(''), editError = ref('')
const policyOpen = ref(false), policyDraft = ref(null)
let timer, controller, generation = 0, disposed = false
const nodeMatches = computed(() => !props.instance || (!!props.instance.host_node_name && props.instance.host_node_name === result.value?.node?.node))
const health = computed(() => {
  if (error.value || !result.value) return 'unknown'
  if (result.value.deployment_state !== 'enabled') return result.value.deployment_state
  return nodeMatches.value ? result.value.health : 'unknown'
})
const tagType = computed(() => ({ healthy: 'success', alert: 'danger', unknown: 'info', disabled: 'info', unconfigured: 'warning' })[health.value])
const policyFields = [
  { key: 'failure_samples', min: 1, max: 10 }, { key: 'recovery_samples', min: 1, max: 10 },
  { key: 'stale_seconds', min: 90, max: 600 }, { key: 'delay_ms', min: 1000, max: 20000, step: 1000 },
  { key: 'capacity_percent', min: 50, max: 95 }, { key: 'recovery_percent', min: 20, max: 94 }
]
const date = v => v ? new Date(v).toLocaleString() : '—'
const fact = v => t(`system.module.pipeline.facts.${v ? 'yes' : 'no'}`)
const errorText = e => e.response?.data?.error || t('system.module.pipeline.failed')
async function load() {
  if (disposed || document.hidden) return
  controller?.abort(); controller = new AbortController(); const id = ++generation
  loading.value = true; error.value = ''
  try { const data = await logPipelineAPI.summary(controller.signal); if (id === generation) result.value = data }
  catch (e) { if (id === generation && e.code !== 'ERR_CANCELED') error.value = errorText(e) }
  finally { if (id === generation) loading.value = false }
}
async function editPolicy() { try { policyDraft.value = await logPipelineAPI.policy(); editError.value = ''; policyOpen.value = true } catch (e) { ElMessage.error(errorText(e)) } }
async function savePolicy() {
  busy.value = true; editError.value = ''
  const { version, ...rest } = policyDraft.value
  const data = { version }; for (const f of policyFields) data[f.key] = rest[f.key]
  try { policyDraft.value = await logPipelineAPI.updatePolicy(data); policyOpen.value = false; await load() }
  catch (e) { editError.value = errorText(e) } finally { busy.value = false }
}
function openAlerts() {
  const query = { category: 'log_pipeline', ...(result.value?.node?.node ? { subject_id: result.value.node.node } : {}), ...(auth.hasPermission('monitor.platform_alert.read') ? {} : { tab: 'notifications' }) }
  openConsoleRoute('/monitor/platform-alerts?' + new URLSearchParams(query).toString())
}
onMounted(() => { load(); timer = setInterval(load, 30000); document.addEventListener('visibilitychange', load) })
onUnmounted(() => { disposed = true; generation++; controller?.abort(); clearInterval(timer); document.removeEventListener('visibilitychange', load) })
</script>
<style scoped>
.pipeline { margin: 16px 0; }
.pipeline-heading { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
.pipeline-hint { color: var(--addp-text-secondary); }
</style>
