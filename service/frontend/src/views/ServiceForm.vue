<template>
  <div class="service-form-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <h3>{{ isEdit ? '编辑服务' : '创建服务' }}</h3>
          <el-button @click="handleBack">返回</el-button>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
        style="max-width: 800px"
      >
        <el-form-item label="服务名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入服务名称" />
        </el-form-item>

        <el-form-item label="服务描述" prop="description">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            placeholder="请输入服务描述"
          />
        </el-form-item>

        <el-form-item label="服务类型" prop="service_type">
          <el-select v-model="form.service_type" placeholder="请选择服务类型" style="width: 100%">
            <el-option label="WMS (Web Map Service)" value="wms" />
            <el-option label="WFS (Web Feature Service)" value="wfs" />
            <el-option label="WMTS (Web Map Tile Service)" value="wmts" />
            <el-option label="OGC API" value="ogc_api" />
            <el-option label="Data API" value="data_api" />
            <el-option label="REST API" value="rest" />
          </el-select>
        </el-form-item>

        <el-form-item label="服务地址" prop="url">
          <el-input v-model="form.url" placeholder="请输入服务地址 (http:// 或 https://)" />
        </el-form-item>

        <el-form-item label="认证类型" prop="auth_type">
          <el-radio-group v-model="form.auth_type">
            <el-radio value="none">无认证</el-radio>
            <el-radio value="basic">Basic Auth</el-radio>
            <el-radio value="bearer">Bearer Token</el-radio>
            <el-radio value="api_key">API Key</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="form.auth_type === 'basic'" label="用户名" prop="auth_username">
          <el-input v-model="form.auth_username" placeholder="请输入用户名" />
        </el-form-item>

        <el-form-item v-if="form.auth_type === 'basic'" label="密码" prop="auth_password">
          <el-input
            v-model="form.auth_password"
            type="password"
            placeholder="请输入密码"
            show-password
          />
        </el-form-item>

        <el-form-item v-if="form.auth_type === 'bearer'" label="Token" prop="auth_token">
          <el-input
            v-model="form.auth_token"
            type="textarea"
            :rows="3"
            placeholder="请输入 Bearer Token"
          />
        </el-form-item>

        <el-form-item v-if="form.auth_type === 'api_key'" label="API Key" prop="auth_api_key">
          <el-input v-model="form.auth_api_key" placeholder="请输入 API Key" />
        </el-form-item>

        <el-form-item v-if="form.auth_type === 'api_key'" label="Key 名称" prop="auth_key_name">
          <el-input v-model="form.auth_key_name" placeholder="例如: X-API-Key" />
        </el-form-item>

        <el-form-item label="健康检查地址" prop="health_check">
          <el-input v-model="form.health_check" placeholder="可选，留空将使用默认检查方式" />
        </el-form-item>

        <el-form-item label="状态" prop="status">
          <el-radio-group v-model="form.status">
            <el-radio value="active">活跃</el-radio>
            <el-radio value="inactive">未激活</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="submitting" @click="handleSubmit">
            {{ isEdit ? '更新' : '创建' }}
          </el-button>
          <el-button @click="handleBack">取消</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import serviceAPI from '../api/service'

const router = useRouter()
const route = useRoute()
const formRef = ref(null)
const submitting = ref(false)

const isEdit = computed(() => !!route.params.id)

const form = ref({
  name: '',
  description: '',
  service_type: '',
  url: '',
  auth_type: 'none',
  auth_username: '',
  auth_password: '',
  auth_token: '',
  auth_api_key: '',
  auth_key_name: 'X-API-Key',
  health_check: '',
  status: 'active'
})

const rules = {
  name: [{ required: true, message: '请输入服务名称', trigger: 'blur' }],
  service_type: [{ required: true, message: '请选择服务类型', trigger: 'change' }],
  url: [
    { required: true, message: '请输入服务地址', trigger: 'blur' },
    { type: 'url', message: '请输入有效的URL', trigger: 'blur' }
  ],
  auth_username: [
    { required: true, message: '请输入用户名', trigger: 'blur', validator: (rule, value, callback) => {
      if (form.value.auth_type === 'basic' && !value) {
        callback(new Error('请输入用户名'))
      }
      callback()
    }}
  ],
  auth_password: [
    { required: true, message: '请输入密码', trigger: 'blur', validator: (rule, value, callback) => {
      if (form.value.auth_type === 'basic' && !value) {
        callback(new Error('请输入密码'))
      }
      callback()
    }}
  ],
  auth_token: [
    { required: true, message: '请输入 Token', trigger: 'blur', validator: (rule, value, callback) => {
      if (form.value.auth_type === 'bearer' && !value) {
        callback(new Error('请输入 Token'))
      }
      callback()
    }}
  ],
  auth_api_key: [
    { required: true, message: '请输入 API Key', trigger: 'blur', validator: (rule, value, callback) => {
      if (form.value.auth_type === 'api_key' && !value) {
        callback(new Error('请输入 API Key'))
      }
      callback()
    }}
  ]
}

const loadService = async () => {
  if (!isEdit.value) return

  try {
    const service = await serviceAPI.get(route.params.id)

    // 填充表单数据
    form.value = {
      name: service.name || '',
      description: service.description || '',
      service_type: service.service_type || '',
      url: service.url || '',
      auth_type: service.auth_type || 'none',
      health_check: service.health_check_url || '',
      status: service.status || 'active',
      // 认证信息不回显（安全考虑）
      auth_username: '',
      auth_password: '',
      auth_token: '',
      auth_api_key: '',
      auth_key_name: 'X-API-Key'
    }
  } catch (error) {
    ElMessage.error('加载服务信息失败: ' + (error.response?.data?.message || error.message))
    handleBack()
  }
}

const handleSubmit = async () => {
  try {
    await formRef.value.validate()
    submitting.value = true

    // 后端 API 期望的字段名
    const submitData = {
      service_name: form.value.name,           // 后端: service_name
      title: form.value.name,                  // 后端: title (必填，使用 name 作为 title)
      description: form.value.description || '',
      keywords: [],                            // 后端: keywords (可选)
      service_type: form.value.service_type,
      endpoint_url: form.value.url,            // 后端: endpoint_url
      auth_type: form.value.auth_type,
      health_check_url: form.value.health_check || '',
      auto_refresh_metadata: false             // 后端: auto_refresh_metadata (可选)
    }

    // 根据认证类型构建 auth_config
    if (form.value.auth_type === 'basic') {
      submitData.auth_config = {
        username: form.value.auth_username,
        password: form.value.auth_password
      }
    } else if (form.value.auth_type === 'bearer') {
      submitData.auth_config = {
        token: form.value.auth_token
      }
    } else if (form.value.auth_type === 'api_key') {
      submitData.auth_config = {
        key: form.value.auth_api_key,
        name: form.value.auth_key_name,        // 后端: name
        location: 'header'                     // 后端: location
      }
    } else {
      submitData.auth_config = {}              // 无认证时也要提供空对象
    }

    if (isEdit.value) {
      await serviceAPI.update(route.params.id, submitData)
      ElMessage.success('更新成功')
    } else {
      await serviceAPI.create(submitData)
      ElMessage.success('创建成功')
    }

    handleBack()
  } catch (error) {
    if (error && error.response) {
      ElMessage.error(error.response?.data?.message || '操作失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleBack = () => {
  router.push('/services')
}

onMounted(() => {
  loadService()
})
</script>

<style scoped>
.service-form-container {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}
</style>
