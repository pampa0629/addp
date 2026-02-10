<template>
  <div class="query-service-detail" v-loading="loading">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <el-button @click="goBack" :icon="ArrowLeft" circle />
        <h2>{{ service?.title }}</h2>
        <div class="service-meta">
          <!-- 配置类型标签 -->
          <el-tag
            :type="service?.config_type === 'table' ? 'success' : 'warning'"
            size="large"
          >
            {{ service?.config_type === 'table' ? '表配置' : 'SQL配置' }}
          </el-tag>

          <!-- 协议标签 -->
          <div class="protocol-tags">
            <el-tag v-if="isProtocolEnabled('rest_api')" size="small" type="primary">
              REST API
            </el-tag>
            <el-tag v-if="isProtocolEnabled('ogc_features')" size="small" type="success">
              OGC Features
            </el-tag>
          </div>
        </div>
      </div>
      <div class="header-right">
        <el-button @click="goToEdit">编辑</el-button>
        <el-button type="danger" @click="handleDelete">删除</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card header="服务信息" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="服务名称">
          <code>{{ service?.service_name }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="标题">
          {{ service?.title }}
        </el-descriptions-item>
        <el-descriptions-item label="配置方式">
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'">
            {{ service?.config_type === 'table' ? '表配置' : 'SQL配置' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="存储引擎">
          Engine #{{ service?.engine_id }}
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">
          {{ service?.description || '无' }}
        </el-descriptions-item>
        <el-descriptions-item label="关键词" :span="2">
          <el-tag
            v-for="kw in service?.keywords"
            :key="kw"
            size="small"
            style="margin-right: 5px"
          >
            {{ kw }}
          </el-tag>
          <span v-if="!service?.keywords || service.keywords.length === 0">无</span>
        </el-descriptions-item>
        <el-descriptions-item label="最大返回数">
          {{ service?.max_features }}
        </el-descriptions-item>
        <el-descriptions-item label="公开访问">
          <el-tag :type="service?.public_access ? 'success' : 'info'" size="small">
            {{ service?.public_access ? '是' : '否' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(service?.status)" size="small">
            {{ getStatusText(service?.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ formatDate(service?.created_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源配置卡片 -->
    <el-card header="数据源配置" style="margin-bottom: 20px">
      <!-- 表配置模式 -->
      <div v-if="service?.config_type === 'table'">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="Schema">
            <code>{{ service?.schema_name }}</code>
          </el-descriptions-item>
          <el-descriptions-item label="Table">
            <code>{{ service?.table_name }}</code>
          </el-descriptions-item>
        </el-descriptions>

        <!-- 空间字段信息 -->
        <div v-if="hasGeometry" style="margin-top: 16px">
          <el-divider content-position="left">空间字段配置</el-divider>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="几何列">
              <code>{{ geometryConfig?.column }}</code>
            </el-descriptions-item>
            <el-descriptions-item label="坐标系">
              EPSG:{{ geometryConfig?.srid }}
            </el-descriptions-item>
            <el-descriptions-item label="几何类型" :span="2">
              <el-tag
                v-for="type in geometryConfig?.types"
                :key="type"
                size="small"
                type="success"
                style="margin-right: 5px"
              >
                {{ type }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item v-if="geometryConfig?.extent" label="空间范围" :span="2">
              <code style="font-size: 12px">
                {{ JSON.stringify(geometryConfig.extent) }}
              </code>
            </el-descriptions-item>
          </el-descriptions>
        </div>

        <!-- 字段配置 -->
        <div v-if="defaultFields || filterableFields" style="margin-top: 16px">
          <el-divider content-position="left">字段配置</el-divider>
          <el-descriptions :column="1" border>
            <el-descriptions-item v-if="defaultFields" label="默认返回字段">
              <el-tag
                v-for="field in defaultFields"
                :key="field"
                size="small"
                style="margin-right: 5px"
              >
                {{ field }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item v-if="filterableFields" label="可过滤字段">
              <el-tag
                v-for="field in filterableFields"
                :key="field"
                size="small"
                type="info"
                style="margin-right: 5px"
              >
                {{ field }}
              </el-tag>
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </div>

      <!-- SQL配置模式 -->
      <div v-else-if="service?.config_type === 'sql'">
        <el-alert
          type="info"
          title="SQL查询模式"
          description="使用自定义SQL查询，仅支持分页参数，不支持动态filter/fields/orderBy"
          :closable="false"
          style="margin-bottom: 16px"
        />
        <div class="sql-query-box">
          <pre><code>{{ service?.sql_query }}</code></pre>
        </div>

        <!-- 空间字段信息（SQL模式） -->
        <div v-if="hasGeometry" style="margin-top: 16px">
          <el-divider content-position="left">空间字段配置</el-divider>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="几何列">
              <code>{{ geometryConfig?.column }}</code>
            </el-descriptions-item>
            <el-descriptions-item label="坐标系">
              EPSG:{{ geometryConfig?.srid }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </div>
    </el-card>

    <!-- 服务端点卡片 -->
    <el-card header="服务端点" style="margin-bottom: 20px">
      <!-- REST API 端点 -->
      <div v-if="isProtocolEnabled('rest_api')" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>REST API 查询端点</span>
        </div>
        <div class="endpoint-url">
          <el-input :value="restApiEndpoint" readonly />
          <el-button @click="copyEndpoint(restApiEndpoint)">复制</el-button>
          <el-button @click="testRestAPI">测试</el-button>
        </div>

        <!-- Table模式：显示查询参数说明 -->
        <div v-if="service?.config_type === 'table'" style="margin-top: 12px; font-size: 13px; color: #606266">
          <strong>支持的查询参数：</strong>
          <ul style="margin: 8px 0; padding-left: 20px">
            <li><code>filter</code>：WHERE条件（如：name='张三' AND age>20）</li>
            <li><code>fields</code>：返回字段列表（如：id,name,age）</li>
            <li><code>orderBy</code>：排序（如：age DESC, name ASC）</li>
            <li><code>page</code> 和 <code>page_size</code>：分页参数</li>
            <li><code>format</code>：输出格式（json/csv/geojson）</li>
          </ul>
        </div>

        <!-- SQL模式：显示查询参数说明 -->
        <div v-else style="margin-top: 12px; font-size: 13px; color: #606266">
          <strong>支持的查询参数：</strong>
          <ul style="margin: 8px 0; padding-left: 20px">
            <li><code>page</code> 和 <code>page_size</code>：分页参数</li>
            <li><code>format</code>：输出格式（json/csv/geojson）</li>
          </ul>
        </div>
      </div>

      <!-- OGC API Features 端点 -->
      <div v-if="isProtocolEnabled('ogc_features')" class="endpoint-item" style="margin-top: 20px">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>OGC API Features</span>
        </div>

        <el-alert
          type="success"
          :closable="false"
          style="margin-bottom: 12px"
        >
          OGC API Features 已自动启用（检测到空间字段）
        </el-alert>

        <!-- Landing Page -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: #909399; margin-bottom: 4px">Landing Page:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesLandingPage" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesLandingPage)">复制</el-button>
            <el-button @click="testEndpoint(ogcFeaturesLandingPage)">测试</el-button>
          </div>
        </div>

        <!-- Collections -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: #909399; margin-bottom: 4px">Collections:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesCollections" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesCollections)">复制</el-button>
            <el-button @click="testEndpoint(ogcFeaturesCollections)">测试</el-button>
          </div>
        </div>

        <!-- Conformance -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: #909399; margin-bottom: 4px">Conformance:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesConformance" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesConformance)">复制</el-button>
            <el-button @click="testEndpoint(ogcFeaturesConformance)">测试</el-button>
          </div>
        </div>

        <!-- Items (实际数据) -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: #909399; margin-bottom: 4px">
            Items (实际地理数据，限制10条):
          </div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesItems" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesItems)">复制</el-button>
            <el-button type="primary" @click="testEndpoint(ogcFeaturesItems)">测试</el-button>
          </div>
        </div>
      </div>

      <div v-if="!isProtocolEnabled('rest_api') && !isProtocolEnabled('ogc_features')" class="no-endpoints">
        <el-empty description="未启用任何协议" />
      </div>
    </el-card>

    <!-- 数据预览卡片 -->
    <el-card header="数据预览" style="margin-bottom: 20px">
      <div class="preview-controls">
        <el-button
          type="primary"
          @click="loadPreviewData"
          :loading="previewLoading"
          :disabled="!isProtocolEnabled('rest_api')"
        >
          {{ previewData.length > 0 ? '刷新数据' : '加载数据' }}
        </el-button>
        <div v-if="previewData.length > 0" style="color: #606266; font-size: 14px">
          共 {{ previewPagination.total }} 条记录，当前显示 {{ previewData.length }} 条
        </div>
      </div>

      <!-- 数据表格 -->
      <el-table
        v-if="previewData.length > 0"
        :data="previewData"
        border
        stripe
        style="margin-top: 16px"
        max-height="600"
        v-loading="previewLoading"
      >
        <el-table-column
          v-for="column in previewColumns"
          :key="column"
          :prop="column"
          :label="column"
          :min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span v-if="isGeometryColumn(column)" class="geometry-data">
              [几何数据]
            </span>
            <span v-else>{{ formatCellValue(row[column]) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-if="previewData.length > 0"
        v-model:current-page="previewPagination.page"
        v-model:page-size="previewPagination.pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="previewPagination.total"
        layout="total, sizes, prev, pager, next, jumper"
        @current-change="loadPreviewData"
        @size-change="loadPreviewData"
        style="margin-top: 16px; justify-content: center"
      />

      <!-- 空状态 -->
      <div v-if="previewData.length === 0 && !previewLoading" class="preview-empty">
        <el-empty description="点击「加载数据」按钮查看数据预览" />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Link } from '@element-plus/icons-vue'
import queryServiceAPI from '@/api/queryService'
import { copyToClipboard } from '../utils/serviceHelper'
import axios from 'axios'

const router = useRouter()
const route = useRoute()

const service = ref(null)
const loading = ref(false)

// 数据预览相关状态
const previewData = ref([])
const previewColumns = ref([])
const previewLoading = ref(false)
const previewPagination = ref({
  page: 1,
  pageSize: 20,
  total: 0
})

const serviceId = computed(() => route.params.id)

// 计算属性：空间字段配置
const hasGeometry = computed(() => {
  if (!service.value?.data_config?.geometry) return false
  return service.value.data_config.geometry.has_geometry === true
})

const geometryConfig = computed(() => {
  return service.value?.data_config?.geometry || null
})

// 计算属性：字段配置
const defaultFields = computed(() => {
  return service.value?.data_config?.default_fields || null
})

const filterableFields = computed(() => {
  return service.value?.data_config?.filterable_fields || null
})

// 计算属性：服务端点
const baseURL = computed(() => {
  // 开发环境和生产环境的基础URL
  return window.location.origin
})

const restApiEndpoint = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/api/query/${service.value.service_name}`
})

const ogcFeaturesLandingPage = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/ogc/features/${service.value.service_name}`
})

const ogcFeaturesCollections = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/ogc/features/${service.value.service_name}/collections`
})

const ogcFeaturesConformance = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/ogc/features/${service.value.service_name}/conformance`
})

const ogcFeaturesItems = computed(() => {
  if (!service.value) return ''
  // collectionId 使用服务名称
  return `${baseURL.value}/ogc/features/${service.value.service_name}/collections/${service.value.service_name}/items?limit=10`
})

// 方法：检查协议是否启用
const isProtocolEnabled = (protocolName) => {
  if (!service.value?.protocols || !service.value.protocols[protocolName]) {
    return false
  }
  return service.value.protocols[protocolName].enabled === true
}

// 方法：获取状态文本
const getStatusText = (status) => {
  const statusMap = {
    active: '活跃',
    inactive: '非活跃',
    error: '错误'
  }
  return statusMap[status] || status
}

const getStatusType = (status) => {
  const typeMap = {
    active: 'success',
    inactive: 'warning',
    error: 'danger'
  }
  return typeMap[status] || 'info'
}

// 方法：格式化日期
const formatDate = (dateString) => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleString('zh-CN')
}

// 方法：复制端点
const copyEndpoint = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动复制')
  }
}

// 方法：测试端点
const testEndpoint = (url) => {
  window.open(url, '_blank')
}

// 方法：测试 REST API
const testRestAPI = () => {
  // 构建测试URL（带分页参数）
  const testUrl = `${restApiEndpoint.value}?page=1&page_size=10&format=json`
  window.open(testUrl, '_blank')
}

// 方法：加载服务详情
const loadService = async () => {
  loading.value = true
  try {
    const response = await queryServiceAPI.getService(serviceId.value)
    service.value = response
  } catch (error) {
    ElMessage.error('加载服务详情失败: ' + (error.message || '未知错误'))
    console.error('Failed to load service:', error)
  } finally {
    loading.value = false
  }
}

// 方法：删除服务
const handleDelete = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要删除此查询服务吗？删除后无法恢复。',
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await queryServiceAPI.deleteService(serviceId.value)
    ElMessage.success('查询服务已删除')
    router.push('/query-services')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.message || '未知错误'))
    }
  }
}

// 方法：导航
const goBack = () => {
  router.push('/query-services')
}

const goToEdit = () => {
  router.push(`/query-services/${serviceId.value}/edit`)
}

// 数据预览方法
const loadPreviewData = async () => {
  if (!service.value?.service_name) {
    ElMessage.warning('服务信息未加载')
    return
  }

  previewLoading.value = true
  try {
    const url = `/api/query/${service.value.service_name}`
    const params = {
      page: previewPagination.value.page,
      page_size: previewPagination.value.pageSize,
      format: 'json'
    }

    const response = await axios.get(url, { params })

    if (response.data && response.data.data) {
      previewData.value = response.data.data

      // 提取列名（从第一行数据中）
      if (response.data.data.length > 0) {
        previewColumns.value = Object.keys(response.data.data[0])
      }

      // 更新分页信息
      if (response.data.pagination) {
        previewPagination.value.total = response.data.pagination.total
      }

      ElMessage.success(`成功加载 ${response.data.data.length} 条数据`)
    } else {
      ElMessage.warning('未获取到数据')
    }
  } catch (error) {
    console.error('加载数据预览失败:', error)
    ElMessage.error('加载数据预览失败: ' + (error.response?.data?.error || error.message))
  } finally {
    previewLoading.value = false
  }
}

// 判断是否是几何列
const isGeometryColumn = (columnName) => {
  const geometryColumnNames = ['geom', 'geometry', 'shape', 'wkb_geometry', 'smgeometry', 'the_geom']
  return geometryColumnNames.includes(columnName.toLowerCase())
}

// 格式化单元格值
const formatCellValue = (value) => {
  if (value === null || value === undefined) {
    return '-'
  }
  if (typeof value === 'object') {
    return JSON.stringify(value)
  }
  if (typeof value === 'number') {
    return value.toLocaleString()
  }
  return value
}

// 生命周期
onMounted(() => {
  loadService()
})
</script>

<style scoped>
.query-service-detail {
  padding: 20px;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
}

.header-left h2 {
  margin: 0;
  font-size: 24px;
  color: #303133;
}

.service-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.protocol-tags {
  display: flex;
  gap: 4px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.endpoint-item {
  margin-bottom: 24px;
}

.endpoint-item:last-child {
  margin-bottom: 0;
}

.endpoint-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 500;
  color: #303133;
  margin-bottom: 8px;
}

.endpoint-url {
  display: flex;
  gap: 8px;
  align-items: center;
}

.endpoint-url .el-input {
  flex: 1;
}

.no-endpoints {
  text-align: center;
  padding: 40px 0;
}

.sql-query-box {
  background-color: #f5f7fa;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
}

.sql-query-box pre {
  margin: 0;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #303133;
  white-space: pre-wrap;
  word-wrap: break-word;
}

code {
  background-color: #f5f7fa;
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #e83e8c;
}

ul {
  line-height: 1.8;
}

.preview-controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.preview-empty {
  padding: 40px 0;
  text-align: center;
}

.geometry-data {
  color: #909399;
  font-style: italic;
  font-size: 12px;
}
</style>
