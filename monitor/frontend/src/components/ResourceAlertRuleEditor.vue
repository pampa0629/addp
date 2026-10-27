<template>
    <el-dialog class="addp-dialog" v-model="open" :title="t(editing ? 'monitor.resourceRules.edit' : 'monitor.resourceRules.create')" width="min(620px, 95vw)" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
      <el-alert v-if="editError" :title="editError" type="error" :closable="false" />
      <el-form label-position="top" :disabled="busy" @submit.prevent="save">
        <el-form-item :label="t('monitor.platformAlerts.name')"><el-input v-model="draft.name" maxlength="100" data-testid="rule-name" /></el-form-item>
        <el-form-item :label="t('monitor.targets.node')">
          <el-input v-if="editing || fixedMount" data-testid="resource-rule-fixed-host" :model-value="selected?.display_name || draft.node_id" readonly />
          <HostNodeSelect v-else v-model="draft.node_id" :selected="selected" :allowed="canReadHosts" :identity="JSON.stringify([auth.authContext, auth.permissions])" :api="nodeResourcesAPI" :label="t('monitor.targets.node')" :placeholder="t('monitor.targets.nodeSearch')" :more-label="t('monitor.targets.moreNodes')" :error-message="t('monitor.targets.nodeLoadingFailed')" @selected="selected = $event" />
        </el-form-item>
        <el-form-item :label="t('monitor.resourceRules.metric')"><el-select data-testid="resource-rule-metric" v-model="draft.metric_key" :disabled="!!editing || !!fixedMount" @change="applyPreset" :aria-label="t('monitor.resourceRules.metric')"><el-option v-for="preset in presets" :key="preset.metric_key" :value="preset.metric_key" :label="metricLabel(preset.metric_key)" :disabled="preset.metric_key === filesystemMetric && !canReadResources" /></el-select></el-form-item>
        <el-form-item v-if="draft.metric_key === filesystemMetric" :label="t('monitor.resourceRules.mount')">
          <el-alert v-if="mountError" :title="t(mountError)" type="error" :closable="false" data-testid="filesystem-rule-mount-error" />
          <el-input v-if="editing || fixedMount" data-testid="filesystem-rule-fixed-mount" :model-value="mountLabel(draft.dimensions)" readonly />
          <template v-else>
            <el-select :model-value="resourceDimensionID(draft.dimensions || {})" :loading="mountLoading" :disabled="!draft.node_id || mountLoading || !canReadResources" :aria-label="t('monitor.resourceRules.mount')" data-testid="filesystem-rule-mount" @change="value => draft.dimensions = mounts.find(row => resourceDimensionID(row.dimensions) === value)?.dimensions || {}">
              <el-option v-for="row in mounts" :key="row.key" :value="resourceDimensionID(row.dimensions)" :label="mountLabel(row.dimensions)" />
            </el-select>
            <small v-if="!mountLoading && draft.node_id && !mounts.length && !mountError">{{ t('monitor.resourceRules.noMounts') }}</small>
          </template>
          <small>{{ t('monitor.resourceRules.filesystemHint') }}</small>
        </el-form-item>
        <el-form-item :label="t('monitor.resourceRules.trigger')"><div class="rule-condition"><el-input-number v-model="draft.trigger_percent" :min="0" :max="100" :aria-label="t('monitor.resourceRules.triggerPercent')" /><span>%</span><el-input-number v-model="draft.trigger_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.triggerSeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div><small>{{ t('monitor.resourceRules.triggerHint') }}</small></el-form-item>
        <el-form-item :label="t('monitor.resourceRules.recovery')"><div class="rule-condition"><el-input-number v-model="draft.recovery_percent" :min="0" :max="100" :aria-label="t('monitor.resourceRules.recoveryPercent')" /><span>%</span><el-input-number v-model="draft.recovery_seconds" :min="15" :max="300" :step="15" step-strictly :aria-label="t('monitor.resourceRules.recoverySeconds')" /><span>{{ t('monitor.resourceRules.seconds') }}</span></div><small>{{ t('monitor.resourceRules.recoveryHint') }}</small></el-form-item>
        <el-form-item :label="t('monitor.platformAlerts.enabled')"><el-switch v-model="draft.enabled" :disabled="!editing" :aria-label="t('monitor.platformAlerts.enabled')" /><small v-if="!editing">{{ t('monitor.resourceRules.disabledHint') }}</small></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="busy" @click="open = false">{{ t('common.close') }}</el-button><el-button type="primary" :loading="busy" :disabled="!selected || (fixedMount && !fixedMountReady)" @click="save">{{ t('monitor.platformAlerts.save') }}</el-button></template>
    </el-dialog>
