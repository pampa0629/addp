<template>
  <div v-loading="loading" class="metric-source-picker">
    <el-alert :title="t('service.query.metricSourceHint')" type="info" :closable="false" />
    <el-alert v-if="errorKey" :title="t(errorKey)" type="warning" :closable="false" />
    <el-form label-position="top">
      <el-form-item :label="t('service.query.metricImplementationLabel')" required>
        <el-select v-model="implementationId" filterable clearable :disabled="loading || !!errorKey" @change="selectImplementation">
          <el-option v-for="item in sources" :key="item.id" :label="`${item.name} (#${item.id})`" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('service.query.metricRevisionLabel')" required>
        <el-select v-model="revisionId" clearable :disabled="loading || !selectedImplementation || !!errorKey" @change="selectRevision">
          <el-option v-for="revision in selectedImplementation?.revisions || []" :key="revision.id" :label="`R${revision.revision_no} (#${revision.id})`" :value="revision.id" />
        </el-select>
      </el-form-item>
    </el-form>
    <el-empty v-if="!loading && !errorKey && !sources.length" :description="t('service.query.metricSourcesEmpty')" />
    <div class="metric-source-actions">
      <el-button :loading="loading" @click="loadSources">{{ t('service.query.reloadMetricSources') }}</el-button>
      <el-button v-if="selectedReference" link type="primary" @click="openRevision">{{ t('service.query.metricOriginDetail') }}</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import { createModelMetricAPI, publishedMetricSources } from '../../../../common-frontend/basic/src/api/modelMetrics.js'
import client from '../api/client'

const props = defineProps({ modelValue: { type: Object, default: null } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const api = createModelMetricAPI(client)
const sources = ref([])
const loading = ref(false)
const errorKey = ref('')
const implementationId = ref(null)
const revisionId = ref(null)
let request = 0
const selectedImplementation = computed(() => sources.value.find(item => item.id === implementationId.value))
const selectedReference = computed(() => {
  const revision = selectedImplementation.value?.revisions.find(item => item.id === revisionId.value)
  return revision ? { implementation_id: implementationId.value, revision_id: revision.id } : null
})
const selectImplementation = () => {
  revisionId.value = null
  emit('update:modelValue', null)
}
const selectRevision = () => emit('update:modelValue', selectedReference.value)
const openRevision = () => {
  if (selectedReference.value) openConsoleRoute(`/modeling/metric-implementations/${implementationId.value}?revision_id=${revisionId.value}`)
}
const loadSources = async () => {
  const serial = ++request
  const previous = props.modelValue
  loading.value = true
  errorKey.value = ''
  sources.value = []
  implementationId.value = null
  revisionId.value = null
  emit('update:modelValue', null)
  try {
    const items = publishedMetricSources(await api.list())
    if (serial !== request) return
    sources.value = items
    implementationId.value = previous?.implementation_id || null
    revisionId.value = previous?.revision_id || null
    if (!selectedReference.value) {
      implementationId.value = null
      revisionId.value = null
    }
    selectRevision()
  } catch (error) {
    if (serial !== request) return
    errorKey.value = error.response?.status === 403 ? 'service.query.metricSourcesForbidden' : 'service.query.metricSourcesUnavailable'
  } finally {
    if (serial === request) loading.value = false
  }
}
onMounted(loadSources)
onBeforeUnmount(() => { request++ })
</script>

<style scoped>
.metric-source-picker { display: flex; flex-direction: column; gap: 16px; }
.metric-source-picker :deep(.el-select) { width: 100%; }
.metric-source-actions { display: flex; flex-wrap: wrap; gap: 12px; }
</style>
