<template>
  <div class="service-detail-container">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <h3>{{ t('service.serviceDetail.title') }}</h3>
          <div>
            <el-button @click="handleRefresh">{{ t('service.serviceDetail.refreshBtn') }}</el-button>
            <el-button @click="handleHealthCheck">{{ t('service.management.healthCheckBtn') }}</el-button>
            <el-button @click="handleEdit">{{ t('service.common.edit') }}</el-button>
            <el-button @click="handleBack">{{ t('service.common.back') }}</el-button>
          </div>
        </div>
      </template>

      <el-descriptions v-if="service" :column="2" border>
        <el-descriptions-item :label="t('service.serviceDetail.serviceIdLabel')">{{ service.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.serviceNameLabel')">{{ service.service_name }}</el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.serviceTypeLabel')">
          <el-tag :type="getServiceTypeColor(service.service_type)">
            {{ formatServiceType(service.service_type) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.statusLabel')">
          <el-tag :type="getStatusColor(service.status)">
            {{ formatStatus(service.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.endpointUrlLabel')" :span="2">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="flex: 1;">{{ service.endpoint_url }}</span>
            <el-button size="small" @click="handleCopyURL(service.endpoint_url)">
              {{ t('service.common.copy') }}
            </el-button>
          </div>
        </el-descriptions-item>
        <el-descriptions-item v-if="service.endpoints?.proxy" :label="t('service.serviceDetail.proxyEndpointLabel')" :span="2">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="flex: 1; color: var(--el-color-primary); font-weight: 500;">{{ service.endpoints.proxy }}</span>
            <el-button type="primary" size="small" @click="handleCopyURL(service.endpoints.proxy)">
              {{ t('service.common.copy') }}
            </el-button>
          </div>
          <div style="margin-top: 8px; font-size: 12px; color: var(--addp-text-tertiary);">
            {{ t('service.serviceDetail.proxyHelp') }}
          </div>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.authTypeLabel')">
          {{ formatAuthType(service.auth_type) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.lastCheckedLabel')">
          {{ formatDate(service.last_checked_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.createdAtLabel')">
          {{ formatDate(service.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.updatedAtLabel')">
          {{ formatDate(service.updated_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.serviceDetail.descriptionLabel')" :span="2">
          {{ service.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider v-if="service && service.metadata">{{ t('service.serviceDetail.metadataDivider') }}</el-divider>

      <el-card v-if="service && service.metadata" shadow="never" style="margin-top: 20px">
        <pre style="max-height: 400px; overflow: auto">{{ formatJSON(service.metadata) }}</pre>
      </el-card>

      <el-divider v-if="layers && layers.length > 0">{{ t('service.serviceDetail.layersDivider') }}</el-divider>

      <el-table v-if="layers && layers.length > 0" :data="layers" style="margin-top: 20px">
        <el-table-column prop="layer_name" :label="t('service.serviceDetail.colLayerName')" min-width="180" />
        <el-table-column prop="display_name" :label="t('service.serviceDetail.colDisplayName')" min-width="180" />
        <el-table-column prop="geometry_type" :label="t('service.serviceDetail.colGeometryType')" width="120" />
        <el-table-column prop="crs" :label="t('service.serviceDetail.colCRS')" width="120" />
        <el-table-column prop="enabled" :label="t('service.serviceDetail.colEnabled')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('service.common.enabled') : t('service.common.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import serviceAPI from '../api/service'
import { copyToClipboard } from '../utils/serviceHelper'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const loading = ref(false)
const service = ref(null)
const layers = ref([])

const loadService = async () => {
  try {
    loading.value = true
    const result = await serviceAPI.get(route.params.id)
    service.value = result
    layers.value = result.layers || []
  } catch (error) {
    ElMessage.error(t('service.serviceDetail.loadFailed') + ': ' + (error.response?.data?.message || error.message))
    handleBack()
  } finally {
    loading.value = false
  }
}

const handleRefresh = async () => {
  try {
    await serviceAPI.refreshMetadata(route.params.id)
    ElMessage.success(t('service.serviceDetail.refreshSuccess'))
    await loadService()
  } catch (error) {
    ElMessage.error(t('service.serviceDetail.refreshFailed') + ': ' + (error.response?.data?.message || error.message))
  }
}

const handleHealthCheck = async () => {
  try {
    const result = await serviceAPI.healthCheck(route.params.id)
    if (result.status === 'healthy') {
      ElMessage.success(t('service.management.healthCheckPassed'))
    } else {
      ElMessage.warning(t('service.management.healthCheckFailed') + ': ' + result.message)
    }
    await loadService()
  } catch (error) {
    ElMessage.error(t('service.management.healthCheckError') + ': ' + (error.response?.data?.message || error.message))
  }
}

const handleEdit = () => {
  navigateServiceRoute(router, `/services/${route.params.id}/edit`)
}

const handleBack = () => {
  navigateServiceRoute(router, '/services', { history: 'replace' })
}

const handleCopyURL = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success(t('service.management.urlCopied'))
  } else {
    ElMessage.error(t('service.common.copyFailed'))
  }
}

const getServiceTypeColor = (type) => {
  const colors = {
    wms: 'success',
    wfs: 'primary',
    wmts: 'warning',
    ogc_api: 'info',
    data_api: 'info',
    rest: 'danger'
  }
  return colors[type] || 'info'
}

const formatServiceType = (type) => {
  const types = {
    wms: 'WMS',
    wfs: 'WFS',
    wmts: 'WMTS',
    ogc_api: 'OGC API',
    data_api: 'Data API',
    rest: 'REST'
  }
  return types[type] || type
}

const getStatusColor = (status) => {
  const colors = {
    active: 'success',
    inactive: 'info',
    error: 'danger'
  }
  return colors[status] || 'info'
}

const formatStatus = (status) => {
  const statuses = {
    active: t('service.management.statusActive'),
    inactive: t('service.management.statusInactive'),
    error: t('service.management.statusError')
  }
  return statuses[status] || status
}

const formatAuthType = (type) => {
  const types = {
    none: t('service.serviceDetail.authNone'),
    basic: 'Basic Auth',
    bearer: 'Bearer Token',
    api_key: 'API Key'
  }
  return types[type] || type
}

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

const formatJSON = (obj) => {
  return JSON.stringify(obj, null, 2)
}

onMounted(() => {
  loadService()
})
</script>

<style scoped>
.service-detail-container {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

pre {
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--addp-text-secondary);
}
</style>
