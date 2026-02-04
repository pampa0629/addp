<template>
  <div class="published-service-form" v-loading="loading">
    <div class="page-header">
      <h2>{{ isEdit ? '编辑服务' : '创建服务' }}</h2>
    </div>

    <!-- 步骤条（仅新建模式显示） -->
    <el-steps
      v-if="!isEdit"
      :active="currentStep"
      finish-status="success"
      align-center
      style="margin-bottom: 30px"
    >
      <el-step title="选择数据表" />
      <el-step title="确认服务类型" />
      <el-step title="配置服务信息" />
    </el-steps>

    <!-- Step 0: 选择数据表（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 0">
      <el-card>
        <template #header>
          <span>选择要发布的数据表</span>
        </template>

        <el-button type="primary" size="large" @click="showTableSelector = true">
          <el-icon style="margin-right: 8px"><FolderOpened /></el-icon>
          选择数据表
        </el-button>

        <el-alert
          v-if="selectedTable"
          type="success"
          :closable="false"
          style="margin-top: 20px"
        >
          <template #title>
            <div>
              已选择表：<strong>{{ selectedTable.fullName }}</strong>
            </div>
            <div v-if="selectedTable.hasGeometry" style="margin-top: 8px; font-size: 13px">
              <el-icon><Location /></el-icon>
              几何列：{{ selectedTable.geometryColumn }}
              (SRID: {{ selectedTable.srid }})
            </div>
            <div v-else style="margin-top: 8px; font-size: 13px; color: #909399">
              无几何列
            </div>
          </template>
        </el-alert>
      </el-card>
    </div>

    <!-- Step 1: 确认服务类型（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 1">
      <el-card>
        <template #header>
          <span>选择服务类型</span>
        </template>

        <el-alert
          v-if="selectedTable && selectedTable.hasGeometry"
          type="info"
          :closable="false"
          style="margin-bottom: 20px"
        >
          检测到表 <strong>{{ selectedTable.fullName }}</strong> 包含几何列
          <strong>{{ selectedTable.geometryColumn }}</strong>，
          您可以选择发布为空间服务或普通数据表服务。
        </el-alert>

        <el-radio-group v-model="form.service_type" style="width: 100%">
          <el-card
            v-if="selectedTable && selectedTable.hasGeometry"
            shadow="hover"
            :class="{ 'selected-card': form.service_type === 'spatial' }"
            style="margin-bottom: 16px; cursor: pointer"
            @click="form.service_type = 'spatial'"
          >
            <el-radio label="spatial" style="width: 100%">
              <div class="service-type-card">
                <h3><el-icon><MapLocation /></el-icon> 空间服务</h3>
                <p>启用 OGC 协议（WFS/WMTS/OGC API），支持在地图上展示</p>
                <p>可被 QGIS、ArcGIS 等 GIS 工具访问</p>
                <p>支持添加多个图层，适合主题地图服务</p>
                <el-tag type="success" size="small">推荐用于地图展示</el-tag>
              </div>
            </el-radio>
          </el-card>

          <el-card
            shadow="hover"
            :class="{ 'selected-card': form.service_type === 'table' }"
            style="cursor: pointer"
            @click="form.service_type = 'table'"
          >
            <el-radio label="table" style="width: 100%">
              <div class="service-type-card">
                <h3><el-icon><Document /></el-icon> 数据表服务</h3>
                <p>简化的 REST API，适合 Web 应用集成</p>
                <p>单图层服务，专注于数据查询和导出</p>
                <p>支持分页、过滤、排序和多种导出格式</p>
                <el-tag type="primary" size="small">推荐用于业务数据</el-tag>
              </div>
            </el-radio>
          </el-card>
        </el-radio-group>
      </el-card>
    </div>

    <!-- Step 2: 配置服务信息 -->
    <div v-if="isEdit || currentStep === 2">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <!-- 基本信息 -->
        <el-card header="基本信息" style="margin-bottom: 20px">
          <el-form-item label="服务类型" v-if="!isEdit">
            <el-tag :type="form.service_type === 'spatial' ? 'success' : 'primary'" size="large">
              {{ form.service_type === 'spatial' ? '空间服务' : '数据表服务' }}
            </el-tag>
            <span style="margin-left: 12px; color: #909399; font-size: 13px">
              （服务类型创建后不可变更）
            </span>
          </el-form-item>

          <el-form-item label="服务名称" prop="service_name" required>
            <el-input
              v-model="form.service_name"
              placeholder="例如: city_poi"
              :disabled="isEdit"
            />
            <div class="help-text">
              仅支持英文、数字和下划线，用于服务 URL
            </div>
          </el-form-item>

          <el-form-item label="标题" prop="title" required>
            <el-input v-model="form.title" placeholder="例如: 城市兴趣点服务" />
          </el-form-item>

          <el-form-item label="摘要" prop="abstract">
            <el-input
              type="textarea"
              v-model="form.abstract"
              :rows="3"
              placeholder="服务的简要描述"
            />
          </el-form-item>

          <el-form-item label="关键词">
            <div class="keyword-input">
              <el-tag
                v-for="tag in form.keywords"
                :key="tag"
                closable
                @close="removeKeyword(tag)"
                style="margin-right: 8px; margin-bottom: 8px"
              >
                {{ tag }}
              </el-tag>
              <el-input
                v-if="inputVisible"
                v-model="inputValue"
                ref="inputRef"
                size="small"
                style="width: 120px"
                @keyup.enter="handleInputConfirm"
                @blur="handleInputConfirm"
              />
              <el-button v-else size="small" @click="showInput">
                + 添加关键词
              </el-button>
            </div>
          </el-form-item>
        </el-card>

        <!-- 空间服务专属配置 -->
        <el-card v-if="form.service_type === 'spatial'" header="空间服务配置" style="margin-bottom: 20px">
          <el-form-item label="默认坐标系" prop="default_srid" required>
            <el-select v-model="form.default_srid" style="width: 300px">
              <el-option :value="4326" label="EPSG:4326 (WGS84)" />
              <el-option :value="3857" label="EPSG:3857 (Web Mercator)" />
              <el-option :value="4490" label="EPSG:4490 (CGCS2000)" />
            </el-select>
          </el-form-item>

          <el-form-item label="启用协议" v-if="!isEdit">
            <el-checkbox-group v-model="enabledProtocols">
              <div style="display: flex; flex-direction: column; gap: 12px">
                <el-checkbox label="wfs">
                  <strong>WFS 2.0</strong>
                  <span style="color: #909399; margin-left: 8px">
                    矢量要素查询服务，支持 QGIS、ArcGIS
                  </span>
                </el-checkbox>

                <el-checkbox label="wmts">
                  <strong>WMTS 1.0</strong>
                  <span style="color: #909399; margin-left: 8px">
                    矢量瓦片地图服务，高性能地图展示
                  </span>
                </el-checkbox>

                <el-checkbox label="ogc_api">
                  <strong>OGC API Features</strong>
                  <span style="color: #909399; margin-left: 8px">
                    现代化 RESTful 空间 API
                  </span>
                </el-checkbox>

                <el-checkbox label="rest_query" checked disabled>
                  <strong>REST Query API</strong>
                  <span style="color: #909399; margin-left: 8px">
                    （默认启用）简化 REST 查询 API
                  </span>
                </el-checkbox>
              </div>
            </el-checkbox-group>
          </el-form-item>

          <el-form-item label="最大要素数" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">单次查询返回的最大要素数量</div>
          </el-form-item>
        </el-card>

        <!-- 数据表服务专属配置 -->
        <el-card v-if="form.service_type === 'table'" header="数据表服务配置" style="margin-bottom: 20px">
          <el-alert type="info" :closable="false" style="margin-bottom: 16px">
            数据表服务自动启用 REST Query API，支持灵活的查询、分页、过滤和导出功能。
          </el-alert>

          <el-form-item label="默认分页大小">
            <el-input-number v-model="form.default_page_size" :min="10" :max="100" />
          </el-form-item>

          <el-form-item label="最大返回记录数" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">单次查询返回的最大记录数量</div>
          </el-form-item>
        </el-card>

        <!-- 通用配置 -->
        <el-card header="访问控制" style="margin-bottom: 20px">
          <el-form-item label="访问权限">
            <el-checkbox v-model="form.public_access">
              允许公开访问（无需 JWT 认证）
            </el-checkbox>
            <div class="help-text">
              启用后，任何人都可以访问此服务的公开端点
            </div>
          </el-form-item>
        </el-card>

        <!-- 元数据（可选） -->
        <el-card header="元数据（可选）" style="margin-bottom: 20px">
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
            <el-input
              v-model="form.contact_email"
              type="email"
              placeholder="example@example.com"
            />
          </el-form-item>
        </el-card>
      </el-form>
    </div>

    <!-- 操作按钮 -->
    <div class="button-group">
      <el-button v-if="!isEdit && currentStep > 0" @click="prevStep">
        上一步
      </el-button>
      <el-button
        v-if="!isEdit && currentStep < 2"
        type="primary"
        :disabled="!canProceed"
        @click="nextStep"
      >
        下一步
      </el-button>
      <el-button
        v-if="isEdit || currentStep === 2"
        type="primary"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEdit ? '更新' : '创建' }}
      </el-button>
      <el-button @click="$router.back()">取消</el-button>
    </div>

    <!-- 表选择器对话框 -->
    <table-selector
      v-model:visible="showTableSelector"
      @table-selected="handleTableSelected"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  FolderOpened,
  Location,
  MapLocation,
  Document
} from '@element-plus/icons-vue'
import publishedServiceAPI from '../api/publishedService'
import TableSelector from '../components/TableSelector.vue'

