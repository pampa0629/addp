<template>
  <div class="query-service-list">
    <h1>{{ $t('service.query.listTitle') }}</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          :placeholder="$t('service.query.searchPlaceholder')"
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">{{ $t('service.common.search') }}</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">{{ $t('service.query.createBtn') }}</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">{{ $t('service.common.loading') }}</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>{{ $t('service.query.emptyText') }}</p>
        <p class="tip">{{ $t('service.query.emptyTip') }}</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>{{ $t('service.query.colServiceName') }}</th>
            <th>{{ $t('service.query.colTitle') }}</th>
            <th>{{ $t('service.query.colConfigType') }}</th>
            <th>{{ $t('service.query.colDataSource') }}</th>
            <th>{{ $t('service.query.colProtocols') }}</th>
            <th>{{ $t('service.query.colAccess') }}</th>
            <th>{{ $t('service.query.colStatus') }}</th>
            <th>{{ $t('service.query.colCreatedAt') }}</th>
            <th>{{ $t('service.query.colActions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="service in services" :key="service.id">
            <td><strong>{{ service.service_name }}</strong></td>
            <td>{{ service.title }}</td>
            <td>
              <span class="badge badge-secondary">
                {{ configTypeText(service.config_type) }}
              </span>
            </td>
            <td>
              <div class="data-source">
                <span v-if="service.config_type === 'table'">
                  {{ service.schema_name }}.{{ service.table_name }}
                </span>
                <span v-else class="sql-indicator">{{ $t('service.query.sqlQuery') }}</span>
              </div>
            </td>
            <td>
              <div class="protocols">
                <span v-if="isProtocolEnabled(service, 'rest_api')" class="badge badge-info">
                  REST API
                </span>
                <span v-if="isProtocolEnabled(service, 'ogc_features')" class="badge badge-info">
                  OGC Features
                </span>
              </div>
            </td>
            <td>
              <span class="badge" :class="service.public_access ? 'badge-success' : 'badge-warning'">
                {{ service.public_access ? $t('service.common.public') : $t('service.common.private') }}
              </span>
            </td>
            <td>
              <span class="badge" :class="statusClass(service.status)">
                {{ statusText(service.status) }}
              </span>
            </td>
            <td>{{ formatDate(service.created_at) }}</td>
            <td class="actions">
              <button @click="goToDetail(service.id)" class="btn btn-sm btn-info">{{ $t('service.common.detail') }}</button>
              <button @click="goToEdit(service.id)" class="btn btn-sm btn-warning">{{ $t('service.common.edit') }}</button>
              <button @click="confirmDelete(service.id)" class="btn btn-sm btn-danger">{{ $t('service.common.delete') }}</button>
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
import queryServiceAPI from '@/api/queryService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

export default {
  name: 'QueryServiceList',
  data() {
    return {
      services: [],
      searchQuery: '',
      page: 1,
      limit: 20,
      total: 0,
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

        const response = await queryServiceAPI.listServices(params)
        this.services = response.data || []
        this.total = response.total || 0
      } catch (error) {
        this.error = this.$t('service.query.loadFailed') + ': ' + (error.message || this.$t('service.common.unknownError'))
        console.error('Failed to load query services:', error)
        alert(this.error)
      } finally {
        this.loading = false
      }
    },

    handleSearch() {
      this.page = 1
      this.loadServices()
    },

    async confirmDelete(id) {
      if (!confirm(this.$t('service.query.deleteConfirm'))) {
        return
      }

      try {
        await queryServiceAPI.deleteService(id)
        alert(this.$t('service.query.deleteSuccess'))
        this.loadServices()
      } catch (error) {
        alert(this.$t('service.query.deleteFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to delete query service:', error)
      }
    },

    isProtocolEnabled(service, protocolName) {
      if (!service.protocols || !service.protocols[protocolName]) {
        return false
      }
      const protocol = service.protocols[protocolName]
      return protocol.enabled === true
    },

    configTypeText(configType) {
      const typeMap = {
        table: this.$t('service.query.configTypeTable'),
        sql: this.$t('service.query.configTypeSql')
      }
      return typeMap[configType] || configType
    },

    statusText(status) {
      const statusMap = {
        active: this.$t('service.query.statusActive'),
        inactive: this.$t('service.query.statusInactive'),
        error: this.$t('service.query.statusError')
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

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    },

    goToCreate() {
      navigateServiceRoute(this.$router, '/query-services/create')
    },

    goToDetail(id) {
      navigateServiceRoute(this.$router, `/query-services/${id}`)
    },

    goToEdit(id) {
      navigateServiceRoute(this.$router, `/query-services/${id}/edit`)
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
.query-service-list {
  padding: 20px;
}

h1 {
  margin-bottom: 20px;
  color: var(--addp-text-primary);
}

.toolbar {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  justify-content: space-between;
}

.search-bar {
  flex: 1;
  display: flex;
  gap: 10px;
}

.search-bar input {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  font-size: 14px;
  background: var(--addp-bg-primary) !important;
  color: var(--addp-text-primary);
}

.btn {
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.3s;
}

.btn-primary {
  background-color: #007bff;
  color: white;
}

.btn-primary:hover {
  background-color: #0056b3;
}

.btn-success {
  background-color: #28a745;
  color: white;
}

.btn-success:hover {
  background-color: #218838;
}

.btn-warning {
  background-color: #ffc107;
  color: var(--addp-text-primary);
}

.btn-warning:hover {
  background-color: #e0a800;
}

.btn-danger {
  background-color: #dc3545;
  color: white;
}

.btn-danger:hover {
  background-color: #c82333;
}

.btn-info {
  background-color: #17a2b8;
  color: white;
}

.btn-info:hover {
  background-color: #138496;
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
  background: var(--addp-bg-primary) !important;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.services-table th {
  background: var(--addp-bg-secondary) !important;
  padding: 12px;
  text-align: left;
  font-weight: 500;
  border-bottom: 2px solid var(--addp-border-color);
  font-size: 14px;
  color: var(--addp-text-primary);
}

.services-table td {
  padding: 12px;
  border-bottom: 1px solid var(--addp-border-color);
  font-size: 14px;
  color: var(--addp-text-primary);
}

.services-table tbody tr:hover {
  background: var(--addp-bg-secondary) !important;
}

.data-source {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.sql-indicator {
  font-style: italic;
  color: var(--addp-text-tertiary);
}

.protocols {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.badge {
  display: inline-block;
  padding: 4px 8px;
  border-radius: 3px;
  font-size: 12px;
  font-weight: 500;
}

.badge-info {
  background-color: #d1ecf1;
  color: #0c5460;
}

.badge-success {
  background-color: #d4edda;
  color: #155724;
}

.badge-warning {
  background-color: #fff3cd;
  color: #856404;
}

.badge-danger {
  background-color: #f8d7da;
  color: #721c24;
}

.badge-secondary {
  background-color: #e2e3e5;
  color: #383d41;
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
  background: var(--addp-bg-secondary) !important;
  border-radius: 4px;
}

.page-info {
  font-size: 14px;
  color: var(--addp-text-secondary);
}
</style>
