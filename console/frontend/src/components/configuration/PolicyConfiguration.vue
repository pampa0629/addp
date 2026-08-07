<template>
  <section class="policy-configuration">
    <header class="policy-header"><div><h2>{{ t(`console.configuration.policies.${owner}.title`) }}</h2><p>{{ contextLabel }}</p></div><el-button :icon="Refresh" circle :loading="loading" @click="load" /></header>
    <el-form v-loading="loading" :model="form" label-position="top" class="policy-form">
      <template v-if="owner === 'develop'"><el-form-item :label="t('console.configuration.policies.develop.defaultQueryTimeout')"><el-input-number v-model="form.default_query_timeout" :min="1" :max="3600" controls-position="right" /></el-form-item><el-form-item v-if="isPlatform" :label="t('console.configuration.policies.develop.maxQueryTimeout')"><el-input-number v-model="form.max_query_timeout" :min="1" :max="86400" controls-position="right" /></el-form-item><el-form-item v-if="isPlatform" :label="t('console.configuration.policies.develop.queryResultLimit')"><el-input-number v-model="form.query_result_limit" :min="1" :max="100000" controls-position="right" /></el-form-item></template>
      <template v-else-if="owner === 'manager'"><el-form-item :label="t('console.configuration.policies.manager.flatgeobufRows')"><el-input-number v-model="form.direct_flatgeobuf_max_rows" :min="1" :max="1000000" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.manager.mvtTimeout')"><el-input-number v-model="form.realtime_tile_timeout_ms" :min="100" :max="120000" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.manager.retryAfter')"><el-input-number v-model="form.realtime_tile_retry_after_sec" :min="1" :max="3600" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.manager.rasterMosaicTimeout')"><el-input-number v-model="form.raster_mosaic_generation_timeout_seconds" :min="60" :max="604800" controls-position="right" /></el-form-item></template>
      <template v-else-if="owner === 'transfer'"><el-form-item v-for="field in TRANSFER_FIELDS" :key="field.key" :label="t(`console.configuration.policies.transfer.${field.label}`)"><el-input-number v-model="form[field.key]" :min="field.min" :max="field.max" controls-position="right" /></el-form-item></template>
      <template v-else-if="owner === 'monitor'"><el-form-item v-for="field in MONITOR_FIELDS" :key="field.key" :label="t(`console.configuration.policies.monitor.${field.label}`)"><el-input-number v-model="form[field.key]" :min="field.min" :max="field.max" controls-position="right" /></el-form-item></template>
      <template v-else-if="owner === 'service'"><el-form-item :label="t('console.configuration.policies.service.healthCheckCron')"><el-input v-model="form.health_check_cron" /></el-form-item><el-form-item :label="t('console.configuration.policies.service.metadataRefreshCron')"><el-input v-model="form.metadata_refresh_cron" /></el-form-item></template>
      <template v-else><el-form-item :label="t('console.configuration.policies.copilot.scoreThreshold')"><el-input-number v-model="form.score_threshold" :min="0.01" :max="1" :step="0.01" :precision="2" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.copilot.maxCandidates')"><el-input-number v-model="form.max_candidates" :min="1" :max="100" controls-position="right" /></el-form-item></template>
      <footer class="form-actions"><el-button type="primary" :icon="Check" :loading="saving" :disabled="!canUpdate" @click="save">{{ t('console.configuration.save') }}</el-button></footer>
    </el-form>
  </section>
