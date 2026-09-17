<template>
  <el-button v-if="canRead" @click="visible = true">{{ t('model.metric_workspace.referencing_services') }}</el-button>
  <el-dialog v-model="visible" class="addp-dialog" :title="t('model.metric_workspace.referencing_services_title', { revision: revision.revision_no })" width="min(760px, calc(100vw - 24px))">
    <div class="reference-toolbar">
      <span>{{ t('model.metric_workspace.referencing_services_hint') }}</span>
      <el-button :loading="loading" @click="refresh++">{{ t('model.metric_workspace.refresh_services') }}</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <div v-loading="loading">
      <el-table v-if="!error && rows.length" :data="rows">
        <el-table-column :label="t('model.metric_workspace.service_title')" min-width="180">
          <template #default="{ row }"><el-button class="service-link" link type="primary" @click="openConsoleRoute(`/service/query-services/${row.id}`)">{{ row.title || row.service_name }}</el-button></template>
        </el-table-column>
        <el-table-column prop="service_name" :label="t('model.metric_workspace.service_name')" min-width="150" />
        <el-table-column :label="t('model.metric_workspace.service_status')" width="100">
          <template #default="{ row }">{{ t(`model.metric_workspace.service_${row.status}`) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-else-if="!error && !loading" :description="t('model.metric_workspace.no_referencing_services')" />
    </div>
    <el-pagination v-if="!error && total > pageSize" v-model:current-page="page" :page-size="pageSize" :total="total" :disabled="loading" layout="prev, pager, next" />
  </el-dialog>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { openConsoleRoute } from '@common-ui'
import { metricServiceAPI } from '../api/model'
const props = defineProps({ canRead: { type: Boolean, default: false }, implementationId: { type: Number, required: true }, revision: { type: Object, required: true } })
const { t } = useI18n()
const visible = ref(false), loading = ref(false), rows = ref([]), total = ref(0), page = ref(1), refresh = ref(0), error = ref('')
const pageSize = 20
watch(() => [props.implementationId, props.revision.id], () => {
  visible.value = false
  page.value = 1
}, { flush: 'sync' })
watch([visible, page, refresh, () => props.implementationId, () => props.revision.id, () => props.canRead], async (_values, _previous, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  rows.value = []
  error.value = ''
  loading.value = false
  if (!visible.value) { total.value = 0; return }
  if (!props.canRead) { total.value = 0; error.value = t('model.metric_workspace.services_forbidden'); return }
  loading.value = true
  try {
    const result = await metricServiceAPI.references(props.implementationId, props.revision.id, page.value, pageSize)
    if (cancelled) return
    total.value = result.total
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize))
    if (page.value > lastPage) { page.value = lastPage; return }
    rows.value = result.data
  } catch (err) {
    if (!cancelled) {
      total.value = 0
      error.value = t(err.response?.status === 403 ? 'model.metric_workspace.services_forbidden' : 'model.metric_workspace.services_unavailable')
    }
  } finally {
    if (!cancelled) loading.value = false
  }
})
</script>

<style scoped>
.reference-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
.service-link { white-space: normal; text-align: left; height: auto; overflow-wrap: anywhere; }
.el-pagination { margin-top: 16px; }
</style>
