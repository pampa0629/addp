<template>
  <el-card data-testid="raster-policy" :data-state="loaded ? 'loaded' : 'unavailable'">
    <template #header><div class="policy-header"><h2>{{ t('system.rasterPolicy.title') }}</h2><el-button :loading="loading" :disabled="saving" @click="reload">{{ t('system.rasterPolicy.refresh') }}</el-button></div></template>
    <el-select v-model="engineID" :placeholder="t('system.rasterPolicy.engine')" :disabled="saving" data-testid="raster-engine">
      <el-option v-for="engine in engines" :key="engine.id" :label="engine.name" :value="engine.id" />
    </el-select>
    <el-empty v-if="!loading && !engines.length" :description="t('system.rasterPolicy.empty')" />
    <template v-if="loaded">
      <el-alert :title="t('system.rasterPolicy.shared')" type="info" :closable="false" show-icon />
      <el-form label-position="top" :disabled="!canUpdate || saving" class="policy-form">
        <template v-if="isPlatform">
          <el-form-item v-for="field in platformFields" :key="field.key" :label="t(`system.rasterPolicy.${field.key}`)">
            <el-input-number v-model="form[field.key]" :min="field.min" :max="field.key === 'default_tenant_running' ? form.running : field.key === 'default_tenant_waiting' ? form.waiting : field.max" :data-testid="field.key" />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item v-for="field in ['running','waiting']" :key="field" :label="t(`system.rasterPolicy.tenant_${field}`)">
            <el-checkbox v-model="inherit[field]" :data-testid="`inherit-${field}`">{{ t('system.rasterPolicy.inherit') }}</el-checkbox>
            <el-input-number v-if="!inherit[field]" v-model="form[field]" :min="field === 'running' ? 1 : 0" :max="Math.max(view.quota[field] ?? 0, view.policy[field])" :disabled="!canUpdate || saving" :data-testid="field" />
          </el-form-item>
          <el-alert v-if="quotaExceedsPlatform" :title="t('system.rasterPolicy.quotaExceeds')" type="warning" :closable="false" data-testid="raster-quota-exceeds" />
          <p data-testid="raster-constraints">{{ t('system.rasterPolicy.constraints', { running: view.policy.running, waiting: view.policy.waiting, cache: view.policy.cache_mib }) }}</p>
          <p>{{ t('system.rasterPolicy.effective', { running: view.effective_running, waiting: view.effective_waiting }) }}</p>
        </template>
        <el-button v-if="canUpdate" type="primary" :loading="saving" @click="save" data-testid="raster-save">{{ t('system.rasterPolicy.save') }}</el-button>
      </el-form>
      <section class="runtime-facts">
        <h3>{{ t('system.rasterPolicy.resources') }}</h3>
        <template v-if="view.runtime">
          <el-alert v-if="!view.runtime.enabled" :title="t('system.rasterPolicy.paused')" type="warning" :closable="false" />
          <p>{{ t('system.rasterPolicy.capacity', { running: view.runtime.running, waiting: view.runtime.waiting }) }}</p>
          <p>{{ t('system.rasterPolicy.applied', { saved: view.policy.version, applied: view.runtime.applied_version ?? t('system.rasterPolicy.unapplied') }) }}</p>
          <p>{{ t('system.rasterPolicy.cacheApplied', { value: view.runtime.cache_mib ?? t('system.rasterPolicy.unapplied') }) }}</p>
          <el-alert v-if="pendingRestart" :title="t('system.rasterPolicy.restart')" type="warning" :closable="false" data-testid="raster-restart" />
          <p>{{ t('system.rasterPolicy.hardware', { cpu: view.runtime.effective_cpu ?? t('system.rasterPolicy.unknown'), memory: memoryMiB, time: view.runtime.observed_at }) }}</p>
          <p>{{ t('system.rasterPolicy.adviceHint') }}</p>
          <p v-if="advice" data-testid="raster-advice-values">{{ t('system.rasterPolicy.adviceValues', { running: advice.running, waiting: advice.waiting, cache: advice.cache_mib }) }}</p>
          <p v-else>{{ t('system.rasterPolicy.adviceUnavailable') }}</p>
          <el-button v-if="isPlatform && canUpdate && advice" :disabled="saving" @click="applyAdvice" data-testid="raster-advice">{{ t('system.rasterPolicy.adoptAdvice') }}</el-button>
        </template>
        <el-alert v-else :title="t('system.rasterPolicy.resourcesUnavailable')" type="warning" :closable="false" />
      </section>
    </template>
  </el-card>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { rasterPoliciesAPI } from '../api/rasterPolicies'

