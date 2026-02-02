<template>
  <div class="published-service-list">
    <h1>服务发布</h1>

    <!-- 操作栏 -->
    <div class="toolbar">
      <div class="search-bar">
        <input
          v-model="searchQuery"
          type="text"
          placeholder="搜索已发布服务..."
          @keyup.enter="handleSearch"
        />
        <button @click="handleSearch" class="btn btn-primary">搜索</button>
      </div>
      <button @click="goToCreate" class="btn btn-success">+ 创建服务</button>
    </div>

    <!-- 服务列表 -->
    <div class="services-container">
      <div v-if="loading" class="loading">加载中...</div>
      <div v-else-if="services.length === 0" class="empty-state">
        <p>暂无服务</p>
      </div>
      <table v-else class="services-table">
        <thead>
          <tr>
            <th>服务名称</th>
            <th>标题</th>
            <th>图层数</th>
            <th>服务类型</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="service in services" :key="service.id">
            <td>{{ service.service_name }}</td>
            <td>{{ service.title }}</td>
            <td>{{ service.layers ? service.layers.length : 0 }}</td>
            <td>
              <div class="service-types">
                <span v-if="service.enabled_wfs" class="badge badge-info">WFS</span>
                <span v-if="service.enabled_ogc_api" class="badge badge-info">OGC API</span>
                <span v-if="service.enabled_wmts" class="badge badge-info">WMTS</span>
                <span v-if="service.enabled_wms" class="badge badge-info">WMS</span>
              </div>
            </td>
            <td>
              <span class="badge" :class="{ 'badge-success': service.status === 'active', 'badge-warning': service.status !== 'active' }">
                {{ statusText(service.status) }}
              </span>
            </td>
            <td>{{ formatDate(service.created_at) }}</td>
            <td class="actions">
              <button @click="goToDetail(service.id)" class="btn btn-sm btn-info">详情</button>
              <button @click="goToEdit(service.id)" class="btn btn-sm btn-warning">编辑</button>
              <button @click="goToTest(service.id)" class="btn btn-sm btn-secondary">测试</button>
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
import publishedServiceAPI from '@/api/publishedService'

export default {
  name: 'PublishedServiceList',
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

        const response = await publishedServiceAPI.listServices(params)
        this.services = response.data || []
        this.total = response.total || 0
      } catch (error) {
        this.error = '加载服务列表失败: ' + (error.message || '未知错误')
        console.error('Failed to load services:', error)
      } finally {
        this.loading = false
      }
    },

    handleSearch() {
      this.page = 1
      this.loadServices()
    },

    async confirmDelete(id) {
      if (!confirm('确定要删除此服务吗？')) {
        return
      }

      try {
        await publishedServiceAPI.deleteService(id)
        alert('服务已删除')
        this.loadServices()
      } catch (error) {
        alert('删除失败: ' + (error.message || '未知错误'))
        console.error('Failed to delete service:', error)
      }
    },

    goToCreate() {
      this.$router.push('/published-services/create')
    },

    goToDetail(id) {
      this.$router.push(`/published-services/${id}`)
    },

    goToEdit(id) {
      this.$router.push(`/published-services/${id}/edit`)
    },

    goToTest(id) {
      this.$router.push(`/published-services/${id}/test`)
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
    },

    statusText(status) {
      const statusMap = {
        active: '活跃',
        inactive: '非活跃',
        error: '错误'
      }
      return statusMap[status] || status
    },

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    }
  }
}
</script>

<style scoped>
.published-service-list {
  padding: 20px;
}

h1 {
  margin-bottom: 20px;
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

.btn-secondary {
  background-color: #6c757d;
  color: white;
}

.btn-secondary:hover {
  background-color: #5a6268;
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
  padding: 20px;
  color: #666;
}

.services-table {
  width: 100%;
  border-collapse: collapse;
  background-color: white;
}

.services-table th {
  background-color: #f8f9fa;
  padding: 12px;
  text-align: left;
  font-weight: 500;
  border-bottom: 2px solid #dee2e6;
}

.services-table td {
  padding: 12px;
  border-bottom: 1px solid #dee2e6;
}

.services-table tbody tr:hover {
  background-color: #f8f9fa;
}

.service-types {
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
