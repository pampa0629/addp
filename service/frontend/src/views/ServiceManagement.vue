<template>
  <div class="service-management">
    <div class="header">
      <h2>{{ t('service.management.title') }}</h2>
      <div class="header-actions">
        <el-input
          v-model="searchKeyword"
          :placeholder="t('service.management.searchPlaceholder')"
          style="width: 300px; margin-right: 12px"
          :prefix-icon="Search"
          clearable
          @clear="loadServices"
          @keyup.enter="handleSearch"
        />
        <el-select
          v-model="filterType"
          :placeholder="t('service.management.serviceTypePlaceholder')"
          style="width: 150px; margin-right: 12px"
          clearable
          @change="loadServices"
        >
          <el-option :label="t('service.management.allTypes')" value="" />
          <el-option label="WMS" value="wms" />
          <el-option label="WFS" value="wfs" />
          <el-option label="WMTS" value="wmts" />
          <el-option label="OGC API" value="ogc_api" />
          <el-option label="Data API" value="data_api" />
          <el-option label="REST" value="rest" />
        </el-select>
        <el-button type="primary" :icon="Plus" @click="handleCreate">{{ t('service.management.registerBtn') }}</el-button>
        <el-button :icon="Download" @click="handleExport">{{ t('service.management.exportBtn') }}</el-button>
      </div>
    </div>

    <el-table
      v-loading="loading"
      :data="services"
      style="width: 100%"
      @row-click="handleRowClick"
    >
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="service_name" :label="t('service.management.colServiceName')" min-width="180" />
      <el-table-column prop="service_type" :label="t('service.management.colType')" width="120">
        <template #default="{ row }">
          <el-tag :type="getServiceTypeColor(row.service_type)">
            {{ formatServiceType(row.service_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('service.management.colEndpointUrl')" min-width="250">
        <template #default="{ row }">
          <div style="display: flex; align-items: center; gap: 8px;" @click.stop>
            <span style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ row.endpoint_url }}
            </span>
            <el-tooltip :content="t('service.management.copyEndpointTooltip')" placement="top">
              <el-button
                size="small"
                text
                @click.stop="handleCopyURL(row.endpoint_url)"
                style="padding: 4px;"
              >
                <el-icon><DocumentCopy /></el-icon>
              </el-button>
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="status" :label="t('service.management.colStatus')" width="100">
        <template #default="{ row }">
          <el-tag :type="getStatusColor(row.status)">
            {{ formatStatus(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_checked_at" :label="t('service.management.colLastChecked')" width="180">
        <template #default="{ row }">
          {{ formatDate(row.last_checked_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('service.common.actions')" width="280" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click.stop="handleEdit(row)">{{ t('service.common.edit') }}</el-button>
          <el-tooltip
            :content="isOGCService(row.service_type) ? t('service.management.refreshMetadataTooltip') : t('service.management.noRefreshTooltip')"
            placement="top"
          >
            <el-button
              size="small"
              :disabled="!isOGCService(row.service_type)"
              @click.stop="handleRefresh(row)"
            >
              {{ t('service.management.refreshBtn') }}
            </el-button>
          </el-tooltip>
          <el-tooltip
            :content="row.health_check_url ? t('service.management.healthCheckTooltip') : t('service.management.healthCheckDefaultTooltip')"
            placement="top"
          >
            <el-button
              size="small"
              @click.stop="handleHealthCheck(row)"
            >
              {{ t('service.management.healthCheckBtn') }}
            </el-button>
          </el-tooltip>
          <el-button size="small" type="danger" @click.stop="handleDelete(row)">{{ t('service.common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-if="total > 0"
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next, jumper"
      style="margin-top: 20px; justify-content: flex-end"
      @size-change="loadServices"
      @current-change="loadServices"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Plus, Download, DocumentCopy } from '@element-plus/icons-vue'
import serviceAPI from '../api/service'
import { copyToClipboard } from '../utils/serviceHelper'

const router = useRouter()
const { t } = useI18n()
const loading = ref(false)
const services = ref([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const searchKeyword = ref('')
const filterType = ref('')

const loadServices = async () => {
  try {
    loading.value = true
    const params = {
      page: currentPage.value,
      page_size: pageSize.value
    }

    if (filterType.value) {
      params.service_type = filterType.value
    }

    const result = await serviceAPI.list(params)
    services.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(t('service.management.loadFailed') + ': ' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

const handleSearch = async () => {
  if (!searchKeyword.value.trim()) {
    await loadServices()
    return
  }

  try {
    loading.value = true
    const result = await serviceAPI.search({ keyword: searchKeyword.value })
    services.value = result.data || []
    total.value = result.total || 0
  } catch (error) {
    ElMessage.error(t('service.management.searchFailed') + ': ' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

const handleCreate = () => {
  router.push('/services/create')
}

const handleEdit = (row) => {
  router.push(`/services/${row.id}/edit`)
}

const handleRowClick = (row) => {
  router.push(`/services/${row.id}`)
}

const handleRefresh = async (row) => {
  try {
    await serviceAPI.refreshMetadata(row.id)
    ElMessage.success(t('service.management.refreshSuccess'))
    await loadServices()
  } catch (error) {
    ElMessage.error(t('service.management.refreshFailed') + ': ' + (error.response?.data?.message || error.message))
  }
}

const handleHealthCheck = async (row) => {
  try {
    const result = await serviceAPI.healthCheck(row.id)
    if (result.status === 'healthy') {
      ElMessage.success(t('service.management.healthCheckPassed'))
    } else {
      ElMessage.warning(t('service.management.healthCheckFailed') + ': ' + result.message)
    }
    await loadServices()
  } catch (error) {
    ElMessage.error(t('service.management.healthCheckError') + ': ' + (error.response?.data?.message || error.message))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(t('service.management.deleteConfirm', { name: row.service_name }), t('service.management.deleteConfirmTitle'), {
      type: 'warning'
    })

    await serviceAPI.delete(row.id)
    ElMessage.success(t('service.management.deleteSuccess'))
    await loadServices()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('service.management.deleteFailed') + ': ' + (error.response?.data?.message || error.message))
    }
  }
}

const handleExport = async () => {
  try {
    const result = await serviceAPI.export()
    const blob = new Blob([JSON.stringify(result, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `services-export-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success(t('service.management.exportSuccess'))
  } catch (error) {
    ElMessage.error(t('service.management.exportFailed') + ': ' + (error.response?.data?.message || error.message))
  }
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

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
}

// 判断是否为 OGC 服务
const isOGCService = (serviceType) => {
  return ['wms', 'wfs', 'wmts', 'ogc_api'].includes(serviceType)
}

onMounted(() => {
  loadServices()
})
</script>

<style scoped>
.service-management {
  padding: 24px;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.header-actions {
  display: flex;
  align-items: center;
}

.el-table {
  flex: 1;
  cursor: pointer;
}
</style>
