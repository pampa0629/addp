<template>
  <div class="published-service-form" v-loading="loading">
    <div class="page-header">
      <h2>{{ isEdit ? '编辑服务' : '创建服务' }}</h2>
    </div>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <!-- 第一部分：基本信息 -->
      <el-card header="基本信息" style="margin-bottom: 20px;">
        <el-form-item label="服务名称" prop="service_name" required>
          <el-input
            v-model="form.service_name"
            placeholder="例如: city_poi"
            :disabled="isEdit"
          />
          <div class="help-text">仅支持英文、数字和下划线，用于 OGC 服务 URL</div>
        </el-form-item>

        <el-form-item label="标题" prop="title" required>
          <el-input v-model="form.title" placeholder="例如: 城市兴趣点服务" />
        </el-form-item>

        <el-form-item label="摘要" prop="abstract">
          <el-input type="textarea" v-model="form.abstract" :rows="3" placeholder="服务的简要描述" />
        </el-form-item>

        <el-form-item label="关键词">
          <div class="keyword-input">
            <el-tag
              v-for="tag in form.keywords"
              :key="tag"
              closable
              @close="removeKeyword(tag)"
              style="margin-right: 8px; margin-bottom: 8px;"
            >
              {{ tag }}
            </el-tag>
            <el-input
              v-if="inputVisible"
              v-model="inputValue"
              ref="inputRef"
              size="small"
              style="width: 120px;"
              @keyup.enter="handleInputConfirm"
              @blur="handleInputConfirm"
            />
            <el-button v-else size="small" @click="showInput">+ 添加关键词</el-button>
          </div>
        </el-form-item>
      </el-card>

      <!-- 第二部分：协议配置 -->
      <el-card header="协议配置" style="margin-bottom: 20px;">
        <el-form-item label="启用协议">
          <div style="display: flex; flex-direction: column; gap: 12px;">
            <div>
              <el-checkbox v-model="form.enabled_ogc_api">
                OGC API - Features
                <el-tooltip content="现代RESTful矢量要素服务，推荐使用" placement="right">
                  <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
                </el-tooltip>
              </el-checkbox>
            </div>

            <div>
              <el-checkbox v-model="form.enabled_wfs">
                WFS 2.0
                <el-tooltip content="传统OGC矢量要素服务，兼容QGIS、ArcGIS" placement="right">
                  <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
                </el-tooltip>
              </el-checkbox>
            </div>

            <div>
              <el-checkbox v-model="form.enabled_wmts">
                WMTS（矢量瓦片）
                <el-tooltip content="高性能矢量瓦片服务，适合大数据可视化" placement="right">
                  <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
                </el-tooltip>
              </el-checkbox>
            </div>

            <div>
              <el-checkbox v-model="form.enabled_rest_query">
                REST Query API
                <el-tooltip content="简化REST查询API，适合前端快速集成" placement="right">
                  <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
                </el-tooltip>
              </el-checkbox>
            </div>

            <div>
              <el-checkbox v-model="form.enabled_wms" disabled>
                WMS 1.3（规划中）
                <el-tooltip content="地图图片渲染服务，即将支持" placement="right">
                  <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
                </el-tooltip>
              </el-checkbox>
            </div>
          </div>
          <el-alert
            type="info"
            show-icon
            :closable="false"
            style="margin-top: 12px;"
          >
            说明：图层包含几何列时可启用空间服务协议（WFS/WMTS/OGC API），非空间数据只能启用REST Query API
          </el-alert>
        </el-form-item>

        <el-form-item label="访问权限">
          <el-checkbox v-model="form.public_access">
            允许公开访问（无需JWT认证）
            <el-tooltip content="启用后，任何人都可以访问此服务的公开端点" placement="right">
              <el-icon style="margin-left: 4px; cursor: help;"><QuestionFilled /></el-icon>
            </el-tooltip>
          </el-checkbox>
        </el-form-item>

        <el-form-item label="默认坐标系" prop="default_srid">
          <el-select v-model="form.default_srid" style="width: 300px;">
            <el-option :value="4326" label="EPSG:4326 (WGS84)" />
            <el-option :value="3857" label="EPSG:3857 (Web Mercator)" />
            <el-option :value="4490" label="EPSG:4490 (CGCS2000)" />
            <el-option :value="4214" label="EPSG:4214 (Beijing 1954)" />
          </el-select>
        </el-form-item>

        <el-form-item label="最大要素数" prop="max_features">
          <el-input-number v-model="form.max_features" :min="1" :max="10000" />
          <div class="help-text">单次查询返回的最大要素数量</div>
        </el-form-item>
      </el-card>

      <!-- 第三部分：元数据（可选） -->
      <el-card header="元数据（可选）" style="margin-bottom: 20px;">
        <el-form-item label="提供者名称">
          <el-input v-model="form.provider_name" placeholder="例如: ADDP 数据平台" />
        </el-form-item>

        <el-form-item label="提供者网站">
          <el-input v-model="form.provider_site" placeholder="https://example.com" />
        </el-form-item>

        <el-form-item label="联系人">
          <el-input v-model="form.contact_person" placeholder="例如: 张三" />
        </el-form-item>

        <el-form-item label="联系邮箱">
          <el-input v-model="form.contact_email" type="email" placeholder="example@example.com" />
        </el-form-item>
      </el-card>

      <!-- 第四部分：存储引擎 -->
      <el-card header="存储引擎" style="margin-bottom: 20px;">
        <el-form-item label="选择引擎" prop="engine_id" required>
          <el-select v-model="form.engine_id" placeholder="请选择 PostgreSQL 引擎" style="width: 400px;">
            <el-option
              v-for="engine in engines"
              :key="engine.id"
              :label="`${engine.name} (${engine.engine_type})`"
              :value="engine.id"
              :disabled="engine.engine_type !== 'postgresql'"
            />
          </el-select>
          <div class="help-text">当前仅支持 PostgreSQL 引擎</div>
        </el-form-item>
      </el-card>

      <!-- 提交按钮 -->
      <el-form-item>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? '更新' : '创建' }}
        </el-button>
        <el-button @click="$router.back()">取消</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import publishedServiceAPI from '../api/publishedService'
