<template>
  <div class="graph-service-list">
    <div class="page-header">
      <h2>图查询服务</h2>
      <el-button type="primary" @click="$router.push('/graph-services/create')">
        + 创建图查询服务
      </el-button>
    </div>

    <!-- 搜索栏 -->
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        placeholder="搜索服务名称、标题..."
        clearable
        style="width: 300px"
        @keyup.enter="loadServices"
        @clear="loadServices"
      >
        <template #suffix>
          <el-icon style="cursor:pointer" @click="loadServices"><Search /></el-icon>
        </template>
      </el-input>
    </div>

    <!-- 列表 -->
    <div v-if="loading" class="loading-state">加载中...</div>
    <div v-else-if="services.length === 0" class="empty-state">
      <p>暂无图查询服务</p>
      <p class="tip">点击"创建图查询服务"开始发布 Neo4j 数据服务</p>
    </div>
    <table v-else class="services-table">
      <thead>
        <tr>
          <th>服务名称</th>
          <th>标题</th>
          <th>配置类型</th>
          <th>节点标签/查询</th>
          <th>访问控制</th>
          <th>状态</th>
          <th>创建时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="svc in services" :key="svc.id">
          <td><strong>{{ svc.service_name }}</strong></td>
          <td>{{ svc.title }}</td>
          <td>
            <el-tag :type="svc.config_type === 'label' ? 'success' : 'warning'" size="small">
              {{ svc.config_type === 'label' ? '标签模式' : 'Cypher 模式' }}
            </el-tag>
          </td>
          <td class="ellipsis">
            <span v-if="svc.config_type === 'label'">{{ svc.node_label }}</span>
            <span v-else class="cypher-preview">{{ svc.cypher_query }}</span>
          </td>
          <td>
            <el-tag :type="svc.public_access ? 'primary' : 'info'" size="small">
              {{ svc.public_access ? '公开' : '需认证' }}
            </el-tag>
          </td>
          <td>
            <el-tag :type="statusType(svc.status)" size="small">
              {{ statusText(svc.status) }}
            </el-tag>
          </td>
          <td>{{ formatDate(svc.created_at) }}</td>
          <td class="actions">
            <el-button size="small" @click="$router.push(`/graph-services/${svc.id}`)">详情</el-button>
            <el-button size="small" @click="$router.push(`/graph-services/${svc.id}/edit`)">编辑</el-button>
            <el-button size="small" type="danger" @click="confirmDelete(svc)">删除</el-button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- 分页 -->
    <div v-if="total > 0" class="pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="limit"
        :total="total"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next"
        @change="loadServices"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import graphApi from '../api/graphQueryService'

const services = ref([])
const loading = ref(false)
const searchQuery = ref('')
const page = ref(1)
const limit = ref(20)
const total = ref(0)

const loadServices = async () => {
  loading.value = true
  try {
    const params = { page: page.value, page_size: limit.value }
    if (searchQuery.value) params.search = searchQuery.value
    const res = await graphApi.listServices(params)
    services.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    ElMessage.error('加载失败：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

const confirmDelete = async (svc) => {
  try {
    await ElMessageBox.confirm(`确认删除服务 "${svc.service_name}"？`, '删除确认', { type: 'warning' })
    await graphApi.deleteService(svc.id)
    ElMessage.success('删除成功')
    loadServices()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}

const statusType = (s) => ({ active: 'success', inactive: 'info', error: 'danger' }[s] || '')
const statusText = (s) => ({ active: '运行中', inactive: '已停用', error: '错误' }[s] || s)
const formatDate = (d) => d ? new Date(d).toLocaleDateString('zh-CN') : '-'

onMounted(loadServices)
</script>

<style scoped>
.graph-service-list {
  padding: 24px;
  min-height: 100%;
  background: var(--addp-bg-secondary);
}
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.toolbar { margin-bottom: 16px; }
.loading-state, .empty-state { text-align: center; padding: 60px 0; color: var(--addp-text-tertiary); }
.empty-state .tip { font-size: 13px; margin-top: 8px; }
.services-table { width: 100%; border-collapse: collapse; font-size: 14px; }
.services-table th, .services-table td { padding: 10px 12px; border-bottom: 1px solid var(--addp-border-color-light); text-align: left; color: var(--addp-text-primary); }
.services-table th { background: var(--addp-bg-secondary); font-weight: 600; }
.services-table tr:hover td { background: var(--addp-bg-secondary); filter: brightness(0.95); }
.ellipsis { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cypher-preview { font-family: monospace; font-size: 12px; color: var(--addp-text-secondary); }
.actions { white-space: nowrap; }
.actions .el-button + .el-button { margin-left: 6px; }
.pagination { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