</template>
<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { filesystemAlertMetric as filesystemMetric, resourceMountLabel as mountLabel, resourceRuleMetricLabelKey } from '../utils/platformAlerts'
import { platformAlertsAPI as api } from '../api/platformAlerts'
import { nodeResourcesAPI } from '../api/nodeResources'
import HostNodeSelect from '../../../../common-frontend/basic/src/components/HostNodeSelect.vue'
import { createLatestRequestScope } from '../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { resourceDimensionID, resourceDimensionRows, validateResourceResponse, validateCollection, validResourceDimensions, currentResourceValue, resourceErrorKey } from '../utils/nodeResources'
const props = defineProps({ presets: { type: Array, default: null } })
const emit = defineEmits(['saved'])
const auth = useAuthStore(), { t } = useI18n()
const canRead = computed(() => auth.contextType === 'platform' && auth.authContext?.principal?.type === 'user' && !auth.authContext?.delegation && auth.hasPermission('monitor.platform_resource_rule.read'))
const canCreate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.create'))
const canUpdate = computed(() => canRead.value && auth.hasPermission('monitor.platform_resource_rule.update'))
const canReadHosts = computed(() => canRead.value && auth.hasPermission('platform.host_node.read'))
const canReadResources = computed(() => canReadHosts.value && auth.hasPermission('monitor.resource_observation.read'))

