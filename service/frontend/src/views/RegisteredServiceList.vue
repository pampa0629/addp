<template>
  <div class="registered-service-list">
    <h1>{{ $t('service.registered.listTitle') }}</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          :placeholder="$t('service.registered.searchPlaceholder')"
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">{{ $t('service.common.search') }}</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">{{ $t('service.registered.createBtn') }}</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">{{ $t('service.common.loading') }}</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>{{ $t('service.registered.emptyText') }}</p>
        <p class="tip">{{ $t('service.registered.emptyTip') }}</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>{{ $t('service.registered.colServiceName') }}</th>
            <th>{{ $t('service.registered.colTitle') }}</th>
            <th>{{ $t('service.registered.colServiceType') }}</th>
            <th>{{ $t('service.registered.colEndpoint') }}</th>
            <th>{{ $t('service.registered.colAuthType') }}</th>
            <th>{{ $t('service.registered.colHealthStatus') }}</th>
            <th>{{ $t('service.registered.colLastChecked') }}</th>
            <th>{{ $t('service.registered.colCreatedAt') }}</th>
            <th>{{ $t('service.registered.colActions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="service in services" :key="service.id">
            <td><strong>{{ service.service_name }}</strong></td>
            <td>{{ service.title }}</td>
            <td>
              <span class="badge badge-info">
                {{ serviceTypeText(service.service_type) }}
              </span>
            </td>
            <td>
              <div class="endpoint-url" :title="service.endpoint_url">
                {{ truncateUrl(service.endpoint_url) }}
              </div>
            </td>
            <td>
              <span class="badge badge-secondary">
                {{ authTypeText(service.auth_type) }}
              </span>
            </td>
            <td>
              <span class="badge" :class="statusClass(service.status)">
                {{ statusText(service.status) }}
              </span>
            </td>
            <td>{{ formatDate(service.last_checked_at) }}</td>
            <td>{{ formatDate(service.created_at) }}</td>
            <td class="actions">
              <button @click="goToDetail(service.id)" class="btn btn-sm btn-info">{{ $t('service.common.detail') }}</button>
              <button @click="goToEdit(service.id)" class="btn btn-sm btn-warning">{{ $t('service.common.edit') }}</button>
              <button @click="refreshMetadata(service.id)" class="btn btn-sm btn-primary">{{ $t('service.common.refresh') }}</button>
              <button @click="healthCheck(service.id)" class="btn btn-sm btn-success">{{ $t('service.common.check') }}</button>
              <button @click="confirmDelete(service.id)" :disabled="deleting" class="btn btn-sm btn-danger">{{ $t('service.common.delete') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div v-if="total > 0" class="pagination">
      <button
        @click="previousPage"
        :disabled="page === 1"
        class="btn btn-sm"
      >
        {{ $t('service.common.prevPage') }}
      </button>
      <span class="page-info">{{ $t('service.common.pageInfo', { page, total: totalPages, count: total }) }}</span>
      <button
        @click="nextPage"
        :disabled="page >= totalPages"
        class="btn btn-sm"
      >
        {{ $t('service.common.nextPage') }}
      </button>
    </div>
  </div>
</template>

<script>
import { ElMessage, ElMessageBox } from 'element-plus'
import registeredServiceAPI from '@/api/registeredService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

export default {
  name: 'RegisteredServiceList',
  data() {
    return {
      services: [],
      searchQuery: '',
      page: 1,
      limit: 20,
      total: 0,
      deleting: false,
      loading: false,
      error: null
    }
  },
  computed: {
    totalPages() {
      return Math.ceil(this.total / this.limit)
    }
  },
  mounted() {
    this.loadServices()
  },
  methods: {
    async loadServices() {
      this.loading = true
      this.error = null
      try {
        const params = {
          page: this.page,
          limit: this.limit
        }
        if (this.searchQuery) {
          params.search = this.searchQuery
        }

        const response = await registeredServiceAPI.listServices(params)
        this.services = response.data || []
        this.total = response.total || 0
      } catch (error) {
        this.error = this.$t('service.registered.loadFailed') + ': ' + (error.message || this.$t('service.common.unknownError'))
        console.error('Failed to load registered services:', error)
        ElMessage.error(this.error)
      } finally {
        this.loading = false
      }
    },

    handleSearch() {
      this.page = 1
      this.loadServices()
    },

    async confirmDelete(id) {
      if (this.deleting) return
      this.deleting = true
      try {
        await ElMessageBox.confirm(
          this.$t('service.registered.deleteConfirm'),
          this.$t('service.common.deleteConfirmTitle'),
          {
            type: 'warning', customClass: 'addp-message-box',
            confirmButtonText: this.$t('service.common.delete'),
            cancelButtonText: this.$t('service.common.cancel'),
            confirmButtonClass: 'el-button--danger',
            autofocus: false, distinguishCancelAndClose: true
          }
        )
        await registeredServiceAPI.deleteService(id)
        ElMessage.success(this.$t('service.registered.deleteSuccess'))
        await this.loadServices()
      } catch (error) {
        if (error === 'cancel' || error === 'close') return
        ElMessage.error(this.$t('service.registered.deleteFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to delete registered service:', error)
      } finally {
        this.deleting = false
      }
    },

    async refreshMetadata(id) {
      try {
        await registeredServiceAPI.refreshMetadata(id, { force: false })
        ElMessage.success(this.$t('service.registered.refreshSuccess'))
        await this.loadServices()
      } catch (error) {
        ElMessage.error(this.$t('service.registered.refreshFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to refresh metadata:', error)
      }
    },

    async healthCheck(id) {
      try {
        const response = await registeredServiceAPI.healthCheck(id)
        const result = response
        ElMessage({
          message: this.$t('service.registered.healthCheckResult', { status: result.status, message: result.message, time: result.response_time }),
          type: result.status === 'healthy' ? 'success' : 'warning',
          showClose: true,
          duration: 6000
        })
        await this.loadServices()
      } catch (error) {
        ElMessage.error(this.$t('service.registered.healthCheckFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to perform health check:', error)
      }
    },

    serviceTypeText(serviceType) {
      const typeMap = {
        wms: 'WMS',
        wfs: 'WFS',
        wmts: 'WMTS',
        ogc_api: 'OGC API',
        xyz: 'XYZ',
        rest: 'REST'
      }
      return typeMap[serviceType] || serviceType.toUpperCase()
    },

    authTypeText(authType) {
      const typeMap = {
        none: this.$t('service.registered.authNone'),
        basic: 'Basic',
        bearer: 'Bearer',
        api_key: 'API Key'
      }
      return typeMap[authType] || authType
    },

    statusText(status) {
      const statusMap = {
        active: this.$t('service.registered.statusActive'),
        inactive: this.$t('service.registered.statusInactive'),
        error: this.$t('service.registered.statusError')
      }
      return statusMap[status] || status
    },

    statusClass(status) {
      const classMap = {
        active: 'badge-success',
        inactive: 'badge-warning',
        error: 'badge-danger'
      }
      return classMap[status] || 'badge-secondary'
    },

    truncateUrl(url) {
      if (!url) return '-'
      if (url.length > 50) {
        return url.substring(0, 47) + '...'
      }
      return url
    },

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    },

    goToCreate() {
      navigateServiceRoute(this.$router, '/services/create')
    },

    goToDetail(id) {
      navigateServiceRoute(this.$router, `/services/${id}`)
    },

    goToEdit(id) {
      navigateServiceRoute(this.$router, `/services/${id}/edit`)
    },

    previousPage() {
      if (this.page > 1) {
        this.page--
        this.loadServices()
      }
    },

    nextPage() {
      if (this.page < this.totalPages) {
        this.page++
        this.loadServices()
      }
    }
  }
}
</script>

<style scoped>
.registered-service-list {
  padding: 20px;
}

h1 {
  margin-bottom: 20px;
  color: var(--addp-text-primary);
}

.toolbar {
  flex-wrap: wrap;
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  justify-content: space-between;
}

.search-bar {
  min-width: 0;
  flex: 1;
  display: flex;
  gap: 10px;
}

.search-bar input {
  min-width: 0;
  color: var(--addp-text-primary);
  background-color: var(--addp-bg-primary);
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  font-size: 14px;
}

.btn {
  color: var(--addp-text-primary);
  background-color: var(--addp-bg-secondary);
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.3s;
}

.btn-primary {
  background-color: var(--el-color-primary);
  color: var(--el-color-white);
}

.btn-primary:hover {
  background-color: var(--el-color-primary-dark-2);
}

.btn-success {
  background-color: var(--el-color-success);
  color: var(--el-color-white);
}

.btn-success:hover {
  background-color: var(--el-color-success-dark-2);
}

.btn-warning {
  background-color: var(--el-color-warning);
  color: var(--el-color-white);
}

.btn-warning:hover {
  background-color: var(--el-color-warning-dark-2);
}

.btn-danger {
  background-color: var(--el-color-danger);
  color: var(--el-color-white);
}

.btn-danger:hover {
  background-color: var(--el-color-danger-dark-2);
}

.btn-info {
  background-color: var(--el-color-info);
  color: var(--el-color-white);
}

.btn-info:hover {
  background-color: var(--el-color-info-dark-2);
}

.btn-sm {
  padding: 4px 8px;
  font-size: 12px;
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.services-container {
  overflow-x: auto;
  margin: 20px 0;
}

.loading,
.empty-state {
  text-align: center;
  padding: 40px 20px;
  color: var(--addp-text-secondary);
}

.empty-state p {
  margin: 10px 0;
}

.empty-state .tip {
  font-size: 14px;
  color: var(--addp-text-tertiary);
}

.services-table {
  width: 100%;
  border-collapse: collapse;
  background-color: var(--addp-bg-primary);
  box-shadow: var(--el-box-shadow-light);
}

.services-table th {
  background-color: var(--addp-bg-secondary);
  padding: 12px;
  text-align: left;
  font-weight: 500;
  border-bottom: 2px solid var(--addp-border-color);
  font-size: 14px;
}

.services-table td {
  padding: 12px;
  border-bottom: 1px solid var(--addp-border-color);
  font-size: 14px;
}

.services-table tbody tr:hover {
  background-color: var(--addp-bg-secondary);
}

.endpoint-url {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--addp-text-primary);
  max-width: 250px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.badge {
  display: inline-block;
  padding: 4px 8px;
  border-radius: 3px;
  font-size: 12px;
  font-weight: 500;
}

.badge-info {
  background-color: var(--el-color-info-light-9);
  color: var(--el-color-info);
}

.badge-success {
  background-color: var(--el-color-success-light-9);
  color: var(--el-color-success);
}

.badge-warning {
  background-color: var(--el-color-warning-light-9);
  color: var(--el-color-warning);
}

.badge-danger {
  background-color: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
}

.badge-secondary {
  background-color: var(--el-color-info-light-9);
  color: var(--el-color-info);
}

.actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 15px;
  margin-top: 20px;
  padding: 15px;
  background-color: var(--addp-bg-secondary);
  border-radius: 4px;
}

.page-info {
  font-size: 14px;
  color: var(--addp-text-secondary);
}
</style>
