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
        <button @click="refreshMetadata" class="btn btn-primary" :disabled="!service || loading || refreshing">
          {{ refreshing ? $t('service.registered.refreshing') : $t('service.registered.refreshMetadata') }}
        </button>
        <button @click="healthCheck" class="btn btn-success" :disabled="!service || loading || checking">
          {{ checking ? $t('service.registered.checking') : $t('service.registered.healthCheck') }}
        </button>
        <button @click="goToEdit" class="btn btn-warning" :disabled="!service || loading">{{ $t('service.common.edit') }}</button>
        <button @click="handleDelete" class="btn btn-danger" :disabled="!service || loading || deleting">{{ $t('service.common.delete') }}</button>
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
          <tbody>
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
          </tbody>
        </table>
      </div>

      <!-- 服务端点卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.colEndpoint') }}</h3>
        <table class="detail-table">
          <tbody>
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
          </tbody>
        </table>
      </div>

      <!-- 认证配置卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.sectionAuthConfig') }}</h3>
        <table class="detail-table">
          <tbody>
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
          </tbody>
        </table>
      </div>

      <!-- 健康状态卡片 -->
      <div class="card">
        <h3>{{ $t('service.registered.healthStatusTitle') }}</h3>
        <table class="detail-table">
          <tbody>
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
          </tbody>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import registeredServiceAPI from '@/api/registeredService'
import { copyToClipboard as copyTextToClipboard } from '../utils/serviceHelper'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { publishConsolePageDescriptor } from '@common-ui'

