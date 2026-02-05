<template>
  <div class="registered-service-form">
    <div class="page-header">
      <button @click="goBack" class="btn btn-back">← 返回</button>
      <h2>{{ isEdit ? '编辑注册服务' : '注册外部服务' }}</h2>
    </div>

    <div v-if="loading" class="loading">加载中...</div>
    <form v-else @submit.prevent="handleSubmit" class="form-container">
      <!-- 基本信息 -->
      <div class="form-section">
        <h3>基本信息</h3>

        <div class="form-group">
          <label for="service_name" class="required">服务名称</label>
          <input
            id="service_name"
            v-model="form.service_name"
            type="text"
            placeholder="唯一标识，例如: geoserver_wms"
            required
            :disabled="isEdit"
          />
          <div class="help-text">用于 URL 路径，创建后不可修改</div>
        </div>

        <div class="form-group">
          <label for="title" class="required">标题</label>
          <input
            id="title"
            v-model="form.title"
            type="text"
            placeholder="服务的显示名称"
            required
          />
        </div>

        <div class="form-group">
          <label for="description">描述</label>
          <textarea
            id="description"
            v-model="form.description"
            rows="3"
            placeholder="服务的详细描述"
          />
        </div>

        <div class="form-group">
          <label for="keywords">关键词</label>
          <input
            id="keywords"
            v-model="keywordsInput"
            type="text"
            placeholder="用逗号分隔，例如: 地图,矢量,WMS"
          />
          <div class="help-text">便于搜索和分类</div>
        </div>
      </div>

      <!-- 服务配置 -->
      <div class="form-section">
        <h3>服务配置</h3>

        <div class="form-group">
          <label for="service_type" class="required">服务类型</label>
          <select
            id="service_type"
            v-model="form.service_type"
            required
            :disabled="isEdit"
          >
            <option value="">请选择服务类型</option>
            <option value="wms">WMS - Web Map Service</option>
            <option value="wfs">WFS - Web Feature Service</option>
            <option value="wmts">WMTS - Web Map Tile Service</option>
            <option value="ogc_api">OGC API Features</option>
            <option value="xyz">XYZ Tiles</option>
            <option value="rest">REST API</option>
          </select>
          <div class="help-text">创建后不可修改</div>
        </div>

        <div class="form-group">
          <label for="endpoint_url" class="required">服务端点 URL</label>
          <input
            id="endpoint_url"
            v-model="form.endpoint_url"
            type="url"
            placeholder="https://example.com/geoserver/wms"
            required
            :disabled="isEdit"
          />
          <div class="help-text">外部服务的完整访问地址，创建后不可修改</div>
        </div>

        <div class="form-group">
          <label for="health_check_url">健康检查 URL</label>
          <input
            id="health_check_url"
            v-model="form.health_check_url"
            type="url"
            placeholder="留空则使用服务端点 URL"
          />
          <div class="help-text">用于定期健康检查的端点</div>
        </div>

        <div v-if="isOGCService" class="form-group">
          <label class="checkbox-label">
            <input
              type="checkbox"
              v-model="form.auto_refresh_metadata"
              :disabled="isEdit"
            />
            自动刷新元数据（GetCapabilities）
          </label>
          <div class="help-text">创建时自动获取服务元数据和图层信息</div>
        </div>
      </div>

      <!-- 认证配置 -->
      <div class="form-section">
        <h3>认证配置</h3>

        <div class="form-group">
          <label for="auth_type" class="required">认证方式</label>
          <select id="auth_type" v-model="form.auth_type" required>
            <option value="none">无需认证</option>
            <option value="basic">Basic 认证</option>
            <option value="bearer">Bearer Token</option>
            <option value="api_key">API Key</option>
          </select>
        </div>

        <!-- Basic 认证配置 -->
        <div v-if="form.auth_type === 'basic'" class="auth-config">
          <div class="form-group">
            <label for="username" class="required">用户名</label>
            <input
              id="username"
              v-model="authConfig.username"
              type="text"
              placeholder="Basic 认证用户名"
              required
            />
          </div>
          <div class="form-group">
            <label for="password" class="required">密码</label>
            <input
              id="password"
              v-model="authConfig.password"
              type="password"
              placeholder="Basic 认证密码"
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
              placeholder="API Key 值"
              required
            />
          </div>
          <div class="form-group">
            <label for="api_key_name" class="required">参数名</label>
            <input
              id="api_key_name"
              v-model="authConfig.name"
              type="text"
              placeholder="例如: apiKey, api_key, token"
              required
            />
          </div>
          <div class="form-group">
            <label for="api_key_location" class="required">位置</label>
            <select id="api_key_location" v-model="authConfig.location" required>
              <option value="header">HTTP Header</option>
              <option value="query">Query Parameter</option>
            </select>
          </div>
        </div>
      </div>

      <!-- 操作按钮 -->
      <div class="form-actions">
        <button type="button" @click="goBack" class="btn btn-secondary">取消</button>
        <button type="submit" class="btn btn-primary" :disabled="submitting">
          {{ submitting ? '保存中...' : (isEdit ? '保存修改' : '创建服务') }}
        </button>
      </div>
    </form>
  </div>
