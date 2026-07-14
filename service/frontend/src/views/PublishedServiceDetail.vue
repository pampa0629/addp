<template>
  <div class="published-service-detail" v-loading="loading">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <h2>{{ service?.title }}</h2>
        <div class="service-meta">
          <el-tag type="primary" size="large">{{ t('service.published.queryServiceTag') }}</el-tag>
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'" size="small">
            {{ service?.config_type === 'table' ? t('service.published.tableModeTag') : t('service.published.sqlModeTag') }}
          </el-tag>
        </div>
      </div>
      <div class="header-right">
        <el-button @click="goToEdit">{{ t('service.common.edit') }}</el-button>
        <el-button @click="goToTest">{{ t('service.published.testService') }}</el-button>
        <el-button type="danger" @click="handleDelete">{{ t('service.common.delete') }}</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card :header="t('service.published.detailTitle')" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('service.published.colServiceNameDetail')">
          {{ service?.service_name }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colTitleDetail')">
          {{ service?.title }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colConfigTypeDetail')">
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'">
            {{ service?.config_type === 'table' ? t('service.published.tableModeTag') : t('service.published.sqlModeTag') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colEngineDetail')">
          Engine #{{ service?.engine_id }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colDescriptionDetail')" :span="2">
          {{ service?.description || t('service.common.none') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colKeywordsDetail')" :span="2">
          <el-tag
            v-for="kw in service?.keywords"
            :key="kw"
            size="small"
            style="margin-right: 5px"
          >
            {{ kw }}
          </el-tag>
          <span v-if="!service?.keywords || service.keywords.length === 0">{{ t('service.common.none') }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colMaxFeaturesDetail')">
          {{ service?.max_features }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colPublicAccessDetail')">
          <el-tag :type="service?.public_access ? 'success' : 'info'" size="small">
            {{ service?.public_access ? t('service.common.yes') : t('service.common.no') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.published.colStatusDetail')">
          <el-tag
            :type="service?.status === 'active' ? 'success' : service?.status === 'inactive' ? 'warning' : 'danger'"
            size="small"
          >
            {{ service?.status === 'active' ? t('service.published.statusActive') : service?.status === 'inactive' ? t('service.published.statusInactive') : t('service.published.statusError') }}
          </el-tag>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源信息卡片 -->
    <el-card :header="t('service.published.dataSourceTitle')" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <template v-if="service?.config_type === 'table'">
          <el-descriptions-item label="Schema">
            {{ service?.schema_name }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.published.tableNameLabel')">
            {{ service?.table_name }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.published.fullPathLabel')" :span="2">
            <el-tag type="info">{{ service?.schema_name }}.{{ service?.table_name }}</el-tag>
          </el-descriptions-item>
        </template>
        <template v-else-if="service?.config_type === 'sql'">
          <el-descriptions-item :label="t('service.published.sqlQueryLabel')" :span="2">
            <pre style="background: var(--addp-bg-secondary); padding: 12px; border-radius: 4px; overflow-x: auto">{{ service?.sql_query }}</pre>
          </el-descriptions-item>
        </template>
      </el-descriptions>

      <!-- 几何信息 -->
      <div v-if="hasGeometry" style="margin-top: 16px">
        <el-divider content-position="left">{{ t('service.published.spatialFieldTitle') }}</el-divider>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('service.published.geometryColumnLabel')">
            {{ geometryInfo.column }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.published.sridLabel')">
            EPSG:{{ geometryInfo.srid }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.published.geometryTypeLabel')" :span="2">
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
    <el-card :header="t('service.published.endpointsTitle')" style="margin-bottom: 20px">
      <!-- REST API 端点 -->
      <div v-if="service?.endpoints?.rest_api" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          REST API
        </div>
        <div class="endpoint-url">
          <el-input :value="service.endpoints.rest_api" readonly />
          <el-button @click="copyEndpoint(service.endpoints.rest_api)">{{ t('service.common.copy') }}</el-button>
          <el-button @click="testEndpoint(service.endpoints.rest_api)">{{ t('service.common.test') }}</el-button>
        </div>
        <div style="margin-top: 12px; font-size: 13px; color: var(--addp-text-secondary)">
          <strong>{{ t('service.query.supportedParams') }}</strong>
          <ul style="margin: 8px 0; padding-left: 20px">
            <li><code>page</code> {{ t('service.query.paramAnd') }} <code>page_size</code>：{{ t('service.query.paramPage') }}</li>
            <li><code>format</code>：{{ t('service.query.paramFormat') }}</li>
            <li><code>fields</code>：{{ t('service.query.paramFields') }}</li>
            <li><code>filter</code>：{{ t('service.query.paramFilter') }}</li>
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
          <el-button @click="copyEndpoint(service.endpoints.ogc_features)">{{ t('service.common.copy') }}</el-button>
          <el-button @click="testEndpoint(service.endpoints.ogc_features)">{{ t('service.common.test') }}</el-button>
        </div>
        <div style="margin-top: 8px; font-size: 13px; color: var(--addp-text-tertiary)">
          Collections: <code>{{ service.endpoints.ogc_features_collections }}</code>
        </div>
      </div>

      <el-empty
        v-if="!service?.endpoints || Object.keys(service.endpoints).length === 0"
        :description="t('service.common.notConfigured')"
      />
    </el-card>

    <!-- 协议配置卡片 -->
    <el-card :header="t('service.published.protocolConfigTitle')">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="REST API">
          <el-tag
            :type="isProtocolEnabled('rest_api') ? 'success' : 'info'"
            size="small"
          >
            {{ isProtocolEnabled('rest_api') ? t('service.published.protocolEnabled') : t('service.published.protocolDisabled') }}
          </el-tag>
          <span v-if="isProtocolEnabled('rest_api')" style="margin-left: 12px; color: var(--addp-text-secondary)">
            {{ t('service.published.supportedFormats') }}: {{ getProtocolFormats('rest_api').join(', ') }}
          </span>
        </el-descriptions-item>
        <el-descriptions-item label="OGC API Features">
          <el-tag
            :type="isProtocolEnabled('ogc_features') ? 'success' : 'info'"
            size="small"
          >
            {{ isProtocolEnabled('ogc_features') ? t('service.published.protocolEnabled') : t('service.published.protocolDisabled') }}
          </el-tag>
          <span v-if="isProtocolEnabled('ogc_features')" style="margin-left: 12px; color: var(--addp-text-secondary)">
            {{ t('service.published.version') }}: {{ getProtocolVersion('ogc_features') }}
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
import { useI18n } from 'vue-i18n'
import publishedServiceAPI from '../api/publishedService'
import { copyToClipboard } from '../utils/serviceHelper'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const service = ref(null)

// 计算几何信息
const spatialInfo = computed(() => service.value?.data_config?.source_snapshot?.spatial || null)
const primaryGeometry = computed(() => {
	const columns = spatialInfo.value?.geometry_columns || []
	const primaryName = spatialInfo.value?.primary_geometry_column
	return columns.find(column => column.name === primaryName) || (columns.length === 1 ? columns[0] : null)
})
const hasGeometry = computed(() => {
	return !!primaryGeometry.value?.name
})

const geometryInfo = computed(() => {
  if (!hasGeometry.value) return null
	return {
	  column: primaryGeometry.value.name,
	  srid: primaryGeometry.value.srid || spatialInfo.value?.srid || 0,
	  types: primaryGeometry.value.geometry_type ? [primaryGeometry.value.geometry_type] : [],
	  extent: spatialInfo.value?.extent || null
	}
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
    ElMessage.error(t('service.common.loadFailed') + ': ' + (error.message || t('service.common.unknownError')))
  } finally {
    loading.value = false
  }
}

// 复制端点 URL
const copyEndpoint = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success(t('service.common.copied'))
  } else {
    ElMessage.error(t('service.common.copyFailed'))
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
    await ElMessageBox.confirm(t('service.published.deleteConfirm'), t('service.common.deleteConfirmTitle'), {
      type: 'warning',
      confirmButtonText: t('service.common.confirm'),
      cancelButtonText: t('service.common.cancel')
    })

    await publishedServiceAPI.deleteService(route.params.id)
    ElMessage.success(t('service.published.deleteSuccess'))
    router.push('/published-services')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('service.published.deleteFailed') + ': ' + (error.message || t('service.common.unknownError')))
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
