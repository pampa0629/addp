<template>
  <div class="query-service-list">
    <h1>查询服务</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          placeholder="搜索查询服务..."
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">搜索</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">+ 创建查询服务</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">加载中...</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>暂无查询服务</p>
        <p class="tip">点击"创建查询服务"按钮开始创建</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>服务名称</th>
            <th>标题</th>
            <th>配置方式</th>
            <th>数据源</th>
            <th>协议支持</th>
            <th>访问控制</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>操作</th>
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
                <span v-else class="sql-indicator">SQL 查询</span>
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
                {{ service.public_access ? '公开' : '私有' }}
              </span>
            </td>
            <td>
              <span class="badge" :class="statusClass(service.status)">
                {{ statusText(service.status) }}
              </span>
            </td>
            <td>{{ formatDate(service.created_at) }}</td>
            <td class="actions">
              <button @click="goToDetail(service.id)" class="btn btn-sm btn-info">详情</button>
              <button @click="goToEdit(service.id)" class="btn btn-sm btn-warning">编辑</button>
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
import queryServiceAPI from '@/api/queryService'

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
        this.error = '加载查询服务列表失败: ' + (error.message || '未知错误')
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
      if (!confirm('确定要删除此查询服务吗？删除后无法恢复。')) {
        return
      }

      try {
        await queryServiceAPI.deleteService(id)
        alert('查询服务已删除')
        this.loadServices()
      } catch (error) {
        alert('删除失败: ' + (error.message || '未知错误'))
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
        table: '表配置',
        sql: 'SQL配置'
      }
      return typeMap[configType] || configType
    },

    statusText(status) {
      const statusMap = {
        active: '活跃',
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

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    },

    goToCreate() {
      this.$router.push('/query-services/create')
    },

    goToDetail(id) {
      this.$router.push(`/query-services/${id}`)
    },

    goToEdit(id) {
      this.$router.push(`/query-services/${id}/edit`)
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
  color: #333;
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
  color: #333;
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
  color: #666;
}

.empty-state p {
  margin: 10px 0;
}

.empty-state .tip {
  font-size: 14px;
  color: #999;
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

.data-source {
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #495057;
}

.sql-indicator {
  font-style: italic;
  color: #6c757d;
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
  background-color: #f8f9fa;
  border-radius: 4px;
}

.page-info {
  font-size: 14px;
  color: #666;
}
</style>
