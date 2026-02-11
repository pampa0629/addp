<template>
  <div class="service-detail-container">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <h3>服务详情</h3>
          <div>
            <el-button @click="handleRefresh">刷新元数据</el-button>
            <el-button @click="handleHealthCheck">健康检查</el-button>
            <el-button @click="handleEdit">编辑</el-button>
            <el-button @click="handleBack">返回</el-button>
          </div>
        </div>
      </template>

      <el-descriptions v-if="service" :column="2" border>
        <el-descriptions-item label="服务ID">{{ service.id }}</el-descriptions-item>
        <el-descriptions-item label="服务名称">{{ service.service_name }}</el-descriptions-item>
        <el-descriptions-item label="服务类型">
          <el-tag :type="getServiceTypeColor(service.service_type)">
            {{ formatServiceType(service.service_type) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusColor(service.status)">
            {{ formatStatus(service.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="原始服务地址" :span="2">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="flex: 1;">{{ service.endpoint_url }}</span>
            <el-button size="small" @click="handleCopyURL(service.endpoint_url)">
              复制
            </el-button>
          </div>
        </el-descriptions-item>
        <el-descriptions-item v-if="service.endpoints?.proxy" label="代理端点" :span="2">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="flex: 1; color: #409EFF; font-weight: 500;">{{ service.endpoints.proxy }}</span>
            <el-button type="primary" size="small" @click="handleCopyURL(service.endpoints.proxy)">
              复制
            </el-button>
          </div>
          <div style="margin-top: 8px; font-size: 12px; color: var(--addp-text-tertiary);">
            ✓ 通过 ADDP Gateway 统一代理，支持认证转发和访问审计
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="认证类型">
          {{ formatAuthType(service.auth_type) }}
        </el-descriptions-item>
        <el-descriptions-item label="最后检查时间">
          {{ formatDate(service.last_checked_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ formatDate(service.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="更新时间">
          {{ formatDate(service.updated_at) }}
        </el-descriptions-item>
        <el-descriptions-item label="服务描述" :span="2">
          {{ service.description || '-' }}
        </el-descriptions-item>
      </el-descriptions>

      <el-divider v-if="service && service.metadata">服务元数据</el-divider>

      <el-card v-if="service && service.metadata" shadow="never" style="margin-top: 20px">
        <pre style="max-height: 400px; overflow: auto">{{ formatJSON(service.metadata) }}</pre>
      </el-card>

      <el-divider v-if="layers && layers.length > 0">图层列表</el-divider>

      <el-table v-if="layers && layers.length > 0" :data="layers" style="margin-top: 20px">
        <el-table-column prop="layer_name" label="图层名称" min-width="180" />
        <el-table-column prop="display_name" label="显示名称" min-width="180" />
        <el-table-column prop="geometry_type" label="几何类型" width="120" />
        <el-table-column prop="crs" label="坐标系" width="120" />
        <el-table-column prop="enabled" label="启用状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
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
import { ElMessage } from 'element-plus'
import serviceAPI from '../api/service'
import { copyToClipboard } from '../utils/serviceHelper'

const router = useRouter()
const route = useRoute()
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
    ElMessage.error('加载服务详情失败: ' + (error.response?.data?.message || error.message))
    handleBack()
  } finally {
    loading.value = false
  }
}

const handleRefresh = async () => {
  try {
    await serviceAPI.refreshMetadata(route.params.id)
    ElMessage.success('元数据刷新成功')
    await loadService()
  } catch (error) {
    ElMessage.error('刷新失败: ' + (error.response?.data?.message || error.message))
  }
}

const handleHealthCheck = async () => {
  try {
    const result = await serviceAPI.healthCheck(route.params.id)
    if (result.status === 'healthy') {
      ElMessage.success('健康检查通过')
    } else {
      ElMessage.warning('健康检查失败: ' + result.message)
    }
    await loadService()
  } catch (error) {
    ElMessage.error('健康检查失败: ' + (error.response?.data?.message || error.message))
  }
}

const handleEdit = () => {
  router.push(`/services/${route.params.id}/edit`)
}

const handleBack = () => {
  router.push('/services')
}

const handleCopyURL = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success('服务地址已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动复制')
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
    active: '活跃',
    inactive: '未激活',
    error: '错误'
  }
  return statuses[status] || status
}

const formatAuthType = (type) => {
  const types = {
    none: '无认证',
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