export default {
  name: 'RegisteredServiceDetail',
  watch: {
    '$route.params.id': {
      immediate: true,
      handler() {
        this.detailEpoch++
        this.service = null
        this.deleting = false
        this.refreshing = false
        this.checking = false
        return this.loadService()
      }
    },
    '$i18n.locale'() {
      this.publishPageDescriptor()
    }
  },
  data() {
    return {
      service: null,
      detailEpoch: 0,
      loadSequence: 0,
      deleting: false,
      loading: false,
      refreshing: false,
      checking: false
    }
  },
  beforeUnmount() {
    this.detailEpoch++
    this.loadSequence++
  },
  methods: {
    isCurrentService(id, epoch) {
      return this.detailEpoch === epoch && String(this.$route.params.id) === String(id)
    },
    publishPageDescriptor() {
      if (!this.service) return
      publishConsolePageDescriptor(this.$router, 'service', {
        title: this.$t('service.registered.recentVisitTitle'),
        subject: this.service.title || this.service.name || ''
      }).catch(() => {})
    },
    async loadService() {
      const id = this.$route.params.id
      const epoch = this.detailEpoch
      const sequence = ++this.loadSequence
      const isCurrent = () => this.isCurrentService(id, epoch) && sequence === this.loadSequence
      this.loading = true
      try {
        const response = await registeredServiceAPI.getService(id)
        if (!isCurrent()) return
        this.service = response
        this.publishPageDescriptor()
      } catch (error) {
        if (!isCurrent()) return
        ElMessage.error(this.$t('service.registered.loadFailed2') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to load service:', error)
        this.goBack()
      } finally {
        if (isCurrent()) this.loading = false
      }
    },

    async refreshMetadata() {
      if (!this.service || this.loading || this.refreshing) return
      const id = this.service.id
      const epoch = this.detailEpoch
      const isCurrent = () => this.isCurrentService(id, epoch)
      if (!isCurrent()) return
      this.refreshing = true
      try {
        await ElMessageBox.confirm(
          this.$t('service.registered.refreshConfirm'),
          this.$t('service.registered.refreshMetadata'),
          {
            type: 'warning', customClass: 'addp-message-box',
            confirmButtonText: this.$t('service.common.refresh'),
            cancelButtonText: this.$t('service.common.cancel'),
            distinguishCancelAndClose: true
          }
        )
        if (!isCurrent()) return
        await registeredServiceAPI.refreshMetadata(id, { force: true })
        if (!isCurrent()) return
        ElMessage.success(this.$t('service.registered.refreshSuccess'))
        await this.loadService()
      } catch (error) {
        if (!isCurrent()) return
        if (error === 'cancel' || error === 'close') return
        ElMessage.error(this.$t('service.registered.refreshFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to refresh metadata:', error)
      } finally {
        if (isCurrent()) this.refreshing = false
      }
    },

    async healthCheck() {
      if (!this.service || this.loading || this.checking) return
      const id = this.service.id
      const epoch = this.detailEpoch
      const isCurrent = () => this.isCurrentService(id, epoch)
      if (!isCurrent()) return
      this.checking = true
      try {
        const response = await registeredServiceAPI.healthCheck(id)
        if (!isCurrent()) return
        const result = response
        ElMessage({
          message: this.$t('service.registered.healthCheckResult', { status: result.status, message: result.message, time: result.response_time }),
          type: result.status === 'healthy' ? 'success' : 'warning',
          showClose: true,
          duration: 6000
        })
        // 重新加载服务以更新健康状态
        await this.loadService()
      } catch (error) {
        if (!isCurrent()) return
        ElMessage.error(this.$t('service.registered.healthCheckFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to perform health check:', error)
      } finally {
        if (isCurrent()) this.checking = false
      }
    },

    async handleDelete() {
      if (!this.service || this.loading || this.deleting) return
      const id = this.service.id
      const epoch = this.detailEpoch
      const isCurrent = () => this.isCurrentService(id, epoch)
      if (!isCurrent()) return
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
        if (!isCurrent()) return
        await registeredServiceAPI.deleteService(id)
        if (!isCurrent()) return
        ElMessage.success(this.$t('service.registered.deleteSuccess'))
        await this.goBack()
      } catch (error) {
        if (!isCurrent()) return
        if (error === 'cancel' || error === 'close') return
        ElMessage.error(this.$t('service.registered.deleteFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to delete service:', error)
      } finally {
        if (isCurrent()) this.deleting = false
      }
    },

    async copyToClipboard(text) {
      const success = await copyTextToClipboard(text)
      if (success) {
        ElMessage.success(this.$t('service.common.copied'))
      } else {
        ElMessage.error(this.$t('service.common.copyFailed'))
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
      navigateServiceRoute(this.$router, '/services', { history: 'replace' })
    },

    goToEdit() {
      if (!this.service || this.loading || !this.isCurrentService(this.service.id, this.detailEpoch)) return
      navigateServiceRoute(this.$router, `/services/${this.service.id}/edit`)
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
  flex-wrap: wrap;
  gap: 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 30px;
  padding-bottom: 20px;
  border-bottom: 2px solid var(--addp-border-color);
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
  flex-wrap: wrap;
  display: flex;
  gap: 10px;
}

.btn-back {
  color: var(--addp-text-primary);
  padding: 8px 16px;
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.3s;
}

.btn-back:hover {
  background-color: var(--addp-bg-secondary);
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

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-primary {
  background-color: var(--el-color-primary);
  color: var(--el-color-white);
}

.btn-primary:hover:not(:disabled) {
  background-color: var(--el-color-primary-dark-2);
}

.btn-success {
  background-color: var(--el-color-success);
  color: var(--el-color-white);
}

.btn-success:hover:not(:disabled) {
  background-color: var(--el-color-success-dark-2);
}

.btn-warning {
  background-color: var(--el-color-warning);
  color: var(--el-color-white);
}

.btn-warning:hover:not(:disabled) {
  background-color: var(--el-color-warning-dark-2);
}

.btn-danger {
  background-color: var(--el-color-danger);
  color: var(--el-color-white);
}

.btn-danger:hover:not(:disabled) {
  background-color: var(--el-color-danger-dark-2);
}

.btn-secondary {
  background-color: var(--el-color-info);
  color: var(--el-color-white);
}

.btn-secondary:hover:not(:disabled) {
  background-color: var(--el-color-info-dark-2);
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
  background-color: var(--addp-bg-primary);
  border-radius: 8px;
  box-shadow: var(--el-box-shadow-light);
  padding: 24px;
}

.card h3 {
  margin: 0 0 20px 0;
  color: var(--addp-text-primary);
  font-size: 18px;
  font-weight: 600;
  border-bottom: 2px solid var(--addp-border-color);
  padding-bottom: 12px;
}

.detail-table {
  width: 100%;
  border-collapse: collapse;
}

.detail-table tr {
  border-bottom: 1px solid var(--addp-border-color);
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
  color: var(--addp-text-secondary);
  background-color: var(--addp-bg-secondary);
}

.endpoint-box {
  display: flex;
  align-items: center;
  gap: 10px;
}

.endpoint-box code {
  flex: 1;
  padding: 8px 12px;
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  font-size: 13px;
  word-break: break-all;
}

.help-text {
  margin-top: 6px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.error-message {
  color: var(--el-color-danger);
  font-style: italic;
}

.metadata-box {
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
}

.metadata-box pre {
  margin: 0;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.5;
  color: var(--addp-text-primary);
}

.layers-table {
  width: 100%;
  border-collapse: collapse;
}

.layers-table thead {
  background-color: var(--addp-bg-secondary);
}

.layers-table th,
.layers-table td {
  padding: 12px;
  text-align: left;
  border-bottom: 1px solid var(--addp-border-color);
  font-size: 14px;
}

.layers-table th {
  font-weight: 500;
  color: var(--addp-text-primary);
}

.layers-table tbody tr:hover {
  background-color: var(--addp-bg-secondary);
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
  background-color: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
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

code {
  padding: 2px 6px;
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--el-color-primary);
}
</style>
