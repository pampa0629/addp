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
        {{ getServiceDescription(service) || t('service.serviceCard.noDescription') }}
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
        <span>{{ t('service.serviceCard.layerCount', { count: getLayerCount(service) }) }}</span>
      </div>
    </div>
  </el-card>
</template>

<script setup>
import { Link, Clock, Grid } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getEnabledProtocols } from '../utils/serviceHelper'

const { t } = useI18n()

const props = defineProps({
  service: {
    type: Object,
    required: true
  },
  source: {
    type: String,
    default: 'external',
    validator: (value) => ['external', 'query', 'registered', 'tile', 'graph'].includes(value)
  }
})

defineEmits(['click'])

const getSourceTypeColor = (source) => {
  const colors = {
    'query': 'primary',
    'registered': 'success',
    'tile': 'warning',
    'external': 'info',
    'graph': 'danger'
  }
  return colors[source] || 'info'
}

const formatSourceType = (source) => {
  const key = `service.serviceCard.sourceType.${source}`
  const translated = t(key)
  return translated !== key ? translated : source
}

const getServiceTypes = (service) => {
  if (props.source === 'query') {
    const protocols = getEnabledProtocols(service)
    return protocols.map(p => p.key)
  } else if (props.source === 'tile') {
    const types = []
    if (service.protocols?.xyz?.enabled) types.push('xyz')
    if (service.protocols?.wmts?.enabled || service.protocols?.wmts !== false) types.push('wmts')
    if (service.protocols?.ogc_tiles?.enabled) types.push('ogc_api')
    if (service.protocols?.tms?.enabled) types.push('tms')
    return types.length > 0 ? types : ['xyz']
  } else if (props.source === 'registered') {
    return [service.service_type || 'unknown']
  } else if (props.source === 'graph') {
    return [service.config_type || 'unknown']
  } else {
    return [service.service_type || 'unknown']
  }
}

const getServiceTypeColor = (type) => {
  const colors = {
    wms: 'success', wfs: 'primary', wmts: 'warning', ogc_api: 'info',
    ogc_features: 'info', rest_api: 'danger', rest: 'danger',
    xyz: 'warning', tms: 'warning', shape: 'success', cypher: 'warning', unknown: ''
  }
  return colors[type] || 'info'
}

const formatServiceType = (type) => {
  const techTypes = {
    wms: 'WMS', wfs: 'WFS', wmts: 'WMTS', ogc_api: 'OGC API',
    ogc_features: 'OGC Features', rest_api: 'REST API', rest: 'REST',
    xyz: 'XYZ Tiles', tms: 'TMS'
  }
  if (techTypes[type]) return techTypes[type]
  const key = `service.serviceCard.serviceType.${type}`
  const translated = t(key)
  return translated !== key ? translated : type.toUpperCase()
}

const getStatusColor = (status) => {
  const colors = { active: 'success', inactive: 'info', error: 'danger' }
  return colors[status] || 'info'
}

const formatStatus = (status) => {
  const statusMap = {
    active: t('service.common.active'),
    inactive: t('service.common.inactive'),
    error: t('service.common.error')
  }
  return statusMap[status] || status
}

const getServiceTitle = (service) => {
  return service.title || service.name || service.service_name || t('service.common.unknown')
}

const getServiceDescription = (service) => {
  return service.abstract || service.description || ''
}

const getServiceUrl = (service) => {
  if (props.source === 'query') {
    const serviceName = service.service_name
    const protocols = getEnabledProtocols(service)
    if (protocols.some(p => p.key === 'rest_api')) return `/api/query/${serviceName}/query`
    if (protocols.some(p => p.key === 'ogc_features')) return `/ogc/features/${serviceName}`
    return t('service.common.notConfigured')
  } else if (props.source === 'tile') {
    const serviceName = service.service_name
    if (service.protocols?.xyz?.enabled) return `/tiles/${serviceName}/{layerName}/{z}/{x}/{y}.mvt`
    if (service.protocols?.wmts?.enabled) return `/wmts/${serviceName}?request=GetCapabilities`
    if (service.protocols?.ogc_tiles?.enabled) return `/ogc/tiles/${serviceName}`
    return `/tiles/${serviceName}/{layerName}/{z}/{x}/{y}.mvt`
  } else if (props.source === 'registered') {
    return service.endpoint_url || t('service.common.notConfigured')
  } else if (props.source === 'graph') {
    return service.endpoints?.execute || `/api/v1/graph/${service.service_name}`
  } else {
    return service.url || t('service.common.notConfigured')
  }
}

const getLayerCount = (service) => {
  if (service.layers && Array.isArray(service.layers)) return service.layers.length
  return 0
}

const formatDate = (dateStr) => {
  if (!dateStr) return t('service.serviceCard.notChecked')
  const date = new Date(dateStr)
  const now = new Date()
  const diff = now - date
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 60) return t('service.serviceCard.minutesAgo', { n: minutes })
  if (hours < 24) return t('service.serviceCard.hoursAgo', { n: hours })
  if (days < 7) return t('service.serviceCard.daysAgo', { n: days })
  return date.toLocaleDateString()
}
</script>

<style scoped>
.service-card {
  cursor: pointer;
  transition: all 0.3s;
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