const open = ref(false), editing = ref(''), draft = ref({}), selected = ref(null), editError = ref(''), busy = ref(false)
const presets = ref([]), fixedMount = ref(null), mounts = ref([]), mountLoading = ref(false), mountError = ref('')
const requests = createLatestRequestScope(), mountRequests = createLatestRequestScope()
const fixedMountReady = computed(() => !mountLoading.value && mounts.value.some(row => resourceDimensionID(row.dimensions) === resourceDimensionID(fixedMount.value || {})))
const metricLabel = value => t(resourceRuleMetricLabelKey(value))
const errorText = e => e.response?.data?.error || t('monitor.platformAlerts.failed')
function close() {
  requests.invalidate(); mountRequests.invalidate()
  open.value = false; busy.value = false; selected.value = null; draft.value = {}; editing.value = ''; editError.value = ''
  fixedMount.value = null; mounts.value = []; mountLoading.value = false; mountError.value = ''; presets.value = []
}
function applyPreset() { const preset = presets.value.find(item => item.metric_key === draft.value.metric_key); if (preset) Object.assign(draft.value, preset.policy) }
async function show(row = null, host = null, dimensions = null) {
  if (busy.value || !canReadHosts.value || (row ? !canUpdate.value : !canCreate.value) || (dimensions && (!canReadResources.value || !validResourceDimensions(dimensions, false)))) return
  close()
  const own = requests.invalidate()
  editing.value = row?.id || ''; fixedMount.value = dimensions ? { ...dimensions } : null
  selected.value = dimensions ? null : host
  draft.value = row ? Object.fromEntries(['version','name','node_id','metric_key','dimensions','trigger_percent','recovery_percent','trigger_seconds','recovery_seconds','enabled'].map(key => [key, key === 'dimensions' ? { ...row[key] } : row[key]])) : { name: '', node_id: dimensions ? host?.node_id : '', metric_key: dimensions ? filesystemMetric : props.presets?.[0]?.metric_key, dimensions: dimensions ? { ...dimensions } : {}, enabled: false }
  open.value = true; busy.value = true
  try {
    const values = props.presets || (await requests.request(config => api.rules(1, config.signal))).presets
    if (!requests.current(own)) return
    if (!Array.isArray(values) || !values.length || (dimensions && !values.some(p => p.metric_key === filesystemMetric))) throw new Error('invalid_presets')
    presets.value = values
    if (!row) applyPreset()
    if ((row && !selected.value) || dimensions) {
      const id = draft.value.node_id
      const current = await requests.request(config => nodeResourcesAPI.node(id, config))
      if (!requests.current(own)) return
      if (current?.node_id !== id || !Number.isSafeInteger(current.version) || current.version < 1) throw new Error('invalid_node')
      selected.value = current
    }
  } catch (e) { if (requests.current(own)) editError.value = errorText(e) }
  finally { if (requests.current(own)) busy.value = false }
}
async function save() {
  if (busy.value || !selected.value || !canReadHosts.value || (editing.value ? !canUpdate.value : !canCreate.value) || (fixedMount.value && !fixedMountReady.value)) return
  const d = draft.value
  if (d.metric_key === filesystemMetric && !validResourceDimensions(d.dimensions, false)) { editError.value = t('monitor.resourceRules.mountRequired'); return }
  if (!d.name.trim() || ![d.recovery_percent,d.trigger_percent].every(value => typeof value === 'number' && Number.isFinite(value)) || !(d.recovery_percent > 0 && d.recovery_percent < d.trigger_percent && d.trigger_percent <= 100) || ![d.trigger_seconds,d.recovery_seconds].every(value => Number.isInteger(value) && value >= 15 && value <= 300 && value % 15 === 0)) { editError.value = t('monitor.resourceRules.invalid'); return }
  const own = requests.invalidate(); busy.value = true; editError.value = ''
  try {
    await requests.request(() => editing.value ? api.updateRule(editing.value, { ...d }) : api.createRule({ ...d, enabled: false }))
    if (!requests.current(own)) return
    close(); ElMessage.success(t('monitor.resourceRules.saved')); emit('saved')
  } catch (e) { if (requests.current(own)) editError.value = e.response?.status === 409 ? t('monitor.resourceRules.conflict') : errorText(e) }
  finally { if (requests.current(own)) busy.value = false }
}
watch(() => [open.value, draft.value.node_id, draft.value.metric_key, canReadResources.value, selected.value?.node_id], async () => {
  const own = mountRequests.invalidate(); mounts.value = []; mountError.value = ''; mountLoading.value = false
  if (editing.value || !open.value) return
  draft.value.dimensions = fixedMount.value ? { ...fixedMount.value } : {}
  if (draft.value.metric_key !== filesystemMetric || !draft.value.node_id || !canReadResources.value || (fixedMount.value && !selected.value)) return
  const nodeID = draft.value.node_id, nodeVersion = selected.value?.version
  mountLoading.value = true
  try {
    const result = await mountRequests.request(config => nodeResourcesAPI.instant({ node_id: nodeID, metrics: filesystemMetric }, config))
    if (!mountRequests.current(own)) return
    validateCollection(result.collection, result.queried_at); validateResourceResponse(result, nodeID, [filesystemMetric])
    if (fixedMount.value && result.node_version !== nodeVersion) throw new Error('invalid_resource_response')
    if (result.collection.state !== 'collecting' || result.collection.filesystem !== 'available') { mountError.value = 'monitor.resourceRules.noMounts'; return }
    mounts.value = resourceDimensionRows(result).filter(row => currentResourceValue(row.metrics[filesystemMetric]) !== null)
    if (fixedMount.value && !mounts.value.some(row => resourceDimensionID(row.dimensions) === resourceDimensionID(fixedMount.value))) mountError.value = 'monitor.resourceRules.noMounts'
  } catch (error) { if (mountRequests.current(own)) mountError.value = resourceErrorKey(error) }
  finally { if (mountRequests.current(own)) mountLoading.value = false }
})
watch(open, value => { if (!value) close() })
watch(() => JSON.stringify([auth.authContext, auth.permissions]), close)
onUnmounted(() => { close(); requests.dispose(); mountRequests.dispose() })
defineExpose({ show, close })
</script>
<style scoped>
.rule-condition { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
small { display: block; color: var(--addp-text-secondary); margin-top: 6px; width: 100%; }
</style>
