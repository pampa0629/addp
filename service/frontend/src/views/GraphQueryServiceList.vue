<template>
  <div class="graph-service-list">
    <div class="page-header">
      <h2>{{ t('service.graph.listTitle') }}</h2>
      <el-button type="primary" @click="openCreate">
        {{ t('service.graph.createBtn') }}
      </el-button>
    </div>

    <!-- 搜索栏 -->
    <div class="toolbar">
      <el-input
        v-model="searchQuery"
        :placeholder="t('service.graph.searchPlaceholder')"
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
    <div v-if="loading" class="loading-state">{{ t('service.common.loading') }}</div>
    <div v-else-if="services.length === 0" class="empty-state">
      <p>{{ t('service.graph.emptyText') }}</p>
      <p class="tip">{{ t('service.graph.emptyTip') }}</p>
    </div>
    <table v-else class="services-table">
      <thead>
        <tr>
          <th>{{ t('service.graph.colServiceName') }}</th>
          <th>{{ t('service.graph.colTitle') }}</th>
          <th>{{ t('service.graph.colConfigType') }}</th>
          <th>{{ t('service.graph.colNodeShape') }}</th>
          <th>{{ t('service.graph.colAccess') }}</th>
          <th>{{ t('service.graph.colStatus') }}</th>
          <th>{{ t('service.graph.colCreatedAt') }}</th>
          <th>{{ t('service.graph.colActions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="svc in services" :key="svc.id">
          <td><strong>{{ svc.service_name }}</strong></td>
          <td>{{ svc.title }}</td>
          <td>
            <el-tag :type="svc.config_type === 'shape' ? 'success' : 'warning'" size="small">
              {{ svc.config_type === 'shape' ? t('service.graph.shapeMode') : t('service.graph.cypherMode') }}
            </el-tag>
          </td>
          <td class="ellipsis">
            <span v-if="svc.config_type === 'shape'">{{ svc.node_shape }}</span>
            <span v-else class="cypher-preview">{{ svc.cypher_query }}</span>
          </td>
          <td>
            <el-tag :type="svc.public_access ? 'primary' : 'info'" size="small">
              {{ svc.public_access ? t('service.graph.publicAccess') : t('service.graph.authRequired') }}
            </el-tag>
          </td>
          <td>
            <el-tag :type="statusType(svc.status)" size="small">
              {{ statusText(svc.status) }}
            </el-tag>
          </td>
          <td>{{ formatDate(svc.created_at) }}</td>
          <td class="actions">
            <el-button size="small" @click="openDetail(svc)">{{ t('service.common.detail') }}</el-button>
            <el-button size="small" @click="openEdit(svc)">{{ t('service.common.edit') }}</el-button>
            <el-button size="small" type="danger" @click="confirmDelete(svc)">{{ t('service.common.delete') }}</el-button>
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
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import graphApi from '../api/graphQueryService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const openCreate = () => navigateServiceRoute(router, '/graph-services/create')
const openDetail = service => navigateServiceRoute(router, `/graph-services/${service.id}`)
const openEdit = service => navigateServiceRoute(router, `/graph-services/${service.id}/edit`)

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
    ElMessage.error(t('service.graph.loadFailed') + '：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

const confirmDelete = async (svc) => {
  try {
    await ElMessageBox.confirm(t('service.graph.deleteConfirm', { name: svc.service_name }), t('service.graph.deleteConfirmTitle'), { type: 'warning' })
    await graphApi.deleteService(svc.id)
    ElMessage.success(t('service.graph.deleteSuccess'))
    loadServices()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('service.graph.deleteFailed') + '：' + (e.response?.data?.error || e.message))
  }
}

const statusType = (s) => ({ active: 'success', inactive: 'info', error: 'danger' }[s] || '')
const statusText = (s) => ({
  active: t('service.graph.statusRunning'),
  inactive: t('service.graph.statusInactive'),
  error: t('service.graph.statusError')
}[s] || s)
const formatDate = (d) => d ? new Date(d).toLocaleDateString('zh-CN') : '-'

onMounted(loadServices)
</script>

<style scoped>
.graph-service-list {
  padding: 24px;
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