const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const inputRef = ref(null)

const isEdit = computed(() => !!route.params.id)

const currentStep = ref(0)
const showTableSelector = ref(false)
const selectedTable = ref(null)
const enabledProtocols = ref(['wfs', 'wmts', 'ogc_api', 'rest_query'])

const form = ref({
  service_name: '',
  title: '',
  abstract: '',
  keywords: [],
  service_type: null,
  public_access: false,
  default_srid: 4326,
  max_features: 1000,
  default_page_size: 20,
  provider_name: '',
  provider_site: '',
  contact_person: '',
  contact_email: '',
  engine_id: null
})

const loading = ref(false)
const submitting = ref(false)
const inputVisible = ref(false)
const inputValue = ref('')

// 判断是否可以进入下一步
const canProceed = computed(() => {
  if (currentStep.value === 0) {
    return !!selectedTable.value
  }
  if (currentStep.value === 1) {
    return !!form.value.service_type
  }
  return true
})

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
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  default_srid: [
    { required: true, message: '请选择默认坐标系', trigger: 'change' }
  ],
  max_features: [
    { required: true, message: '请输入最大要素数', trigger: 'blur' },
    {
      type: 'number',
      min: 1,
      max: 10000,
      message: '值必须在 1 到 10000 之间',
      trigger: 'blur'
    }
  ]
}

