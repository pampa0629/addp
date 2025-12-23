<template>
  <el-card class="service-card" shadow="hover" @click="$emit('click', service)">
    <div class="card-header">
      <div class="service-type-badge">
        <el-tag :type="getServiceTypeColor(service.service_type)" size="small">
          {{ formatServiceType(service.service_type) }}
        </el-tag>
      </div>
      <div class="service-status">
        <el-tag :type="getStatusColor(service.status)" size="small">
          {{ formatStatus(service.status) }}
        </el-tag>
      </div>
    </div>

    <div class="card-body">
      <h4 class="service-name" :title="service.name">{{ service.name }}</h4>
      <p class="service-description" :title="service.description">
        {{ service.description || '暂无描述' }}
      </p>
      <div class="service-url">
        <el-icon><Link /></el-icon>
        <span :title="service.url">{{ service.url }}</span>
      </div>
    </div>

    <div class="card-footer">
      <div class="footer-item">
        <el-icon><Clock /></el-icon>
        <span>{{ formatDate(service.last_checked_at) }}</span>
      </div>
      <div v-if="service.layers && service.layers.length > 0" class="footer-item">
        <el-icon><Grid /></el-icon>
        <span>{{ service.layers.length }} 图层</span>
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
  }
})

defineEmits(['click'])

const getServiceTypeColor = (type) => {
  const colors = {
    wms: 'success',
    wfs: 'primary',
    wmts: 'warning',
    ogc_api: 'info',
    data_api: '',
    rest: 'danger'
  }
  return colors[type] || ''
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
  return colors[status] || ''
}

const formatStatus = (status) => {
  const statuses = {
    active: '活跃',
    inactive: '未激活',
    error: '错误'
  }
  return statuses[status] || status
}

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
