<template>
  <div class="registered-service-detail">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <button @click="goBack" class="btn btn-back">← 返回</button>
        <h2>{{ service?.title || '加载中...' }}</h2>
        <span v-if="service" class="badge" :class="statusClass(service.status)">
          {{ statusText(service.status) }}
        </span>
      </div>
      <div class="header-right">
        <button @click="refreshMetadata" class="btn btn-primary" :disabled="loading || refreshing">
          {{ refreshing ? '刷新中...' : '刷新元数据' }}
        </button>
        <button @click="healthCheck" class="btn btn-success" :disabled="loading || checking">
          {{ checking ? '检查中...' : '健康检查' }}
        </button>
        <button @click="goToEdit" class="btn btn-warning" :disabled="loading">编辑</button>
        <button @click="handleDelete" class="btn btn-danger" :disabled="loading">删除</button>
      </div>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" class="loading">加载中...</div>

    <!-- 服务详情 -->
    <div v-else-if="service" class="detail-container">
      <!-- 基本信息卡片 -->
      <div class="card">
        <h3>基本信息</h3>
        <table class="detail-table">
          <tr>
            <td class="label">服务名称</td>
            <td><code>{{ service.service_name }}</code></td>
          </tr>
          <tr>
            <td class="label">标题</td>
            <td>{{ service.title }}</td>
          </tr>
          <tr>
            <td class="label">描述</td>
            <td>{{ service.description || '-' }}</td>
          </tr>
          <tr>
            <td class="label">关键词</td>
            <td>
              <span v-if="service.keywords && service.keywords.length > 0">
                <span v-for="kw in service.keywords" :key="kw" class="badge badge-info">
                  {{ kw }}
                </span>
              </span>
              <span v-else>-</span>
            </td>
          </tr>
          <tr>
            <td class="label">服务类型</td>
            <td>
              <span class="badge badge-primary">
                {{ serviceTypeText(service.service_type) }}
              </span>
            </td>
          </tr>
          <tr>
            <td class="label">创建时间</td>
            <td>{{ formatDate(service.created_at) }}</td>
          </tr>
          <tr>
            <td class="label">更新时间</td>
            <td>{{ formatDate(service.updated_at) }}</td>
          </tr>
        </table>
      </div>

      <!-- 服务端点卡片 -->
      <div class="card">
        <h3>服务端点</h3>
        <table class="detail-table">
          <tr>
            <td class="label">原始端点</td>
            <td>
              <div class="endpoint-box">
                <code>{{ service.endpoint_url }}</code>
                <button @click="copyToClipboard(service.endpoint_url)" class="btn btn-sm btn-secondary">
                  复制
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="service.endpoints?.proxy">
            <td class="label">代理端点</td>
            <td>
              <div class="endpoint-box">
                <code>{{ service.endpoints.proxy }}</code>
                <button @click="copyToClipboard(service.endpoints.proxy)" class="btn btn-sm btn-secondary">
                  复制
                </button>
              </div>
              <div class="help-text">通过 ADDP 代理访问，支持认证和审计</div>
            </td>
          </tr>
          <tr v-if="service.health_check_url">
            <td class="label">健康检查 URL</td>
            <td><code>{{ service.health_check_url }}</code></td>
          </tr>
        </table>
      </div>

      <!-- 认证配置卡片 -->
      <div class="card">
        <h3>认证配置</h3>
        <table class="detail-table">
          <tr>
            <td class="label">认证方式</td>
            <td>
              <span class="badge badge-secondary">
                {{ authTypeText(service.auth_type) }}
              </span>
            </td>
          </tr>
          <tr>
            <td class="label">认证凭据</td>
            <td>
              <span v-if="service.has_auth_config" class="badge badge-success">已配置</span>
              <span v-else class="badge badge-secondary">未配置</span>
            </td>
          </tr>
        </table>
      </div>

      <!-- 健康状态卡片 -->
      <div class="card">
        <h3>健康状态</h3>
        <table class="detail-table">
          <tr>
            <td class="label">当前状态</td>
            <td>
              <span class="badge" :class="statusClass(service.status)">
                {{ statusText(service.status) }}
              </span>
            </td>
          </tr>
          <tr v-if="service.error_message">
            <td class="label">错误信息</td>
            <td class="error-message">{{ service.error_message }}</td>
          </tr>
          <tr>
            <td class="label">最后检查时间</td>
            <td>{{ formatDate(service.last_checked_at) }}</td>
          </tr>
        </table>
      </div>

      <!-- 元数据信息卡片 -->
      <div v-if="service.metadata && Object.keys(service.metadata).length > 0" class="card">
        <h3>元数据信息</h3>
        <div class="metadata-box">
          <pre>{{ JSON.stringify(service.metadata, null, 2) }}</pre>
        </div>
      </div>

      <!-- 图层列表卡片 -->
      <div v-if="service.layers && service.layers.length > 0" class="card">
        <h3>图层列表</h3>
        <table class="layers-table">
          <thead>
            <tr>
              <th>图层名称</th>
              <th>显示名称</th>
              <th>几何类型</th>
              <th>坐标系</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="layer in service.layers" :key="layer.id">
              <td><code>{{ layer.layer_name }}</code></td>
              <td>{{ layer.display_name || '-' }}</td>
              <td>{{ layer.geometry_type || '-' }}</td>
              <td>{{ layer.crs || '-' }}</td>
              <td>
                <span class="badge" :class="layer.enabled ? 'badge-success' : 'badge-secondary'">
                  {{ layer.enabled ? '启用' : '禁用' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 错误状态 -->
    <div v-else class="error-state">
      <p>无法加载服务详情</p>
    </div>
  </div>
</template>

<script>
import registeredServiceAPI from '@/api/registeredService'
import { copyToClipboard as copyTextToClipboard } from '../utils/serviceHelper'

export default {
  name: 'RegisteredServiceDetail',
  data() {
    return {
      service: null,
      loading: false,
      refreshing: false,
      checking: false
    }
  },
  mounted() {
    this.loadService()
  },
  methods: {
    async loadService() {
      this.loading = true
      try {
        const id = this.$route.params.id
        const response = await registeredServiceAPI.getService(id)
        this.service = response
      } catch (error) {
        alert('加载服务失败: ' + (error.message || '未知错误'))
        console.error('Failed to load service:', error)
        this.goBack()
      } finally {
        this.loading = false
      }
    },

    async refreshMetadata() {
      if (!confirm('确定要刷新此服务的元数据吗？')) {
        return
      }

      this.refreshing = true
      try {
        await registeredServiceAPI.refreshMetadata(this.service.id, { force: true })
        alert('元数据刷新已触发，请稍后刷新页面查看结果')
        // 等待一段时间后重新加载
        setTimeout(() => {
          this.loadService()
        }, 2000)
      } catch (error) {
        alert('刷新失败: ' + (error.message || '未知错误'))
        console.error('Failed to refresh metadata:', error)
      } finally {
        this.refreshing = false
      }
    },

    async healthCheck() {
      this.checking = true
      try {
        const response = await registeredServiceAPI.healthCheck(this.service.id)
        const result = response
        alert(`健康检查完成\n\n状态: ${result.status}\n消息: ${result.message}\nHTTP 状态码: ${result.status_code || 'N/A'}\n响应时间: ${result.response_time}ms`)
        // 重新加载服务以更新健康状态
        this.loadService()
      } catch (error) {
        alert('健康检查失败: ' + (error.message || '未知错误'))
        console.error('Failed to perform health check:', error)
      } finally {
        this.checking = false
      }
    },

    async handleDelete() {
      if (!confirm('确定要删除此注册服务吗？删除后无法恢复，包括所有相关的图层信息。')) {
        return
      }

      try {
        await registeredServiceAPI.deleteService(this.service.id)
        alert('服务已删除')
        this.goBack()
      } catch (error) {
        alert('删除失败: ' + (error.message || '未知错误'))
        console.error('Failed to delete service:', error)
      }
    },

    async copyToClipboard(text) {
      const success = await copyTextToClipboard(text)
      if (success) {
        alert('已复制到剪贴板')
      } else {
        alert('复制失败，请手动复制')
      }
    },

    serviceTypeText(serviceType) {
      const typeMap = {
        wms: 'WMS - Web Map Service',
        wfs: 'WFS - Web Feature Service',
        wmts: 'WMTS - Web Map Tile Service',
        ogc_api: 'OGC API Features',
        xyz: 'XYZ Tiles',
        rest: 'REST API'
      }
      return typeMap[serviceType] || serviceType.toUpperCase()
    },

    authTypeText(authType) {
      const typeMap = {
        none: '无需认证',
        basic: 'Basic 认证',
        bearer: 'Bearer Token',
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

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    },

    goBack() {
      this.$router.push('/registered-services')
    },

    goToEdit() {
      this.$router.push(`/registered-services/${this.service.id}/edit`)
    }
  }
}
</script>

<style scoped>
.registered-service-detail {
  padding: 20px;
  max-width: 1200px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 30px;
  padding-bottom: 20px;
  border-bottom: 2px solid #e9ecef;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 15px;
}

.header-left h2 {
  margin: 0;
  color: #333;
}

.header-right {
  display: flex;
  gap: 10px;
}

.btn-back {
  padding: 8px 16px;
  background-color: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.3s;
}

.btn-back:hover {
  background-color: #e9ecef;
}

.btn {
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.3s;
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-primary {
  background-color: #007bff;
  color: white;
}

.btn-primary:hover:not(:disabled) {
  background-color: #0056b3;
}

.btn-success {
  background-color: #28a745;
  color: white;
}

.btn-success:hover:not(:disabled) {
  background-color: #218838;
}

.btn-warning {
  background-color: #ffc107;
  color: #333;
}

.btn-warning:hover:not(:disabled) {
  background-color: #e0a800;
}

.btn-danger {
  background-color: #dc3545;
  color: white;
}

.btn-danger:hover:not(:disabled) {
  background-color: #c82333;
}

.btn-secondary {
  background-color: #6c757d;
  color: white;
}

.btn-secondary:hover:not(:disabled) {
  background-color: #545b62;
}

.btn-sm {
  padding: 4px 8px;
  font-size: 12px;
}

.loading,
.error-state {
  text-align: center;
  padding: 40px 20px;
  color: #666;
}

.detail-container {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.card {
  background-color: white;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  padding: 24px;
}

.card h3 {
  margin: 0 0 20px 0;
  color: #495057;
  font-size: 18px;
  font-weight: 600;
  border-bottom: 2px solid #e9ecef;
  padding-bottom: 12px;
}

.detail-table {
  width: 100%;
  border-collapse: collapse;
}

.detail-table tr {
  border-bottom: 1px solid #e9ecef;
}

.detail-table tr:last-child {
  border-bottom: none;
}

.detail-table td {
  padding: 12px;
  font-size: 14px;
}

.detail-table td.label {
  width: 180px;
  font-weight: 500;
  color: #6c757d;
  background-color: #f8f9fa;
}

.endpoint-box {
  display: flex;
  align-items: center;
  gap: 10px;
}

.endpoint-box code {
  flex: 1;
  padding: 8px 12px;
  background-color: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 4px;
  font-size: 13px;
  word-break: break-all;
}

.help-text {
  margin-top: 6px;
  font-size: 12px;
  color: #6c757d;
}

.error-message {
  color: #dc3545;
  font-style: italic;
}

.metadata-box {
  background-color: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
}

.metadata-box pre {
  margin: 0;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.5;
  color: #495057;
}

.layers-table {
  width: 100%;
  border-collapse: collapse;
}

.layers-table thead {
  background-color: #f8f9fa;
}

.layers-table th,
.layers-table td {
  padding: 12px;
  text-align: left;
  border-bottom: 1px solid #dee2e6;
  font-size: 14px;
}

.layers-table th {
  font-weight: 500;
  color: #495057;
}

.layers-table tbody tr:hover {
  background-color: #f8f9fa;
}

.badge {
  display: inline-block;
  padding: 4px 8px;
  border-radius: 3px;
  font-size: 12px;
  font-weight: 500;
  margin-right: 5px;
}

.badge-primary {
  background-color: #cfe2ff;
  color: #084298;
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

code {
  padding: 2px 6px;
  background-color: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #e83e8c;
}
</style>