// 处理表选择
const handleTableSelected = (table) => {
  selectedTable.value = table
  form.value.engine_id = table.engineId

  // 自动填充表单
  form.value.service_name = table.tableName
  form.value.title = table.tableName

  // 根据是否有几何列自动选择服务类型
  if (table.hasGeometry) {
    form.value.service_type = 'spatial'
    form.value.default_srid = table.srid || 4326
  } else {
    form.value.service_type = 'table'
  }
}

// 下一步
const nextStep = () => {
  if (!canProceed.value) {
    if (currentStep.value === 0) {
      ElMessage.warning('请先选择数据表')
    } else if (currentStep.value === 1) {
      ElMessage.warning('请选择服务类型')
    }
    return
  }
  currentStep.value++
}

// 上一步
const prevStep = () => {
  if (currentStep.value > 0) {
    currentStep.value--
  }
}

// 关键词管理
const removeKeyword = (tag) => {
  form.value.keywords = form.value.keywords.filter((k) => k !== tag)
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

// 加载服务详情（编辑模式）
const loadService = async () => {
  if (!isEdit.value) return

  loading.value = true
  try {
    const service_data = await publishedServiceAPI.getService(route.params.id)
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

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    // 验证表单
    await formRef.value.validate()

    submitting.value = true

    if (isEdit.value) {
      // 编辑模式：不允许修改服务类型和协议配置
      const updateData = {
        title: form.value.title,
        abstract: form.value.abstract,
        keywords: form.value.keywords,
        public_access: form.value.public_access,
        max_features: form.value.max_features,
        provider_name: form.value.provider_name,
        provider_site: form.value.provider_site,
        contact_person: form.value.contact_person,
        contact_email: form.value.contact_email
      }

      if (form.value.service_type === 'spatial') {
        updateData.default_srid = form.value.default_srid
      }

      await publishedServiceAPI.updateService(route.params.id, updateData)
      ElMessage.success('更新成功')
    } else {
      // 新建模式
      const createData = {
        service_name: form.value.service_name,
        title: form.value.title,
        abstract: form.value.abstract,
        keywords: form.value.keywords,
        service_type: form.value.service_type,
        public_access: form.value.public_access,
        max_features: form.value.max_features,
        provider_name: form.value.provider_name,
        provider_site: form.value.provider_site,
        contact_person: form.value.contact_person,
        contact_email: form.value.contact_email,
        engine_id: form.value.engine_id,
        first_layer: {
          layer_name: selectedTable.value.tableName,
          title: selectedTable.value.label,
          schema_name: selectedTable.value.schema,
          db_table_name: selectedTable.value.tableName,
          geometry_column: selectedTable.value.geometryColumn || '',
          srid: selectedTable.value.srid || 0,
          geometry_types: selectedTable.value.geometryType
            ? [selectedTable.value.geometryType]
            : [],
          enabled: true
        }
      }

      // 空间服务的额外配置
      if (form.value.service_type === 'spatial') {
        createData.default_srid = form.value.default_srid

        // 构建协议配置
        createData.protocols_config = {
          wfs: {
            enabled: enabledProtocols.value.includes('wfs'),
            version: '2.0.0'
          },
          wmts: {
            enabled: enabledProtocols.value.includes('wmts'),
            version: '1.0.0'
          },
          ogc_api: {
            enabled: enabledProtocols.value.includes('ogc_api'),
            version: '1.0'
          },
          rest_query: { enabled: true }
        }
      }

      const response = await publishedServiceAPI.createService(createData)
      ElMessage.success('创建成功')
      router.push(`/published-services/${response.id}`)
    }
  } catch (error) {
    if (error.message) {
      ElMessage.error('操作失败: ' + error.message)
    }
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  if (isEdit.value) {
    loadService()
  }
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

.button-group {
  margin-top: 20px;
  text-align: center;
}

.service-type-card {
  padding: 12px 0;
}

.service-type-card h3 {
  margin: 0 0 12px 0;
  font-size: 16px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.service-type-card p {
  margin: 4px 0;
  font-size: 13px;
  color: #606266;
}

.selected-card {
  border-color: #409eff;
  box-shadow: 0 2px 12px 0 rgba(64, 158, 255, 0.3);
}

:deep(.el-card__header) {
  font-weight: 600;
  font-size: 16px;
}

:deep(.el-radio) {
  width: 100%;
  margin-right: 0;
}
</style>
