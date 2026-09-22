<template>
  <div class="registered-service-form">
    <div class="page-header">
      <button @click="goBack" class="btn btn-back">← {{ $t('service.common.back') }}</button>
      <h2>{{ isEdit ? $t('service.registered.formEditTitle') : $t('service.registered.formCreateTitle') }}</h2>
    </div>

    <div v-if="loading" class="loading">{{ $t('service.common.loading') }}</div>
    <form v-else @submit.prevent="handleSubmit" class="form-container">
      <fieldset :disabled="submitting" class="form-fields">
      <!-- 基本信息 -->
      <div class="form-section">
        <h3>{{ $t('service.registered.sectionBasicInfo') }}</h3>

        <div class="form-group">
          <label for="service_name" class="required">{{ $t('service.registered.serviceNameLabel') }}</label>
          <input
            id="service_name"
            v-model="form.service_name"
            type="text"
            :placeholder="$t('service.registered.serviceNamePlaceholder')"
            required
            :disabled="isEdit"
          />
          <div class="help-text">{{ $t('service.registered.serviceNameHelp') }}</div>
        </div>

        <div class="form-group">
          <label for="title" class="required">{{ $t('service.registered.titleLabel') }}</label>
          <input
            id="title"
            v-model="form.title"
            type="text"
            :placeholder="$t('service.registered.titlePlaceholder')"
            required
          />
        </div>

        <div class="form-group">
          <label for="description">{{ $t('service.registered.descriptionLabel') }}</label>
          <textarea
            id="description"
            v-model="form.description"
            rows="3"
            :placeholder="$t('service.registered.descriptionPlaceholder')"
          />
        </div>

        <div class="form-group">
          <label for="keywords">{{ $t('service.registered.keywordsLabel') }}</label>
          <input
            id="keywords"
            v-model="keywordsInput"
            type="text"
            :placeholder="$t('service.registered.keywordsPlaceholder')"
          />
          <div class="help-text">{{ $t('service.registered.keywordsHelp') }}</div>
        </div>
      </div>

      <!-- 服务配置 -->
      <div class="form-section">
        <h3>{{ $t('service.registered.sectionServiceConfig') }}</h3>

        <div class="form-group">
          <label for="service_type" class="required">{{ $t('service.registered.serviceTypeLabel') }}</label>
          <select
            id="service_type"
            v-model="form.service_type"
            required
            :disabled="isEdit"
          >
            <option value="">{{ $t('service.registered.serviceTypePlaceholder') }}</option>
            <option value="wms">WMS - Web Map Service</option>
            <option value="wfs">WFS - Web Feature Service</option>
            <option value="wmts">WMTS - Web Map Tile Service</option>
            <option value="ogc_api">OGC API Features</option>
            <option value="xyz">XYZ Tiles</option>
            <option value="rest">REST API</option>
          </select>
          <div class="help-text">{{ $t('service.registered.serviceTypeHelp') }}</div>
        </div>

        <div class="form-group">
          <label for="endpoint_url" class="required">{{ $t('service.registered.endpointUrlLabel') }}</label>
          <input
            id="endpoint_url"
            v-model="form.endpoint_url"
            type="url"
            :placeholder="$t('service.registered.endpointUrlPlaceholder')"
            required
            :disabled="isEdit"
          />
          <div class="help-text">{{ $t('service.registered.endpointUrlHelp') }}</div>
        </div>

        <div class="form-group">
          <label for="health_check_url">{{ $t('service.registered.healthCheckUrlLabel') }}</label>
          <input
            id="health_check_url"
            v-model="form.health_check_url"
            type="url"
            :placeholder="$t('service.registered.healthCheckUrlPlaceholder')"
          />
          <div class="help-text">{{ $t('service.registered.healthCheckUrlHelp') }}</div>
        </div>

        <div v-if="isOGCService" class="form-group">
          <label class="checkbox-label">
            <input
              type="checkbox"
              v-model="form.auto_refresh_metadata"
              :disabled="isEdit"
            />
            {{ $t('service.registered.autoRefreshMetadata') }}
          </label>
          <div class="help-text">{{ $t('service.registered.autoRefreshMetadataHelp') }}</div>
        </div>
      </div>

      <!-- 认证配置 -->
      <div class="form-section">
        <h3>{{ $t('service.registered.sectionAuthConfig') }}</h3>

        <div class="form-group">
          <label for="auth_type" class="required">{{ $t('service.registered.authTypeLabel') }}</label>
          <select id="auth_type" v-model="form.auth_type" required>
            <option value="none">{{ $t('service.registered.authNone') }}</option>
            <option value="basic">Basic {{ $t('service.registered.authBasic') }}</option>
            <option value="bearer">Bearer Token</option>
            <option value="api_key">API Key</option>
          </select>
        </div>

        <!-- Basic 认证配置 -->
        <div v-if="form.auth_type === 'basic'" class="auth-config">
          <div class="form-group">
            <label for="username" class="required">{{ $t('service.registered.usernameLabel') }}</label>
            <input
              id="username"
              v-model="authConfig.username"
              type="text"
              :placeholder="$t('service.registered.usernamePlaceholder')"
              required
            />
          </div>
          <div class="form-group">
            <label for="password" class="required">{{ $t('service.registered.passwordLabel') }}</label>
            <input
              id="password"
              v-model="authConfig.password"
              type="password"
              :placeholder="$t('service.registered.passwordPlaceholder')"
              required
            />
          </div>
        </div>

        <!-- Bearer Token 配置 -->
        <div v-if="form.auth_type === 'bearer'" class="auth-config">
          <div class="form-group">
            <label for="token" class="required">Token</label>
            <input
              id="token"
              v-model="authConfig.token"
              type="password"
              placeholder="Bearer Token"
              required
            />
          </div>
        </div>

        <!-- API Key 配置 -->
        <div v-if="form.auth_type === 'api_key'" class="auth-config">
          <div class="form-group">
            <label for="api_key" class="required">API Key</label>
            <input
              id="api_key"
              v-model="authConfig.key"
              type="password"
              :placeholder="$t('service.registered.apiKeyPlaceholder')"
              required
            />
          </div>
          <div class="form-group">
            <label for="api_key_name" class="required">{{ $t('service.registered.apiKeyNameLabel') }}</label>
            <input
              id="api_key_name"
              v-model="authConfig.name"
              type="text"
              :placeholder="$t('service.registered.apiKeyNamePlaceholder')"
              required
            />
          </div>
          <div class="form-group">
            <label for="api_key_location" class="required">{{ $t('service.registered.apiKeyLocationLabel') }}</label>
            <select id="api_key_location" v-model="authConfig.location" required>
              <option value="header">HTTP Header</option>
              <option value="query">Query Parameter</option>
            </select>
          </div>
        </div>
      </div>

      <!-- 操作按钮 -->
      <div class="form-actions">
        <button type="button" @click="goBack" class="btn btn-secondary">{{ $t('service.common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="submitting">
          {{ submitting ? $t('service.registered.saving') : (isEdit ? $t('service.registered.saveChanges') : $t('service.registered.createService')) }}
        </button>
      </div>
      </fieldset>
    </form>
  </div>
</template>

<script>
import { reactive, ref, toRefs } from 'vue'
import { useRouter } from 'vue-router'
import { useUnsavedChangesGuard } from '@common-ui'
import { ElMessage } from 'element-plus'
import registeredServiceAPI from '@/api/registeredService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const createDraft = () => ({
  form: {
    service_name: '',
    title: '',
    description: '',
    keywords: [],
    service_type: '',
    endpoint_url: '',
    health_check_url: '',
    auth_type: 'none',
    auth_config: {},
    auto_refresh_metadata: true
  },
  authConfig: {
    username: '',
    password: '',
    token: '',
    key: '',
    name: '',
    location: 'header'
  },
  keywordsInput: ''
})

export default {
  name: 'RegisteredServiceForm',
  setup() {
    const draft = reactive(createDraft())
    const captureDraft = () => JSON.stringify(draft)
    const savedDraft = ref(captureDraft())
    const markSaved = (snapshot = captureDraft()) => { savedDraft.value = snapshot }
    const resetDraft = () => {
      Object.assign(draft, createDraft())
      markSaved()
    }
    useUnsavedChangesGuard({
      router: useRouter(),
      isDirty: () => captureDraft() !== savedDraft.value,
      shouldConfirmUpdate: (to, from) => to.name !== from.name || to.params.id !== from.params.id
    })
    return { ...toRefs(draft), captureDraft, markSaved, resetDraft }
  },
  data() {
    return {
      isEdit: false,
      serviceId: null,
      loading: false,
      submitting: false,
      loadVersion: 0
    }
  },
  computed: {
    isOGCService() {
      const ogcTypes = ['wms', 'wfs', 'wmts', 'ogc_api']
      return ogcTypes.includes(this.form.service_type)
    }
  },
  watch: {
    '$route.path': {
      immediate: true,
      handler() {
        this.loadVersion++
        this.resetDraft()
        this.loading = false
        this.submitting = false
        const id = this.$route.params.id
        this.isEdit = Boolean(id && this.$route.path.endsWith('/edit'))
        this.serviceId = this.isEdit ? Number(id) : null
        if (this.isEdit) return this.loadService()
      }
    }
  },
  beforeUnmount() {
    this.loadVersion++
  },
  methods: {
    async loadService() {
      const version = ++this.loadVersion
      this.loading = true
      try {
        const response = await registeredServiceAPI.getService(this.serviceId)
        if (version !== this.loadVersion) return
        const service = response

        this.form = {
          service_name: service.service_name,
          title: service.title,
          description: service.description || '',
          keywords: service.keywords || [],
          service_type: service.service_type,
          endpoint_url: service.endpoint_url,
          health_check_url: service.health_check_url || '',
          auth_type: service.auth_type || 'none',
          auth_config: {},
          auto_refresh_metadata: false
        }

        this.keywordsInput = (service.keywords || []).join(', ')
        this.markSaved()

        // 注意：不从服务器加载实际的认证凭据（安全考虑）
        // 如果需要更新认证信息，用户需要重新输入
      } catch (error) {
        if (version !== this.loadVersion) return
        ElMessage.error(this.$t('service.registered.loadFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to load service:', error)
        this.goBack()
      } finally {
        if (version === this.loadVersion) this.loading = false
      }
    },

    async handleSubmit() {
      if (this.loading || this.submitting) return
      const version = this.loadVersion
      const path = this.$route.path
      const serviceId = this.serviceId
      const isEdit = this.isEdit
      const isCurrent = () => version === this.loadVersion && path === this.$route.path
      const submittedDraft = this.captureDraft()
      this.submitting = true
      try {
        // 处理关键词
        const keywords = this.keywordsInput
          .split(',')
          .map(k => k.trim())
          .filter(k => k.length > 0)

        // 构建认证配置
        let authConfig = {}
        if (this.form.auth_type === 'basic') {
          authConfig = {
            username: this.authConfig.username,
            password: this.authConfig.password
          }
        } else if (this.form.auth_type === 'bearer') {
          authConfig = {
            token: this.authConfig.token
          }
        } else if (this.form.auth_type === 'api_key') {
          authConfig = {
            key: this.authConfig.key,
            name: this.authConfig.name,
            location: this.authConfig.location
          }
        }

        const data = {
          ...this.form,
          keywords,
          auth_config: authConfig
        }

        if (isEdit) {
          // 编辑模式：只更新允许修改的字段
          const updateData = {
            title: data.title,
            description: data.description || null,
            keywords: data.keywords,
            auth_type: data.auth_type,
            auth_config: Object.keys(authConfig).length > 0 ? authConfig : null,
            health_check_url: data.health_check_url || null
          }
          await registeredServiceAPI.updateService(serviceId, updateData)
          if (!isCurrent()) return
          this.markSaved(submittedDraft)
          ElMessage.success(this.$t('service.registered.updateSuccess'))
          await navigateServiceRoute(this.$router, `/services/${serviceId}`, { history: 'replace' })
        } else {
          // 创建模式
          const created = await registeredServiceAPI.createService(data)
          if (!isCurrent()) return
          this.markSaved(submittedDraft)
          ElMessage.success(this.$t('service.registered.createSuccess'))
          await navigateServiceRoute(this.$router, `/services/${created.id}`, { history: 'replace' })
        }
      } catch (error) {
        if (!isCurrent()) return
        ElMessage.error(this.$t('service.registered.saveFailed') + ': ' + (error.message || this.$t('service.common.unknownError')))
        console.error('Failed to save service:', error)
      } finally {
        if (isCurrent()) this.submitting = false
      }
    },

    goBack() {
      return navigateServiceRoute(this.$router, '/services', { history: 'replace' })
    }
  }
}
</script>

<style scoped>
.registered-service-form {
  padding: 20px;
  max-width: 900px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  align-items: center;
  gap: 15px;
  margin-bottom: 30px;
}

.page-header h2 {
  margin: 0;
  color: var(--addp-text-primary);
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

.loading {
  text-align: center;
  padding: 40px;
  color: var(--addp-text-secondary);
}

.form-container {
  background-color: var(--addp-bg-primary);
  border-radius: 8px;
  box-shadow: var(--el-box-shadow-light);
  padding: 30px;
}

.form-fields {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}

.form-section {
  margin-bottom: 30px;
  padding-bottom: 30px;
  border-bottom: 1px solid var(--addp-border-color);
}

.form-section:last-of-type {
  border-bottom: none;
  margin-bottom: 0;
  padding-bottom: 0;
}

.form-section h3 {
  margin: 0 0 20px 0;
  color: var(--addp-text-primary);
  font-size: 18px;
  font-weight: 600;
}

.form-group {
  margin-bottom: 20px;
}

.form-group label {
  display: block;
  margin-bottom: 8px;
  font-weight: 500;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.form-group label.required::after {
  content: ' *';
  color: var(--el-color-danger);
}

.form-group input[type="text"],
.form-group input[type="url"],
.form-group input[type="password"],
.form-group select,
.form-group textarea {
  color: var(--addp-text-primary);
  background-color: var(--addp-bg-primary);
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  font-size: 14px;
  font-family: inherit;
  transition: border-color 0.3s;
}

.form-group input:focus,
.form-group select:focus,
.form-group textarea:focus {
  outline: none;
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 0.2rem var(--el-color-primary-light-8);
}

.form-group input:disabled,
.form-group select:disabled {
  background-color: var(--addp-bg-secondary);
  cursor: not-allowed;
}

.form-group textarea {
  resize: vertical;
  min-height: 80px;
}

.help-text {
  margin-top: 6px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.checkbox-label input[type="checkbox"] {
  width: auto;
  cursor: pointer;
}

.auth-config {
  background-color: var(--addp-bg-secondary);
  padding: 20px;
  border-radius: 4px;
  margin-top: 15px;
}

.form-actions {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  margin-top: 30px;
  padding-top: 20px;
  border-top: 1px solid var(--addp-border-color);
}

.btn {
  color: var(--addp-text-primary);
  background-color: var(--addp-bg-secondary);
  padding: 10px 24px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 500;
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

.btn-secondary {
  background-color: var(--el-color-info);
  color: var(--el-color-white);
}

.btn-secondary:hover {
  background-color: var(--el-color-info-dark-2);
}
</style>
