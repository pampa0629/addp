<template>
  <div class="tile-service-list">
    <h1>{{ $t('service.tile.listTitle') }}</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          :placeholder="$t('service.tile.searchPlaceholder')"
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">{{ $t('service.common.search') }}</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">{{ $t('service.tile.createBtn') }}</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">{{ $t('service.common.loading') }}</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>{{ $t('service.tile.emptyText') }}</p>
        <p class="tip">{{ $t('service.tile.emptyTip') }}</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>{{ $t('service.tile.colServiceName') }}</th>
            <th>{{ $t('service.tile.colTitle') }}</th>
            <th>{{ $t('service.tile.colLayers') }}</th>
            <th>{{ $t('service.tile.colProtocols') }}</th>
            <th>{{ $t('service.tile.colAccess') }}</th>
            <th>{{ $t('service.tile.colStatus') }}</th>
            <th>{{ $t('service.tile.colCreatedAt') }}</th>
            <th>{{ $t('service.tile.colActions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="service in services" :key="service.id">
            <td><strong>{{ service.service_name }}</strong></td>
            <td>{{ service.title }}</td>
            <td>
              <span class="badge badge-info">
                {{ $t('service.serviceCard.layerCount', { count: service.layers ? service.layers.length : 0 }) }}
              </span>
            </td>
            <td>
              <div class="protocols">
                <span v-if="isProtocolEnabled(service, 'xyz')" class="badge badge-info" title="XYZ Tiles">
                  XYZ
                </span>
                <span v-if="isProtocolEnabled(service, 'ogc_tiles')" class="badge badge-info" title="OGC Tiles API">
                  OGC
                </span>
                <span v-if="isProtocolEnabled(service, 'tms')" class="badge badge-info" title="TMS">
                  TMS
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
import tileServiceAPI from '@/api/tileService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

export default {
  name: 'TileServiceList',
  data() {
    return {
      services: [],
      searchQuery: '',
      page: 1,
      limit: 20,
      total: 0,
      loading: false
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
      try {
        const params = {
          offset: (this.page - 1) * this.limit,
          limit: this.limit
        }

        if (this.searchQuery) {
          params.keyword = this.searchQuery
        }

        const response = await tileServiceAPI.listServices(params)
        console.log('[TileServiceList] API 响应:', response)

        // createAPIClient 的 extractData=true 已自动提取 .data
        // response 结构: { data: [...], total: 8, limit: 20, offset: 0 }
        this.services = response.data || []
        this.total = response.total || 0

        console.log('[TileServiceList] 加载了', this.services.length, '个服务，共', this.total, '个')
      } catch (error) {
        console.error('加载瓦片服务失败:', error)
        alert(this.$t('service.tile.loadFailed') + ': ' + (error.response?.data?.error || error.message))
      } finally {
        this.loading = false
      }
    },

    handleSearch() {
      this.page = 1
      this.loadServices()
    },

    goToCreate() {
      navigateServiceRoute(this.$router, '/tile/create')
    },

    goToDetail(id) {
      navigateServiceRoute(this.$router, `/tile/${id}`)
    },

    async confirmDelete(id) {
      const service = this.services.find(s => s.id === id)
      if (!confirm(this.$t('service.tile.deleteConfirm', { name: service.service_name }))) {
        return
      }

      try {
        await tileServiceAPI.deleteService(id)
        alert(this.$t('service.tile.deleteSuccess'))
        this.loadServices()
      } catch (error) {
        console.error('删除服务失败:', error)
        alert(this.$t('service.tile.deleteFailed') + ': ' + (error.response?.data?.error || error.message))
      }
    },

    isProtocolEnabled(service, protocolName) {
      if (!service.protocols || !service.protocols[protocolName]) {
        return false
      }
      const protocol = service.protocols[protocolName]
      return protocol.enabled === true
    },

    statusClass(status) {
      const statusMap = {
        active: 'badge-success',
        inactive: 'badge-secondary',
        error: 'badge-danger'
      }
      return statusMap[status] || 'badge-secondary'
    },

    statusText(status) {
      const statusMap = {
        active: this.$t('service.tile.statusActive'),
        inactive: this.$t('service.tile.statusInactive'),
        error: this.$t('service.tile.statusError')
      }
      return statusMap[status] || status
    },

    formatDate(dateString) {
      if (!dateString) return '-'
      const date = new Date(dateString)
      return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
      })
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
.tile-service-list {
  padding: 20px;
}

h1 {
  margin-bottom: 20px;
  color: var(--addp-text-primary);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  padding: 15px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.search-bar {
  display: flex;
  gap: 10px;
}

.search-bar input {
  padding: 8px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  width: 300px;
  font-size: 14px;
  background: var(--addp-bg-primary) !important;
  color: var(--addp-text-primary);
}

.services-container {
  background: var(--addp-bg-primary) !important;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  min-height: 200px;
}

.loading,
.empty-state {
  text-align: center;
  padding: 60px 20px;
  color: var(--addp-text-tertiary);
}

.empty-state .tip {
  font-size: 14px;
  color: #ccc;
  margin-top: 10px;
}

.services-table {
  width: 100%;
  border-collapse: collapse;
}

.services-table th {
  background: var(--addp-bg-secondary) !important;
  padding: 12px;
  text-align: left;
  font-weight: 600;
  color: var(--addp-text-primary);
  border-bottom: 2px solid var(--addp-border-color);
}

.services-table td {
  padding: 12px;
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
}

.services-table tbody tr:hover {
  background: var(--addp-bg-secondary) !important;
}

.protocols {
  display: flex;
  gap: 5px;
  flex-wrap: wrap;
}

.badge {
  display: inline-block;
  padding: 4px 8px;
  font-size: 12px;
  border-radius: 3px;
  font-weight: 500;
}

.badge-info {
  background: #e3f2fd;
  color: #1976d2;
}

.badge-success {
  background: #e8f5e9;
  color: #388e3c;
}

.badge-warning {
  background: #fff3e0;
  color: #f57c00;
}

.badge-danger {
  background: #ffebee;
  color: #d32f2f;
}

.badge-secondary {
  background: var(--addp-bg-secondary);
  color: #757575;
}

.actions {
  display: flex;
  gap: 8px;
}

.pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 15px;
  margin-top: 20px;
  padding: 15px;
  background: var(--addp-bg-secondary) !important;
  border-radius: 4px;
}

.page-info {
  color: var(--addp-text-secondary);
  font-size: 14px;
}

/* 按钮样式 */
.btn {
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background: #1976d2;
  color: white;
}

.btn-primary:hover:not(:disabled) {
  background: #1565c0;
}

.btn-success {
  background: #388e3c;
  color: white;
}

.btn-success:hover {
  background: #2e7d32;
}

.btn-sm {
  padding: 6px 12px;
  font-size: 12px;
}

.btn-info {
  background: #0288d1;
  color: white;
}

.btn-info:hover {
  background: #0277bd;
}

.btn-warning {
  background: #f57c00;
  color: white;
}

.btn-warning:hover {
  background: #ef6c00;
}

.btn-danger {
  background: #d32f2f;
  color: white;
}

.btn-danger:hover {
  background: #c62828;
}
</style>