const { t } = useI18n()
const auth = useAuthStore()
const scope = computed(() => auth.contextType)
const isPlatform = computed(() => scope.value === 'platform')
const canUpdate = computed(() => auth.hasPermission('system.engine_raster_policy.update'))
const engines = ref([]), engineID = ref(null), view = ref(null), loaded = ref(false), loading = ref(false), saving = ref(false)
const form = reactive({}), inherit = reactive({ running: true, waiting: true })
let requestID = 0
const platformFields = [
  { key: 'running', min: 1, max: 64 }, { key: 'waiting', min: 0, max: 1024 },
  { key: 'cache_mib', min: 16, max: 65536 }, { key: 'default_tenant_running', min: 1, max: 64 },
  { key: 'default_tenant_waiting', min: 0, max: 1024 }
]
const pendingRestart = computed(() => view.value?.runtime?.cache_mib != null && view.value.runtime.cache_mib !== view.value.policy.cache_mib)
const quotaExceedsPlatform = computed(() => !isPlatform.value && view.value?.quota && ['running', 'waiting'].some(field => view.value.quota[field] != null && view.value.quota[field] > view.value.policy[field]))
const memoryMiB = computed(() => view.value?.runtime?.memory_limit_bytes == null ? t('system.rasterPolicy.unknown') : Math.floor(view.value.runtime.memory_limit_bytes / 1048576))
const advice = computed(() => {
  const value = view.value?.runtime?.advice
  if (!value || !Number.isInteger(value.running) || value.running < 1 || value.running > 64 || !Number.isInteger(value.waiting) || value.waiting < 0 || value.waiting > 1024 || !Number.isInteger(value.cache_mib) || value.cache_mib < 16 || value.cache_mib > 65536) return null
  return value
})
function populate(value) {
  view.value = value
  if (isPlatform.value) Object.assign(form, value.policy)
  else {
    form.version = value.quota.version
    for (const key of ['running', 'waiting']) {
      inherit[key] = value.quota[key] == null
      form[key] = value.quota[key] ?? value[`effective_${key}`]
    }
  }
  loaded.value = true
}
async function load() {
  const ticket = ++requestID, id = engineID.value, context = scope.value
  loaded.value = false; view.value = null
  if (!id) { loading.value = false; return }
  loading.value = true
  try { const value = await rasterPoliciesAPI.get(context, id); if (ticket === requestID) populate(value) }
  catch { if (ticket === requestID) ElMessage.error(t('system.rasterPolicy.loadFailed')) }
  finally { if (ticket === requestID) loading.value = false }
}
async function reload() {
  const ticket = ++requestID, context = scope.value
  loaded.value = false; view.value = null; loading.value = true
  try {
    const values = await rasterPoliciesAPI.engines(context)
    if (ticket !== requestID) return
    engines.value = values
    const id = values.some(item => item.id === engineID.value) ? engineID.value : values[0]?.id ?? null
    if (id === engineID.value) await load()
    else engineID.value = id
  } catch { if (ticket === requestID) { engines.value = []; ElMessage.error(t('system.rasterPolicy.loadFailed')) } }
  finally { if (ticket === requestID) loading.value = false }
}
function applyAdvice() {
  const value = advice.value
  Object.assign(form, { running: value.running, waiting: value.waiting, cache_mib: value.cache_mib,
    default_tenant_running: Math.min(form.default_tenant_running, value.running),
    default_tenant_waiting: Math.min(form.default_tenant_waiting, value.waiting) })
}
async function save() {
  if (!loaded.value || !canUpdate.value || saving.value) return
  const ticket = requestID, context = scope.value, id = engineID.value
  const body = isPlatform.value ? Object.fromEntries(['version', ...platformFields.map(field => field.key)].map(key => [key, form[key]]))
    : { version: form.version, running: inherit.running ? null : form.running, waiting: inherit.waiting ? null : form.waiting }
  saving.value = true
  try { const value = await rasterPoliciesAPI.save(context, id, body); if (ticket === requestID) { populate(value); ElMessage.success(t('system.rasterPolicy.saved')) } }
  catch (error) { if (ticket === requestID) ElMessage.error(t(error.response?.status === 409 ? 'system.rasterPolicy.conflict' : 'system.rasterPolicy.saveFailed')) }
  finally { saving.value = false }
}
watch(engineID, load)
watch(() => JSON.stringify([auth.authContext?.principal?.id, auth.authContext?.context]), () => { engineID.value = null; engines.value = []; reload() })
onMounted(reload)
onBeforeUnmount(() => { requestID++ })
</script>

<style scoped>
.policy-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.policy-header h2 { margin: 0; }
.policy-form { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin: 20px 0; }
.policy-form p { grid-column: 1 / -1; }
.runtime-facts { border-top: 1px solid var(--addp-border-color); padding-top: 16px; }
:deep(.el-alert) { margin-top: 16px; }
</style>
