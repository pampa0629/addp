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
import { getEnabledProtocols } from '../utils/serviceHelper'

const props = defineProps({
  service: {
    type: Object,
    required: true
  },
  source: {
    type: String,
    default: 'external', // 'external', 'query', 'registered', 'tile', 'graph'
    validator: (value) => ['external', 'query', 'registered', 'tile', 'graph'].includes(value)
  }
})

defineEmits(['click'])

// 服务来源相关
const getSourceTypeColor = (source) => {
  const colors = {
    'query': 'primary',       // 查询服务 - 蓝色
    'registered': 'success',  // 注册服务 - 绿色
    'tile': 'warning',        // 瓦片服务 - 橙色
    'external': 'info',       // 外部服务(旧) - 灰色
    'graph': 'danger'         // 图查询服务 - 红色
  }
  return colors[source] || 'info'
}

const formatSourceType = (source) => {
  const types = {
    'query': '查询服务',
    'registered': '注册服务',
    'tile': '瓦片服务',
    'external': '服务注册',
    'graph': '图查询服务'
  }
  return types[source] || source
}

// 服务类型相关
const getServiceTypes = (service) => {
  const types = []

  if (props.source === 'query') {
    // 查询服务：返回所有启用的协议
    const protocols = getEnabledProtocols(service)
    return protocols.map(p => p.key)
  } else if (props.source === 'tile') {
    // 瓦片服务：返回所有启用的协议
    const types = []
    if (service.protocols?.xyz?.enabled) types.push('xyz')
    if (service.protocols?.wmts?.enabled || service.protocols?.wmts !== false) types.push('wmts')  // WMTS 默认启用
    if (service.protocols?.ogc_tiles?.enabled) types.push('ogc_api')
    if (service.protocols?.tms?.enabled) types.push('tms')
    return types.length > 0 ? types : ['xyz']  // 默认至少显示 XYZ
  } else if (props.source === 'registered') {
    // 注册服务：返回单一service_type
    return [service.service_type || 'unknown']
  } else if (props.source === 'graph') {
    // 图查询服务：返回config_type
    return [service.config_type || 'unknown']
  } else {
    // 外部服务（旧）：返回单一service_type
    return [service.service_type || 'unknown']
  }
}

const getServiceTypeColor = (type) => {
  const colors = {
    wms: 'success',
    wfs: 'primary',
    wmts: 'warning',
    ogc_api: 'info',
    ogc_features: 'info',
    rest_api: 'danger',
    rest: 'danger',
    xyz: 'warning',
    tms: 'warning',
    label: 'success',
    cypher: 'warning',
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
    ogc_features: 'OGC Features',
    rest_api: 'REST API',
    rest: 'REST',
    xyz: 'XYZ Tiles',
    tms: 'TMS',
    label: '标签模式',
    cypher: 'Cypher 模式',
    unknown: '未知'
  }
  return types[type] || type.toUpperCase()
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

// 获取服务URL（兼容查询服务、注册服务和瓦片服务）
const getServiceUrl = (service) => {
  if (props.source === 'query') {
    // 查询服务：构建 REST API URL
    const serviceName = service.service_name
    const protocols = getEnabledProtocols(service)
    if (protocols.some(p => p.key === 'rest_api')) {
      return `/api/query/${serviceName}`
    } else if (protocols.some(p => p.key === 'ogc_features')) {
      return `/ogc/features/${serviceName}`
    }
    return '未配置服务端点'
  } else if (props.source === 'tile') {
    // 瓦片服务：返回瓦片端点
    const serviceName = service.service_name
    if (service.protocols?.xyz?.enabled) {
      return `/tiles/${serviceName}/{layerName}/{z}/{x}/{y}.mvt`
    } else if (service.protocols?.wmts?.enabled) {
      return `/wmts/${serviceName}?request=GetCapabilities`
    } else if (service.protocols?.ogc_tiles?.enabled) {
      return `/ogc/tiles/${serviceName}`
    }
    return `/tiles/${serviceName}/{layerName}/{z}/{x}/{y}.mvt`  // 默认 XYZ
  } else if (props.source === 'registered') {
    // 注册服务：返回端点URL
    return service.endpoint_url || '未配置'
  } else if (props.source === 'graph') {
    // 图查询服务：返回执行端点
    return service.endpoints?.execute || `/api/graph/${service.service_name}`
  } else {
    // 外部服务（旧）：直接返回URL
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
  color: var(--addp-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.service-description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
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
  color: var(--addp-text-tertiary);
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
  border-top: 1px solid var(--addp-border-color-light);
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.footer-item {
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
