<template>
  <section class="policy-configuration">
    <header class="policy-header"><div><h2>{{ t(`console.configuration.policies.${owner}.title`) }}</h2><p>{{ contextLabel }}</p></div><el-button :icon="Refresh" circle :loading="loading" @click="load" /></header>
    <el-form v-loading="loading" :model="form" label-position="top" class="policy-form">
      <template v-if="owner === 'develop'"><el-form-item :label="t('console.configuration.policies.develop.defaultQueryTimeout')"><el-input-number v-model="form.default_query_timeout" :min="1" :max="3600" controls-position="right" /></el-form-item><el-form-item v-if="isPlatform" :label="t('console.configuration.policies.develop.maxQueryTimeout')"><el-input-number v-model="form.max_query_timeout" :min="1" :max="86400" controls-position="right" /></el-form-item><el-form-item v-if="isPlatform" :label="t('console.configuration.policies.develop.queryResultLimit')"><el-input-number v-model="form.query_result_limit" :min="1" :max="100000" controls-position="right" /></el-form-item></template>
      <template v-else-if="owner === 'manager'"><el-form-item :label="t('console.configuration.policies.manager.flatgeobufRows')"><el-input-number v-model="form.direct_flatgeobuf_max_rows" :min="1" :max="1000000" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.manager.mvtTimeout')"><el-input-number v-model="form.realtime_tile_timeout_ms" :min="100" :max="120000" controls-position="right" /></el-form-item><el-form-item :label="t('console.configuration.policies.manager.retryAfter')"><el-input-number v-model="form.realtime_tile_retry_after_sec" :min="1" :max="3600" controls-position="right" /></el-form-item></template>
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
const form = reactive({ version: 0, default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, direct_flatgeobuf_max_rows: 2000, realtime_tile_timeout_ms: 2500, realtime_tile_retry_after_sec: 60, score_threshold: 0.15, max_candidates: 10 })
const isPlatform = computed(() => authStore.contextType === 'platform'); const contextLabel = computed(() => isPlatform.value ? t('console.configuration.platformContext') : t('console.configuration.tenantContext')); const canUpdate = computed(() => authStore.hasPermission(`${owner}.configuration.update`))
async function load() { loading.value = true; try { Object.assign(form, await getPolicyConfiguration(owner)) } catch (error) { ElMessage.error(error?.response?.data?.error || t('console.configuration.loadFailed')) } finally { loading.value = false } }
async function save() { saving.value = true; try { await updatePolicyConfiguration(owner, { ...form }); ElMessage.success(t('console.configuration.saveSuccess')); await load() } catch (error) { if (error?.response?.status === 409) { ElMessage.warning(t('console.configuration.versionConflict')); await load() } else ElMessage.error(error?.response?.data?.error || t('console.configuration.saveFailed')) } finally { saving.value = false } }
onMounted(load)
</script>
<style scoped>.policy-configuration{width:100%}.policy-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.policy-header h2{margin:0 0 6px;color:var(--addp-text-primary);font-size:20px;font-weight:600;letter-spacing:0}.policy-header p{margin:0;color:var(--addp-text-secondary);font-size:14px}.policy-form{max-width:560px}.form-actions{display:flex;justify-content:flex-end;margin-top:12px}</style>
