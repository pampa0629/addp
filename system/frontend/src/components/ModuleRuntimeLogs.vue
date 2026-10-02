<template>
  <el-drawer :model-value="modelValue" :title="t('system.module.logs.title')" size="min(960px, 95vw)" @update:model-value="emit('update:modelValue', $event)">
    <template v-if="instance">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('system.module.columns.name')">{{ resolveIAMModuleName(instance.module_name, t, te) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.instances.id')">{{ instance.instance_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.instances.role')">{{ t(`system.module.roles.${instance.role}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.instances.node')"><ModuleInstanceNode :instance="instance" /></el-descriptions-item>
        <el-descriptions-item :label="t('system.module.instances.processStartedAt')">{{ date(instance.process_started_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('system.module.instances.offlineDeterminedAt')">{{ date(instance.stopped_at) }}</el-descriptions-item>
      </el-descriptions>
      <ModuleLogPipeline v-if="modelValue && authStore.hasPermission('monitor.log_pipeline.read')" compact :instance="instance" />
      <p v-if="instance.stop_reason === 'lease_expired'" class="log-hint">{{ t('system.module.instances.leaseExpiredHint') }}</p>
      <div class="log-controls">
        <el-select v-model="period" :aria-label="t('system.module.query.timeRange')" @change="changed">
          <el-option v-if="instance.stopped_at" value="offline" :label="t('system.module.logs.offlineWindow')" />
          <el-option v-for="item in MODULE_INSTANCE_PERIODS" :key="item.value" :value="item.value" :label="t(item.label)" />
          <el-option value="custom" :label="t('system.module.query.custom')" />
        </el-select>
        <el-select v-model="level" :empty-values="[null, undefined]" :aria-label="t('system.module.logs.level')" @change="changed">
          <el-option value="" :label="t('system.module.logs.allLevels')" />
          <el-option v-for="value in levels" :key="value" :value="value" :label="t(`system.module.logs.levels.${value}`)" />
        </el-select>
        <el-input v-model="keyword" :placeholder="t('system.module.logs.keyword')" :maxlength="128" clearable @input="schedule" @keyup.enter="changed" />
        <el-select v-model="limit" :aria-label="t('system.module.logs.limit')" @change="changed">
          <el-option v-for="n in [200, 500, 1000]" :key="n" :value="n" :label="t('system.module.logs.limitValue', { n })" />
        </el-select>
      </div>
      <el-date-picker v-if="period === 'custom'" v-model="customRange" type="datetimerange" :start-placeholder="t('system.module.query.from')" :end-placeholder="t('system.module.query.to')" @change="changed" />
      <div class="log-actions">
        <el-button @click="reset">{{ t('system.module.query.reset') }}</el-button>
        <el-button :loading="loading" @click="changed">{{ t('system.module.refresh') }}</el-button>
        <el-switch v-model="follow" :active-text="t('system.module.logs.follow')" />
      </div>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-alert v-if="error && result" :title="t('system.module.logs.stale')" type="warning" :closable="false" />
      <el-alert v-if="result?.limited" :title="t('system.module.logs.limited')" type="warning" :closable="false" />
      <el-alert v-if="result?.outside_retention" :title="t('system.module.logs.retention')" type="warning" :closable="false" />
      <p v-if="result" class="log-hint">{{ t('system.module.logs.summary', { n: result.returned, time: date(result.queried_at) }) }} · {{ t('system.module.logs.coverageUnknown') }}</p>
      <div v-loading="loading" class="log-output" data-testid="runtime-log-output">
        <el-empty v-if="result && !result.entries.length" :description="t('system.module.logs.empty')" />
        <article v-for="entry in result?.entries || []" :key="entry.entry_id" class="log-entry">
          <div class="log-entry-heading">
            <time>{{ date(entry.timestamp) }}</time>
            <el-tag :type="levelType(entry.level)" size="small">{{ t(`system.module.logs.levels.${entry.level}`) }}</el-tag>
            <span>{{ entry.channel }}</span>
            <el-button text size="small" @click="copy(entry)">{{ t('system.module.logs.copy') }}</el-button>
          </div>
          <pre>{{ entry.message }}</pre>
          <details v-if="entry.stack"><summary>{{ t('system.module.logs.stack') }}</summary><pre>{{ entry.stack }}</pre></details>
          <span v-if="entry.truncated" class="log-hint">{{ t('system.module.logs.truncated') }}</span>
        </article>
      </div>
    </template>
  </el-drawer>
</template>

<script setup>
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { modulesAPI } from '../api/modules'
import { resolveIAMModuleName } from '../utils/iamPresentation'
import { isRuntimeInstanceOnline } from '../utils/moduleRegistry'
import { MODULE_INSTANCE_PERIODS } from '../utils/routeState'
import ModuleLogPipeline from './ModuleLogPipeline.vue'
import { useAuthStore } from '../store/auth'
import ModuleInstanceNode from './ModuleInstanceNode.vue'
const props = defineProps({ modelValue: Boolean, instance: { type: Object, default: null } })
const emit = defineEmits(['update:modelValue'])
const { t, te } = useI18n()
const authStore = useAuthStore()
const levels = ['debug', 'info', 'warn', 'error', 'unknown']
const period = ref('15m'), level = ref(''), keyword = ref(''), limit = ref(200), customRange = ref(null)
const follow = ref(false), loading = ref(false), result = ref(null), error = ref('')
let timer = null, generation = 0, controller = null
const durations = { '15m': 900000, '30m': 1800000, '1h': 3600000, '6h': 21600000, '12h': 43200000, '24h': 86400000, '7d': 604800000 }
function date(value) { return value ? new Date(value).toLocaleString() : '—' }
function levelType(value) { return ({ error: 'danger', warn: 'warning', info: 'success' })[value] || 'info' }
function range() {
  const now = Date.now()
  if (period.value === 'custom') {
    if (!Array.isArray(customRange.value) || customRange.value.length !== 2) return null
    const [from, to] = customRange.value.map(v => new Date(v).getTime())
    return from < to && to - from <= durations['7d'] ? [from, to] : null
  }
  if (period.value === 'offline') {
    const stopped = new Date(props.instance.stopped_at).getTime()
    const from = stopped - durations['15m'], to = Math.min(now, stopped + 300000)
    return from < to ? [from, to] : null
  }
  return [now - durations[period.value], now]
}
function invalidate() { generation++; controller?.abort(); controller = null; clearTimeout(timer); timer = null; loading.value = false }
async function load() {
  if (!props.modelValue || !props.instance || document.hidden) return
  const window = range()
  if (!window) { error.value = t('system.module.logs.invalidRange'); return }
  const id = ++generation
  controller?.abort(); controller = new AbortController()
  loading.value = true; error.value = ''
  try {
    const data = await modulesAPI.logs(props.instance.module_name, props.instance.instance_id, {
      from: new Date(window[0]).toISOString(), to: new Date(window[1]).toISOString(), level: level.value, keyword: keyword.value, limit: limit.value
    }, controller.signal)
    if (id === generation) result.value = data
  } catch (e) {
    if (id === generation && e.code !== 'ERR_CANCELED') error.value = e.response?.data?.error || t('system.module.logs.failed')
  } finally { if (id === generation) loading.value = false }
}
function changed() { invalidate(); load() }
function schedule() { invalidate(); timer = setTimeout(() => { timer = null; load() }, 300) }
function reset() { period.value = isRuntimeInstanceOnline(props.instance) || !props.instance.stopped_at ? '15m' : 'offline'; level.value = ''; keyword.value = ''; limit.value = 200; customRange.value = null; changed() }
async function copy(entry) {
  try { await navigator.clipboard.writeText(JSON.stringify(entry, null, 2)); ElMessage.success(t('system.module.logs.copied')) }
  catch { ElMessage.error(t('system.module.logs.copyFailed')) }
}
watch(() => [props.modelValue, props.instance?.instance_id], () => { invalidate(); result.value = null; error.value = ''; follow.value = false; if (props.modelValue && props.instance) reset() }, { immediate: true })
const polling = setInterval(() => { if (follow.value && !loading.value && !timer) load() }, 3000)
function visibility() { if (!document.hidden && follow.value) changed(); else if (document.hidden) invalidate() }
document.addEventListener('visibilitychange', visibility)
onUnmounted(() => { invalidate(); clearInterval(polling); document.removeEventListener('visibilitychange', visibility) })
</script>

<style scoped>
.log-controls { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin: 20px 0 12px; }
.log-actions, .log-entry-heading { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin: 12px 0; }
.log-output { min-height: 160px; }
.log-entry { border-bottom: 1px solid var(--addp-border-color); padding: 10px 0; }
.log-entry pre { white-space: pre-wrap; overflow-wrap: anywhere; font-family: monospace; background: var(--addp-bg-secondary); padding: 12px; }
.log-hint { color: var(--addp-text-secondary); font-size: 12px; }
@media (max-width: 600px) { .log-controls { grid-template-columns: 1fr; } }
</style>