</template>
<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../store/auth'
import { getPolicyConfiguration, updatePolicyConfiguration } from '../../api/policyConfiguration'
const props = defineProps({ owner: { type: String, required: true } }); const owner = props.owner
const { t } = useI18n(); const authStore = useAuthStore(); const loading = ref(false); const saving = ref(false)
const form = reactive({ version: 0, default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, direct_flatgeobuf_max_rows: 2000, realtime_tile_timeout_ms: 2500, realtime_tile_retry_after_sec: 60, raster_mosaic_generation_timeout_seconds: 7200, score_threshold: 0.15, max_candidates: 10, alert_evaluation_interval_seconds: 15, webhook_dispatch_interval_seconds: 2, webhook_http_timeout_seconds: 10, webhook_lease_duration_seconds: 30, webhook_max_attempts: 8, webhook_retry_initial_backoff_seconds: 5, webhook_retry_max_backoff_seconds: 300, email_dispatch_interval_seconds: 2, email_smtp_timeout_seconds: 15, email_lease_duration_seconds: 30, email_max_attempts: 8, email_retry_initial_backoff_seconds: 5, email_retry_max_backoff_seconds: 300, diagnostics_interval_seconds: 15, retention_degraded_horizon_seconds: 21600, retention_critical_horizon_seconds: 3600, checkpoint_stale_after_seconds: 300, recovery_initial_backoff_seconds: 1, recovery_max_backoff_seconds: 60, recovery_max_failures: 5, recovery_circuit_open_seconds: 300, recovery_stability_window_seconds: 300 })
const TRANSFER_FIELDS = [
  { key: 'diagnostics_interval_seconds', label: 'diagnosticsInterval', min: 1, max: 86400 },
  { key: 'retention_degraded_horizon_seconds', label: 'retentionDegraded', min: 1, max: 31536000 },
  { key: 'retention_critical_horizon_seconds', label: 'retentionCritical', min: 1, max: 31536000 },
  { key: 'checkpoint_stale_after_seconds', label: 'checkpointStale', min: 1, max: 31536000 },
  { key: 'recovery_initial_backoff_seconds', label: 'recoveryInitial', min: 1, max: 86400 },
  { key: 'recovery_max_backoff_seconds', label: 'recoveryMax', min: 1, max: 31536000 },
  { key: 'recovery_max_failures', label: 'maxFailures', min: 1, max: 1000 },
  { key: 'recovery_circuit_open_seconds', label: 'circuitOpen', min: 1, max: 31536000 },
  { key: 'recovery_stability_window_seconds', label: 'stabilityWindow', min: 1, max: 31536000 }
]
const MONITOR_FIELDS = [
  { key: 'alert_evaluation_interval_seconds', label: 'alertInterval', min: 1, max: 86400 },
  { key: 'webhook_dispatch_interval_seconds', label: 'webhookInterval', min: 1, max: 86400 },
  { key: 'webhook_http_timeout_seconds', label: 'webhookTimeout', min: 1, max: 86400 },
  { key: 'webhook_lease_duration_seconds', label: 'webhookLease', min: 1, max: 86400 },
  { key: 'webhook_max_attempts', label: 'webhookAttempts', min: 1, max: 1000 },
  { key: 'webhook_retry_initial_backoff_seconds', label: 'webhookRetryInitial', min: 1, max: 31536000 },
  { key: 'webhook_retry_max_backoff_seconds', label: 'webhookRetryMax', min: 1, max: 31536000 },
  { key: 'email_dispatch_interval_seconds', label: 'emailInterval', min: 1, max: 86400 },
  { key: 'email_smtp_timeout_seconds', label: 'emailTimeout', min: 1, max: 86400 },
  { key: 'email_lease_duration_seconds', label: 'emailLease', min: 1, max: 86400 },
  { key: 'email_max_attempts', label: 'emailAttempts', min: 1, max: 1000 },
  { key: 'email_retry_initial_backoff_seconds', label: 'emailRetryInitial', min: 1, max: 31536000 },
  { key: 'email_retry_max_backoff_seconds', label: 'emailRetryMax', min: 1, max: 31536000 }
]
const isPlatform = computed(() => authStore.contextType === 'platform'); const contextLabel = computed(() => isPlatform.value ? t('console.configuration.platformContext') : t('console.configuration.tenantContext')); const canUpdate = computed(() => authStore.hasPermission(`${owner}.configuration.update`))
async function load() { loading.value = true; try { Object.assign(form, await getPolicyConfiguration(owner)) } catch (error) { ElMessage.error(error?.response?.data?.error || t('console.configuration.loadFailed')) } finally { loading.value = false } }
async function save() { saving.value = true; try { await updatePolicyConfiguration(owner, { ...form }); ElMessage.success(t('console.configuration.saveSuccess')); await load() } catch (error) { if (error?.response?.status === 409) { ElMessage.warning(t('console.configuration.versionConflict')); await load() } else ElMessage.error(error?.response?.data?.error || t('console.configuration.saveFailed')) } finally { saving.value = false } }
onMounted(load)
</script>
<style scoped>.policy-configuration{width:100%}.policy-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.policy-header h2{margin:0 0 6px;color:var(--addp-text-primary);font-size:20px;font-weight:600;letter-spacing:0}.policy-header p{margin:0;color:var(--addp-text-secondary);font-size:14px}.policy-form{max-width:560px}.form-actions{display:flex;justify-content:flex-end;margin-top:12px}</style>
