<template>
  <el-card class="service-card" shadow="hover" @click="$emit('click', service)">
    <div class="card-header">
      <div class="badge-group">
        <!-- 服务来源标识 -->
        <el-tag :type="getSourceTypeColor(source)" size="small" effect="dark">
          {{ formatSourceType(source) }}
        </el-tag>
        <!-- 服务类型标识（支持多个） -->
        <el-tag
          v-for="type in getServiceTypes(service)"
          :key="type"
          :type="getServiceTypeColor(type)"
          size="small"
        >
          {{ formatServiceType(type) }}
        </el-tag>
      </div>
      <div class="service-status">
        <el-tag :type="getStatusColor(service.status)" size="small">
          {{ formatStatus(service.status) }}
        </el-tag>
      </div>
    </div>

    <div class="card-body">
      <h4 class="service-name" :title="getServiceTitle(service)">{{ getServiceTitle(service) }}</h4>
      <p class="service-description" :title="getServiceDescription(service)">
        {{ getServiceDescription(service) || '暂无描述' }}
      </p>
      <div class="service-url">
        <el-icon><Link /></el-icon>
        <span :title="getServiceUrl(service)">{{ getServiceUrl(service) }}</span>
      </div>
    </div>

    <div class="card-footer">
      <div class="footer-item">
        <el-icon><Clock /></el-icon>
        <span>{{ formatDate(service.last_checked_at || service.created_at) }}</span>
      </div>
      <div v-if="getLayerCount(service) > 0" class="footer-item">
        <el-icon><Grid /></el-icon>
        <span>{{ getLayerCount(service) }} 图层</span>
      </div>
    </div>
  </el-card>
</template>

<script setup>
import { Link, Clock, Grid } from '@element-plus/icons-vue'

const props = defineProps({
  service: {
    type: Object,
    required: true
  },
  source: {
    type: String,
    default: 'external', // 'external', 'internal', 或 'data'
    validator: (value) => ['external', 'internal', 'data'].includes(value)
  }
})

defineEmits(['click'])

// 服务来源相关
const getSourceTypeColor = (source) => {
  const colors = {
    'internal': 'primary',    // 空间服务 - 蓝色
    'external': 'success',    // 服务注册 - 绿色
    'data': 'warning'         // 数据服务 - 橙色
  }
  return colors[source] || 'info'
}

const formatSourceType = (source) => {
  const types = {
    'internal': '空间服务',
    'external': '服务注册',
    'data': '数据服务'
  }
  return types[source] || source
}

// 服务类型相关
const getServiceTypes = (service) => {
  const types = []

  if (props.source === 'internal') {
    // 空间服务：返回所有启用的协议
    if (service.enabled_wfs) types.push('wfs')
    if (service.enabled_wmts) types.push('wmts')
    if (service.enabled_wms) types.push('wms')
    if (service.enabled_ogc_api) types.push('ogc_api')
    return types.length > 0 ? types : ['unknown']
  } else if (props.source === 'data') {
    // 数据服务：返回 Data API
    return ['data_api']
  } else {
    // 服务注册：返回单一service_type
    return [service.service_type || 'unknown']
  }
}

const getServiceTypeColor = (type) => {
  const colors = {
    wms: 'success',
    wfs: 'primary',
    wmts: 'warning',
    ogc_api: 'info',
    data_api: 'info',
    rest: 'danger',
    unknown: ''
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
    rest: 'REST',
    unknown: '未知'
  }
  return types[type] || type
}

// 服务状态相关
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

// 获取服务标题（兼容内外部服务）
const getServiceTitle = (service) => {
  return service.title || service.name || service.service_name || '未命名服务'
}

// 获取服务描述（兼容内外部服务）
const getServiceDescription = (service) => {
  return service.abstract || service.description || ''
}

// 获取服务URL（兼容内外部服务）
const getServiceUrl = (service) => {
  if (props.source === 'internal') {
    // 内部服务：构建OGC服务URL
    const serviceName = service.service_name
    if (service.enabled_wfs) {
      return `/ogc/wfs/${serviceName}`
    } else if (service.enabled_ogc_api) {
      return `/ogc/api/${serviceName}`
    } else if (service.enabled_wmts) {
      return `/ogc/wmts/${serviceName}`
    } else if (service.enabled_wms) {
      return `/ogc/wms/${serviceName}`
    }
    return '未配置服务端点'
  } else {
    // 外部服务：直接返回URL
    return service.url || '未配置'
  }
}

// 获取图层数量（兼容内外部服务）
const getLayerCount = (service) => {
  if (service.layers && Array.isArray(service.layers)) {
    return service.layers.length
  }
  return 0
}

// 格式化日期
const formatDate = (dateStr) => {
  if (!dateStr) return '未检查'
  const date = new Date(dateStr)
  const now = new Date()
  const diff = now - date
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 60) return `${minutes} 分钟前`
  if (hours < 24) return `${hours} 小时前`
  if (days < 7) return `${days} 天前`
  return date.toLocaleDateString('zh-CN')
}
</script>

<style scoped>
.service-card {
  cursor: pointer;
  transition: all 0.3s;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.service-card:hover {
  transform: translateY(-4px);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.badge-group {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

.card-body {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.service-name {
  margin: 0 0 8px 0;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.service-description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: #606266;
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  flex: 1;
}

.service-url {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #909399;
  margin-bottom: 12px;
}

.service-url span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 12px;
  border-top: 1px solid #ebeef5;
  font-size: 12px;
  color: #909399;
}

.footer-item {
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
