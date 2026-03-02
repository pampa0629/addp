<template>
  <div class="registered-service-list">
    <h1>注册服务</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          placeholder="搜索注册服务..."
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">搜索</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">+ 注册外部服务</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">加载中...</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>暂无注册服务</p>
        <p class="tip">点击"注册外部服务"按钮开始注册</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>服务名称</th>
            <th>标题</th>
            <th>服务类型</th>
            <th>服务端点</th>
            <th>认证方式</th>
            <th>健康状态</th>
            <th>最后检查</th>
            <th>创建时间</th>
            <th>操作</th>
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
              <button @click="goToDetail(service.id)" class="btn btn-sm btn-info">详情</button>
              <button @click="goToEdit(service.id)" class="btn btn-sm btn-warning">编辑</button>
              <button @click="refreshMetadata(service.id)" class="btn btn-sm btn-primary">刷新</button>
              <button @click="healthCheck(service.id)" class="btn btn-sm btn-success">检查</button>
              <button @click="confirmDelete(service.id)" class="btn btn-sm btn-danger">删除</button>
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
        上一页
      </button>
      <span class="page-info">第 {{ page }} 页，共 {{ totalPages }} 页（共 {{ total }} 项）</span>
      <button
        @click="nextPage"
        :disabled="page >= totalPages"
        class="btn btn-sm"
      >
        下一页
      </button>
    </div>
  </div>
</template>

<script>
import registeredServiceAPI from '@/api/registeredService'

export default {
  name: 'RegisteredServiceList',
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

        const response = await registeredServiceAPI.listServices(params)
        this.services = response.data || []
        this.total = response.total || 0
      } catch (error) {
        this.error = '加载注册服务列表失败: ' + (error.message || '未知错误')
        console.error('Failed to load registered services:', error)
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
      if (!confirm('确定要删除此注册服务吗？删除后无法恢复。')) {
        return
      }

      try {
        await registeredServiceAPI.deleteService(id)
        alert('注册服务已删除')
        this.loadServices()
      } catch (error) {
        alert('删除失败: ' + (error.message || '未知错误'))
        console.error('Failed to delete registered service:', error)
      }
    },

    async refreshMetadata(id) {
      try {
        await registeredServiceAPI.refreshMetadata(id, { force: false })
        alert('元数据刷新已触发')
        this.loadServices()
      } catch (error) {
        alert('刷新失败: ' + (error.message || '未知错误'))
        console.error('Failed to refresh metadata:', error)
      }
    },

    async healthCheck(id) {
      try {
        const response = await registeredServiceAPI.healthCheck(id)
        const result = response
        alert(`健康检查完成\n状态: ${result.status}\n消息: ${result.message}\n响应时间: ${result.response_time}ms`)
        this.loadServices()
      } catch (error) {
        alert('健康检查失败: ' + (error.message || '未知错误'))
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
        none: '无需认证',
        basic: 'Basic',
        bearer: 'Bearer',
        api_key: 'API Key'
      }
      return typeMap[authType] || authType
    },

    statusText(status) {
      const statusMap = {
        active: '正常',
        inactive: '非活跃',
        error: '错误'
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
      this.$router.push('/registered-services/create')
    },

    goToDetail(id) {
      this.$router.push(`/registered-services/${id}`)
    },

    goToEdit(id) {
      this.$router.push(`/registered-services/${id}/edit`)
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
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 14px;
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
  background-color: white;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.services-table th {
  background-color: #f8f9fa;
  padding: 12px;
  text-align: left;
  font-weight: 500;
  border-bottom: 2px solid #dee2e6;
  font-size: 14px;
}

.services-table td {
  padding: 12px;
  border-bottom: 1px solid #dee2e6;
  font-size: 14px;
}

.services-table tbody tr:hover {
  background-color: #f8f9fa;
}

.endpoint-url {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #495057;
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
  background-color: #f8f9fa;
  border-radius: 4px;
}

.page-info {
  font-size: 14px;
  color: var(--addp-text-secondary);
}
</style>