import client from '../api/client'

const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const inputRef = ref(null)

const isEdit = computed(() => !!route.params.id)

const form = ref({
  service_name: '',
  title: '',
  abstract: '',
  keywords: [],
  enabled_wfs: true,
  enabled_ogc_api: true,
  enabled_wmts: true,
  enabled_wms: false,
  enabled_rest_query: true,
  public_access: false,
  default_srid: 4326,
  max_features: 1000,
  provider_name: '',
  provider_site: '',
  contact_person: '',
  contact_email: '',
  engine_id: null
})

const engines = ref([])
const loading = ref(false)
const submitting = ref(false)
const inputVisible = ref(false)
const inputValue = ref('')

// 表单验证规则
const rules = {
  service_name: [
    { required: true, message: '请输入服务名称', trigger: 'blur' },
    {
      pattern: /^[a-zA-Z0-9_]+$/,
      message: '仅支持英文、数字和下划线',
      trigger: 'blur'
    }
  ],
  title: [
    { required: true, message: '请输入标题', trigger: 'blur' }
  ],
  engine_id: [
    { required: true, message: '请选择存储引擎', trigger: 'change' }
  ],
  default_srid: [
    { required: true, message: '请选择默认坐标系', trigger: 'change' }
  ],
  max_features: [
    { required: true, message: '请输入最大要素数', trigger: 'blur' },
    { type: 'number', min: 1, max: 10000, message: '值必须在 1 到 10000 之间', trigger: 'blur' }
  ]
}

// 加载存储引擎列表
const loadEngines = async () => {
  try {
    const res = await client.get('/service/engines')
    engines.value = res || []
  } catch (error) {
    ElMessage.error('加载引擎列表失败: ' + (error.message || '未知错误'))
  }
}

// 加载服务详情（编辑模式）
const loadService = async () => {
  if (!isEdit.value) return

  loading.value = true
  try {
    // createAPIClient 默认 extractData=true，已经提取了 response.data
    const service_data = await publishedServiceAPI.getService(route.params.id)
    // 将后端返回的数据赋值到表单
    Object.assign(form.value, service_data)
    // 确保 keywords 是数组
    if (!Array.isArray(form.value.keywords)) {
      form.value.keywords = []
    }
  } catch (error) {
    ElMessage.error('加载服务失败: ' + (error.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

// 关键词管理
const removeKeyword = (tag) => {
  form.value.keywords = form.value.keywords.filter(k => k !== tag)
}

const showInput = () => {
  inputVisible.value = true
  nextTick(() => {
    inputRef.value?.focus()
  })
}

const handleInputConfirm = () => {
  const value = inputValue.value.trim()
  if (value && !form.value.keywords.includes(value)) {
    form.value.keywords.push(value)
  }
  inputValue.value = ''
  inputVisible.value = false
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    // 验证表单
    await formRef.value.validate()

    submitting.value = true

    if (isEdit.value) {
      await publishedServiceAPI.updateService(route.params.id, form.value)
      ElMessage.success('更新成功')
    } else {
      await publishedServiceAPI.createService(form.value)
      ElMessage.success('创建成功')
    }

    router.push('/published-services')
  } catch (error) {
    if (error.message) {
      ElMessage.error('操作失败: ' + error.message)
    }
    // 表单验证失败会自动提示，无需额外处理
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  loadEngines()
  loadService()
})
</script>

<style scoped>
.published-service-form {
  padding: 20px;
}

.page-header {
  margin-bottom: 20px;
}

.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: #303133;
}

.help-text {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
  line-height: 1.5;
}

.keyword-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

:deep(.el-card__header) {
  font-weight: 600;
  font-size: 16px;
}
</style>
