<template>
  <div class="published-service-detail" v-loading="loading">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <h2>{{ service?.title }}</h2>
        <div class="service-meta">
          <el-tag type="primary" size="large">查询服务</el-tag>
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'" size="small">
            {{ service?.config_type === 'table' ? '表模式' : 'SQL模式' }}
          </el-tag>
        </div>
      </div>
      <div class="header-right">
        <el-button @click="goToEdit">编辑</el-button>
        <el-button @click="goToTest">测试服务</el-button>
        <el-button type="danger" @click="handleDelete">删除</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card header="服务信息" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="服务名称">
          {{ service?.service_name }}
        </el-descriptions-item>
        <el-descriptions-item label="标题">
          {{ service?.title }}
        </el-descriptions-item>
        <el-descriptions-item label="配置类型">
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'">
            {{ service?.config_type === 'table' ? '表模式' : 'SQL模式' }}
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
        <el-descriptions-item label="最大要素数">
          {{ service?.max_features }}
        </el-descriptions-item>
        <el-descriptions-item label="公开访问">
          <el-tag :type="service?.public_access ? 'success' : 'info'" size="small">
            {{ service?.public_access ? '是' : '否' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag
            :type="service?.status === 'active' ? 'success' : service?.status === 'inactive' ? 'warning' : 'danger'"
            size="small"
          >
            {{ service?.status === 'active' ? '活动' : service?.status === 'inactive' ? '未激活' : '错误' }}
          </el-tag>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源信息卡片 -->
    <el-card header="数据源" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <template v-if="service?.config_type === 'table'">
          <el-descriptions-item label="Schema">
            {{ service?.schema_name }}
          </el-descriptions-item>
          <el-descriptions-item label="表名">
            {{ service?.table_name }}
          </el-descriptions-item>
          <el-descriptions-item label="完整路径" :span="2">
            <el-tag type="info">{{ service?.schema_name }}.{{ service?.table_name }}</el-tag>
          </el-descriptions-item>
        </template>
        <template v-else-if="service?.config_type === 'sql'">
          <el-descriptions-item label="SQL 查询" :span="2">
            <pre style="background: var(--addp-bg-secondary); padding: 12px; border-radius: 4px; overflow-x: auto">{{ service?.sql_query }}</pre>
          </el-descriptions-item>
        </template>
      </el-descriptions>

      <!-- 几何信息 -->
      <div v-if="hasGeometry" style="margin-top: 16px">
        <el-divider content-position="left">空间字段信息</el-divider>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="几何列">
            {{ geometryInfo.column }}
          </el-descriptions-item>
          <el-descriptions-item label="坐标系">
            EPSG:{{ geometryInfo.srid }}
          </el-descriptions-item>
          <el-descriptions-item label="几何类型" :span="2">
            <el-tag
              v-for="type in geometryInfo.types"
              :key="type"
              type="success"
              size="small"
              style="margin-right: 5px"
            >
              {{ type }}
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>
      </div>
    </el-card>

    <!-- 服务端点卡片 -->
    <el-card header="服务端点" style="margin-bottom: 20px">
      <!-- REST API 端点 -->
      <div v-if="service?.endpoints?.rest_api" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          REST API
        </div>
        <div class="endpoint-url">
          <el-input :value="service.endpoints.rest_api" readonly />
          <el-button @click="copyEndpoint(service.endpoints.rest_api)">复制</el-button>
          <el-button @click="testEndpoint(service.endpoints.rest_api)">测试</el-button>
        </div>
        <div style="margin-top: 12px; font-size: 13px; color: var(--addp-text-secondary)">
          <strong>支持的查询参数：</strong>
          <ul style="margin: 8px 0; padding-left: 20px">
            <li><code>page</code> 和 <code>page_size</code>：分页参数</li>
            <li><code>format</code>：输出格式 (json, csv, geojson)</li>
            <li><code>fields</code>：指定返回的字段</li>
            <li><code>filter</code>：条件过滤</li>
          </ul>
        </div>
      </div>

      <!-- OGC Features 端点 -->
      <div v-if="service?.endpoints?.ogc_features" class="endpoint-item" style="margin-top: 20px">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          OGC API Features
        </div>
        <div class="endpoint-url">
          <el-input :value="service.endpoints.ogc_features" readonly />
          <el-button @click="copyEndpoint(service.endpoints.ogc_features)">复制</el-button>
          <el-button @click="testEndpoint(service.endpoints.ogc_features)">测试</el-button>
        </div>
        <div style="margin-top: 8px; font-size: 13px; color: var(--addp-text-tertiary)">
          Collections: <code>{{ service.endpoints.ogc_features_collections }}</code>
        </div>
      </div>

      <el-empty
        v-if="!service?.endpoints || Object.keys(service.endpoints).length === 0"
        description="未配置任何服务端点"
      />
    </el-card>

    <!-- 协议配置卡片 -->
    <el-card header="协议配置">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="REST API">
          <el-tag
            :type="isProtocolEnabled('rest_api') ? 'success' : 'info'"
            size="small"
          >
            {{ isProtocolEnabled('rest_api') ? '已启用' : '未启用' }}
          </el-tag>
          <span v-if="isProtocolEnabled('rest_api')" style="margin-left: 12px; color: var(--addp-text-secondary)">
            支持格式: {{ getProtocolFormats('rest_api').join(', ') }}
          </span>
        </el-descriptions-item>
        <el-descriptions-item label="OGC API Features">
          <el-tag
            :type="isProtocolEnabled('ogc_features') ? 'success' : 'info'"
            size="small"
          >
            {{ isProtocolEnabled('ogc_features') ? '已启用' : '未启用' }}
          </el-tag>
          <span v-if="isProtocolEnabled('ogc_features')" style="margin-left: 12px; color: var(--addp-text-secondary)">
            版本: {{ getProtocolVersion('ogc_features') }}
          </span>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link } from '@element-plus/icons-vue'
import publishedServiceAPI from '../api/publishedService'
import { copyToClipboard } from '../utils/serviceHelper'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const service = ref(null)

// 计算几何信息
const hasGeometry = computed(() => {
  if (!service.value?.data_config?.geometry) return false
  return service.value.data_config.geometry.has_geometry === true
})

const geometryInfo = computed(() => {
  if (!hasGeometry.value) return null
  return service.value.data_config.geometry
})

// 检查协议是否启用
const isProtocolEnabled = (protocol) => {
  if (!service.value?.protocols) return false
  const config = service.value.protocols[protocol]
  if (!config) return false
  return config.enabled === true
}

// 获取协议支持的格式
const getProtocolFormats = (protocol) => {
  if (!service.value?.protocols) return []
  const config = service.value.protocols[protocol]
  if (!config || !config.formats) return []
  return config.formats
}

// 获取协议版本
const getProtocolVersion = (protocol) => {
  if (!service.value?.protocols) return ''
  const config = service.value.protocols[protocol]
  if (!config || !config.version) return ''
  return config.version
}

// 加载服务详情
const loadService = async () => {
  loading.value = true
  try {
    const data = await publishedServiceAPI.getService(route.params.id)
    service.value = data
  } catch (error) {
    ElMessage.error('加载失败: ' + (error.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

// 复制端点 URL
const copyEndpoint = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动复制')
  }
}

// 测试端点
const testEndpoint = (url) => {
  window.open(url, '_blank')
}

// 跳转到编辑页
const goToEdit = () => {
  router.push(`/published-services/${route.params.id}/edit`)
}

// 跳转到测试页
const goToTest = () => {
  router.push(`/published-services/${route.params.id}/test`)
}

// 删除服务
const handleDelete = async () => {
  try {
    await ElMessageBox.confirm('确定要删除该服务吗？此操作不可恢复。', '警告', {
      type: 'warning',
      confirmButtonText: '确定',
      cancelButtonText: '取消'
    })

    await publishedServiceAPI.deleteService(route.params.id)
    ElMessage.success('删除成功')
    router.push('/published-services')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.message || '未知错误'))
    }
  }
}

onMounted(() => {
  loadService()
})
</script>

<style scoped>
.published-service-detail {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
}

.header-left h2 {
  margin: 0 0 12px 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.service-meta {
  display: flex;
  gap: 8px;
  align-items: center;
}

.header-right {
  display: flex;
  gap: 12px;
}

.endpoint-item {
  margin-bottom: 16px;
}

.endpoint-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--addp-text-primary);
}

.endpoint-url {
  display: flex;
  gap: 8px;
  align-items: center;
}

.endpoint-url .el-input {
  flex: 1;
}

pre {
  margin: 0;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.5;
}

code {
  background: var(--addp-bg-secondary);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', monospace;
  font-size: 12px;
}
</style>
