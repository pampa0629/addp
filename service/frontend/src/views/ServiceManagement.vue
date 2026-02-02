<template>
  <div class="service-management">
    <div class="header">
      <h2>服务注册</h2>
      <div class="header-actions">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索已注册服务"
          style="width: 300px; margin-right: 12px"
          :prefix-icon="Search"
          clearable
          @clear="loadServices"
          @keyup.enter="handleSearch"
        />
        <el-select
          v-model="filterType"
          placeholder="服务类型"
          style="width: 150px; margin-right: 12px"
          clearable
          @change="loadServices"
        >
          <el-option label="全部" value="" />
          <el-option label="WMS" value="wms" />
          <el-option label="WFS" value="wfs" />
          <el-option label="WMTS" value="wmts" />
          <el-option label="OGC API" value="ogc_api" />
          <el-option label="Data API" value="data_api" />
          <el-option label="REST" value="rest" />
        </el-select>
        <el-button type="primary" :icon="Plus" @click="handleCreate">注册服务</el-button>
        <el-button :icon="Download" @click="handleExport">导出配置</el-button>
      </div>
    </div>

    <el-table
      v-loading="loading"
      :data="services"
      style="width: 100%"
      @row-click="handleRowClick"
    >
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="服务名称" min-width="180" />
      <el-table-column prop="service_type" label="类型" width="120">
        <template #default="{ row }">
          <el-tag :type="getServiceTypeColor(row.service_type)">
            {{ formatServiceType(row.service_type) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="服务地址" min-width="250">
        <template #default="{ row }">
          <div style="display: flex; align-items: center; gap: 8px;" @click.stop>
            <span style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ row.url }}
            </span>
            <el-tooltip content="复制服务地址" placement="top">
              <el-button
                size="small"
                text
                @click.stop="handleCopyURL(row.url)"
                style="padding: 4px;"
              >
                <el-icon><DocumentCopy /></el-icon>
              </el-button>
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="getStatusColor(row.status)">
            {{ formatStatus(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_checked_at" label="最后检查" width="180">
        <template #default="{ row }">
          {{ formatDate(row.last_checked_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click.stop="handleEdit(row)">编辑</el-button>
          <el-button size="small" @click.stop="handleRefresh(row)">刷新</el-button>
          <el-tooltip
            :content="row.health_check_url ? '执行健康检查' : '未配置健康检查地址，将使用服务地址进行检查'"
            placement="top"
          >
            <el-button
              size="small"
              @click.stop="handleHealthCheck(row)"
            >
              健康检查
            </el-button>
          </el-tooltip>
          <el-button size="small" type="danger" @click.stop="handleDelete(row)">删除</el-button>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Plus, Download, DocumentCopy } from '@element-plus/icons-vue'
import serviceAPI from '../api/service'

const router = useRouter()
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
    ElMessage.error('加载服务列表失败: ' + (error.response?.data?.message || error.message))
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
    ElMessage.error('搜索失败: ' + (error.response?.data?.message || error.message))
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
    ElMessage.success('元数据刷新成功')
    await loadServices()
  } catch (error) {
    ElMessage.error('刷新失败: ' + (error.response?.data?.message || error.message))
  }
}

const handleHealthCheck = async (row) => {
  try {
    const result = await serviceAPI.healthCheck(row.id)
    if (result.status === 'healthy') {
      ElMessage.success('健康检查通过')
    } else {
      ElMessage.warning('健康检查失败: ' + result.message)
    }
    await loadServices()
  } catch (error) {
    ElMessage.error('健康检查失败: ' + (error.response?.data?.message || error.message))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确定要删除服务"${row.name}"吗？`, '确认删除', {
      type: 'warning'
    })

    await serviceAPI.delete(row.id)
    ElMessage.success('删除成功')
    await loadServices()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.response?.data?.message || error.message))
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
    ElMessage.success('导出成功')
  } catch (error) {
    ElMessage.error('导出失败: ' + (error.response?.data?.message || error.message))
  }
}

const handleCopyURL = async (url) => {
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('服务地址已复制到剪贴板')
  } catch (error) {
    // 降级方案：使用传统方法复制
    const textarea = document.createElement('textarea')
    textarea.value = url
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    try {
      document.execCommand('copy')
      ElMessage.success('服务地址已复制到剪贴板')
    } catch (err) {
      ElMessage.error('复制失败，请手动复制')
    }
    document.body.removeChild(textarea)
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

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN')
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