</template>

<script>
import registeredServiceAPI from '@/api/registeredService'

export default {
  name: 'RegisteredServiceForm',
  data() {
    return {
      isEdit: false,
      serviceId: null,
      loading: false,
      submitting: false,
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
    }
  },
  computed: {
    isOGCService() {
      const ogcTypes = ['wms', 'wfs', 'wmts', 'ogc_api']
      return ogcTypes.includes(this.form.service_type)
    }
  },
  mounted() {
    const id = this.$route.params.id
    if (id && this.$route.path.includes('/edit')) {
      this.isEdit = true
      this.serviceId = parseInt(id)
      this.loadService()
    }
  },
  methods: {
    async loadService() {
      this.loading = true
      try {
        const response = await registeredServiceAPI.getService(this.serviceId)
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

        // 注意：不从服务器加载实际的认证凭据（安全考虑）
        // 如果需要更新认证信息，用户需要重新输入
      } catch (error) {
        alert('加载服务失败: ' + (error.message || '未知错误'))
        console.error('Failed to load service:', error)
        this.goBack()
      } finally {
        this.loading = false
      }
    },

    async handleSubmit() {
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

        if (this.isEdit) {
          // 编辑模式：只更新允许修改的字段
          const updateData = {
            title: data.title,
            description: data.description || null,
            keywords: data.keywords,
            auth_type: data.auth_type,
            auth_config: Object.keys(authConfig).length > 0 ? authConfig : null,
            health_check_url: data.health_check_url || null
          }
          await registeredServiceAPI.updateService(this.serviceId, updateData)
          alert('服务已更新')
        } else {
          // 创建模式
          await registeredServiceAPI.createService(data)
          alert('服务已创建')
        }

        this.goBack()
      } catch (error) {
        alert('保存失败: ' + (error.message || '未知错误'))
        console.error('Failed to save service:', error)
      } finally {
        this.submitting = false
      }
    },

    goBack() {
      this.$router.push('/registered-services')
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
  color: #333;
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

.loading {
  text-align: center;
  padding: 40px;
  color: #666;
}

.form-container {
  background-color: white;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  padding: 30px;
}

.form-section {
  margin-bottom: 30px;
  padding-bottom: 30px;
  border-bottom: 1px solid #e9ecef;
}

.form-section:last-of-type {
  border-bottom: none;
  margin-bottom: 0;
  padding-bottom: 0;
}

.form-section h3 {
  margin: 0 0 20px 0;
  color: #495057;
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
  color: #495057;
  font-size: 14px;
}

.form-group label.required::after {
  content: ' *';
  color: #dc3545;
}

.form-group input[type="text"],
.form-group input[type="url"],
.form-group input[type="password"],
.form-group select,
.form-group textarea {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid #ced4da;
  border-radius: 4px;
  font-size: 14px;
  font-family: inherit;
  transition: border-color 0.3s;
}

.form-group input:focus,
.form-group select:focus,
.form-group textarea:focus {
  outline: none;
  border-color: #007bff;
  box-shadow: 0 0 0 0.2rem rgba(0, 123, 255, 0.25);
}

.form-group input:disabled,
.form-group select:disabled {
  background-color: #e9ecef;
  cursor: not-allowed;
}

.form-group textarea {
  resize: vertical;
  min-height: 80px;
}

.help-text {
  margin-top: 6px;
  font-size: 12px;
  color: #6c757d;
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
  background-color: #f8f9fa;
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
  border-top: 1px solid #e9ecef;
}

.btn {
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
  background-color: #007bff;
  color: white;
}

.btn-primary:hover:not(:disabled) {
  background-color: #0056b3;
}

.btn-secondary {
  background-color: #6c757d;
  color: white;
}

.btn-secondary:hover {
  background-color: #545b62;
}
</style>
