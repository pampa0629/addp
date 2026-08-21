<template>
  <div class="registered-service-detail">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <button @click="goBack" class="btn btn-back">← {{ $t('service.common.back') }}</button>
        <h2>{{ service?.title || $t('service.common.loading') }}</h2>
        <span v-if="service" class="badge" :class="statusClass(service.status)">
          {{ statusText(service.status) }}
        </span>
      </div>
      <div class="header-right">
        <button @click="refreshMetadata" class="btn btn-primary" :disabled="loading || refreshing">
          {{ refreshing ? $t('service.registered.refreshing') : $t('service.registered.refreshMetadata') }}
        </button>
        <button @click="healthCheck" class="btn btn-success" :disabled="loading || checking">
          {{ checking ? $t('service.registered.checking') : $t('service.registered.healthCheck') }}
        </button>
        <button @click="goToEdit" class="btn btn-warning" :disabled="loading">{{ $t('service.common.edit') }}</button>
        <button @click="handleDelete" class="btn btn-danger" :disabled="loading">{{ $t('service.common.delete') }}</button>
      </div>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" class="loading">{{ $t('service.common.loading') }}</div>

    <!-- 服务详情 -->
    <div v-else-if="service" class="detail-container">
      <!-- 基本信息卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.sectionBasicInfo') }}</h3>
        <table class="detail-table">
          <tr>
            <td class="label">{{ $t('service.registered.serviceNameLabel') }}</td>
            <td><code>{{ service.service_name }}</code></td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.titleLabel') }}</td>
            <td>{{ service.title }}</td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.descriptionLabel') }}</td>
            <td>{{ service.description || '-' }}</td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.keywordsLabel') }}</td>
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
            <td class="label">{{ $t('service.registered.serviceTypeLabel') }}</td>
            <td>
              <span class="badge badge-primary">
                {{ serviceTypeText(service.service_type) }}
              </span>
            </td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.colCreatedAt') }}</td>
            <td>{{ formatDate(service.created_at) }}</td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.updatedAt') }}</td>
            <td>{{ formatDate(service.updated_at) }}</td>
          </tr>
        </table>
      </div>

      <!-- 服务端点卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.colEndpoint') }}</h3>
        <table class="detail-table">
          <tr>
            <td class="label">{{ $t('service.registered.originalEndpoint') }}</td>
            <td>
              <div class="endpoint-box">
                <code>{{ service.endpoint_url }}</code>
                <button @click="copyToClipboard(service.endpoint_url)" class="btn btn-sm btn-secondary">
                  {{ $t('service.common.copy') }}
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="service.endpoints?.proxy">
            <td class="label">{{ $t('service.registered.proxyEndpoint') }}</td>
            <td>
              <div class="endpoint-box">
                <code>{{ service.endpoints.proxy }}</code>
                <button @click="copyToClipboard(service.endpoints.proxy)" class="btn btn-sm btn-secondary">
                  {{ $t('service.common.copy') }}
                </button>
              </div>
              <div class="help-text">{{ $t('service.registered.proxyHelp') }}</div>
            </td>
          </tr>
          <tr v-if="service.health_check_url">
            <td class="label">{{ $t('service.registered.healthCheckUrlLabel') }}</td>
            <td><code>{{ service.health_check_url }}</code></td>
          </tr>
        </table>
      </div>

      <!-- 认证配置卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.sectionAuthConfig') }}</h3>
        <table class="detail-table">
          <tr>
            <td class="label">{{ $t('service.registered.authTypeLabel') }}</td>
            <td>
              <span class="badge badge-secondary">
                {{ authTypeText(service.auth_type) }}
              </span>
            </td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.authCredentials') }}</td>
            <td>
              <span v-if="service.has_auth_config" class="badge badge-success">{{ $t('service.registered.configured') }}</span>
              <span v-else class="badge badge-secondary">{{ $t('service.common.notConfigured') }}</span>
            </td>
          </tr>
        </table>
      </div>

      <!-- 健康状态卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.healthStatusTitle') }}</h3>
        <table class="detail-table">
          <tr>
            <td class="label">{{ $t('service.registered.currentStatus') }}</td>
            <td>
              <span class="badge" :class="statusClass(service.status)">
                {{ statusText(service.status) }}
              </span>
            </td>
          </tr>
          <tr v-if="service.error_message">
            <td class="label">{{ $t('service.registered.errorMessage') }}</td>
            <td class="error-message">{{ service.error_message }}</td>
          </tr>
          <tr>
            <td class="label">{{ $t('service.registered.colLastChecked') }}</td>
            <td>{{ formatDate(service.last_checked_at) }}</td>
          </tr>
        </table>
      </div>

      <!-- 元数据信息卡片 -->
      <div v-if="service.metadata && Object.keys(service.metadata).length > 0" class="card">
        <h3>{{ $t('service.registered.metadataTitle') }}</h3>
        <div class="metadata-box">
          <pre>{{ JSON.stringify(service.metadata, null, 2) }}</pre>
        </div>
      </div>

      <!-- 图层列表卡片 -->
      <div v-if="service.layers && service.layers.length > 0" class="card">
        <h3>{{ $t('service.registered.layersTitle') }}</h3>
        <table class="layers-table">
          <thead>
            <tr>
              <th>{{ $t('service.registered.layerName') }}</th>
              <th>{{ $t('service.registered.layerDisplayName') }}</th>
              <th>{{ $t('service.registered.layerGeomType') }}</th>
              <th>{{ $t('service.registered.layerCrs') }}</th>
              <th>{{ $t('service.registered.layerStatus') }}</th>
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
                  {{ layer.enabled ? $t('service.registered.layerEnabled') : $t('service.registered.layerDisabled') }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 错误状态 -->
    <div v-else class="error-state">
      <p>{{ $t('service.registered.loadDetailFailed') }}</p>
    </div>
  </div>
</template>

<script>
import registeredServiceAPI from '@/api/registeredService'
import { copyToClipboard as copyTextToClipboard } from '../utils/serviceHelper'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { publishConsolePageDescriptor } from '@common-ui'

export default {
  name: 'RegisteredServiceDetail',
  watch: {
    '$i18n.locale'() {
      this.publishPageDescriptor()
    }
  },
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
    publishPageDescriptor() {
      if (!this.service) return
      publishConsolePageDescriptor(this.$router, 'service', {
        title: this.$t('service.registered.recentVisitTitle'),
        subject: this.service.title || this.service.name || ''
      }).catch(() => {})
    },
    async loadService() {
      this.loading = true
      try {
        const id = this.$route.params.id
        const response = await registeredServiceAPI.getService(id)
        this.service = response
        this.publishPageDescriptor()
      } catch (error) {
        alert(this.$t('service.registered.loadFailed2') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to load service:', error)
        this.goBack()
      } finally {
        this.loading = false
      }
    },

    async refreshMetadata() {
      if (!confirm(this.$t('service.registered.refreshConfirm'))) {
        return
      }

      this.refreshing = true
      try {
        await registeredServiceAPI.refreshMetadata(this.service.id, { force: true })
        alert(this.$t('service.registered.refreshSuccess'))
        // 等待一段时间后重新加载
        setTimeout(() => {
          this.loadService()
        }, 2000)
      } catch (error) {
        alert(this.$t('service.registered.refreshFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
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
        alert(this.$t('service.registered.healthCheckResult', { status: result.status, message: result.message, time: result.response_time }))
        // 重新加载服务以更新健康状态
        this.loadService()
      } catch (error) {
        alert(this.$t('service.registered.healthCheckFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to perform health check:', error)
      } finally {
        this.checking = false
      }
    },

    async handleDelete() {
      if (!confirm(this.$t('service.registered.deleteConfirm'))) {
        return
      }

      try {
        await registeredServiceAPI.deleteService(this.service.id)
        alert(this.$t('service.registered.deleteSuccess'))
        this.goBack()
      } catch (error) {
        alert(this.$t('service.registered.deleteFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to delete service:', error)
      }
    },

    async copyToClipboard(text) {
      const success = await copyTextToClipboard(text)
      if (success) {
        alert(this.$t('service.common.copied'))
      } else {
        alert(this.$t('service.common.copyFailed'))
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
        none: this.$t('service.registered.authNone'),
        basic: 'Basic Auth',
        bearer: 'Bearer Token',
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

    formatDate(dateString) {
      if (!dateString) return '-'
      return new Date(dateString).toLocaleString('zh-CN')
    },

    goBack() {
      navigateServiceRoute(this.$router, '/registered-services', { history: 'replace' })
    },

    goToEdit() {
      navigateServiceRoute(this.$router, `/registered-services/${this.service.id}/edit`)
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
  color: var(--addp-text-primary);
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
  color: var(--addp-text-primary);
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
  color: var(--addp-text-secondary);
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
