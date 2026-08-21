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
            {{ service?.config_type === 'table' ? t('service.query.configTypeTable') : t('service.query.configTypeSql') }}
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
		<el-button
		  v-if="service?.config_type === 'table'"
		  :loading="snapshotChecking"
		  @click="checkSourceSnapshot"
		>
		  {{ t('service.query.snapshotCheck') }}
		</el-button>
        <el-button @click="goToEdit">{{ t('service.common.edit') }}</el-button>
        <el-button type="danger" @click="handleDelete">{{ t('service.common.delete') }}</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card :header="t('service.query.cardServiceInfo')" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('service.query.labelServiceName')">
          <code>{{ service?.service_name }}</code>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelTitle')">
          {{ service?.title }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelConfigType')">
          <el-tag :type="service?.config_type === 'table' ? 'success' : 'warning'">
            {{ service?.config_type === 'table' ? t('service.query.configTypeTable') : t('service.query.configTypeSql') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelEngine')">
          {{ service?.runtime_engine_id ? `Runtime #${service.runtime_engine_id}` : `Engine #${service?.engine_id}` }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelDescription')" :span="2">
          {{ service?.description || t('service.common.none') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelKeywords')" :span="2">
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
        <el-descriptions-item :label="t('service.query.labelMaxFeatures')">
          {{ service?.max_features }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelPublicAccess')">
          <el-tag :type="service?.public_access ? 'success' : 'info'" size="small">
            {{ service?.public_access ? t('service.common.yes') : t('service.common.no') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelStatus')">
          <el-tag :type="getStatusType(service?.status)" size="small">
            {{ getStatusText(service?.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('service.query.labelCreatedAt')">
          {{ formatDate(service?.created_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 数据源配置卡片 -->
    <el-card :header="t('service.query.cardDataSource')" style="margin-bottom: 20px">
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
          <el-divider content-position="left">{{ t('service.query.dividerGeometry') }}</el-divider>
          <el-descriptions :column="2" border>
            <el-descriptions-item :label="t('service.query.labelGeomColumn')">
			  <code>{{ primaryGeometry?.name }}</code>
            </el-descriptions-item>
            <el-descriptions-item :label="t('service.query.labelSrid')">
			  {{ primaryCRSLabel }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('service.query.labelGeomType')" :span="2">
              <el-tag
				v-for="type in geometryTypes"
                :key="type"
                size="small"
                type="success"
                style="margin-right: 5px"
              >
                {{ type }}
              </el-tag>
            </el-descriptions-item>
			<el-descriptions-item v-if="spatialInfo?.extent" :label="t('service.query.labelExtent')" :span="2">
              <code style="font-size: 12px">
				{{ JSON.stringify(spatialInfo.extent) }}
              </code>
            </el-descriptions-item>
          </el-descriptions>
        </div>

        <!-- 字段配置 -->
        <div v-if="defaultFields || filterableFields" style="margin-top: 16px">
          <el-divider content-position="left">{{ t('service.query.dividerFields') }}</el-divider>
          <el-descriptions :column="1" border>
            <el-descriptions-item v-if="defaultFields" :label="t('service.query.labelDefaultFields')">
              <el-tag
                v-for="field in defaultFields"
                :key="field"
                size="small"
                style="margin-right: 5px"
              >
                {{ field }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item v-if="filterableFields" :label="t('service.query.labelFilterableFields')">
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
          :title="t('service.query.sqlModeTitle')"
          :description="t('service.query.sqlModeDesc')"
          :closable="false"
          style="margin-bottom: 16px"
        />
        <div class="sql-query-box">
          <pre><code>{{ service?.sql_query }}</code></pre>
        </div>

        <!-- 空间字段信息（SQL模式） -->
        <div v-if="hasGeometry" style="margin-top: 16px">
          <el-divider content-position="left">{{ t('service.query.dividerGeometry') }}</el-divider>
          <el-descriptions :column="2" border>
            <el-descriptions-item :label="t('service.query.labelGeomColumn')">
			  <code>{{ primaryGeometry?.name }}</code>
            </el-descriptions-item>
            <el-descriptions-item :label="t('service.query.labelSrid')">
			  {{ primaryCRSLabel }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </div>
    </el-card>

	<el-card v-if="sourceSnapshot" :header="t('service.query.snapshotTitle')" class="snapshot-card">
	  <el-descriptions :column="2" border>
		<el-descriptions-item :label="t('service.query.snapshotCapturedAt')">
		  {{ formatDate(sourceSnapshot.captured_at) }}
		</el-descriptions-item>
		<el-descriptions-item :label="t('service.query.snapshotStatus')">
		  <el-tag :type="snapshotStatusType">{{ snapshotStatusText }}</el-tag>
		</el-descriptions-item>
		<el-descriptions-item :label="t('service.query.snapshotHash')" :span="2">
		  <code>{{ sourceSnapshot.dependency_hash }}</code>
		</el-descriptions-item>
		<el-descriptions-item v-if="sourceSnapshot.source" :label="t('service.query.snapshotFingerprint')" :span="2">
		  <code>{{ sourceSnapshot.source.item_fingerprint }}</code>
		</el-descriptions-item>
	  </el-descriptions>
	  <div v-if="snapshotDiff?.status === 'changed'" class="snapshot-diff">
		<el-tag v-if="snapshotDiff.source_changed" type="warning">{{ t('service.query.snapshotSourceChanged') }}</el-tag>
		<el-tag v-if="snapshotDiff.table_changed" type="warning">{{ t('service.query.snapshotTableChanged') }}</el-tag>
		<el-tag v-if="snapshotDiff.spatial_changed" type="warning">{{ t('service.query.snapshotSpatialChanged') }}</el-tag>
		<el-tag v-if="snapshotDiff.object_table_changed" type="warning">{{ t('service.query.snapshotObjectChanged') }}</el-tag>
		<el-button type="primary" :loading="snapshotRefreshing" @click="refreshSourceSnapshot">
		  {{ t('service.query.snapshotRefresh') }}
		</el-button>
	  </div>
	</el-card>

    <!-- 服务端点卡片 -->
    <el-card :header="t('service.query.cardEndpoints')" style="margin-bottom: 20px">
      <!-- REST API 端点 -->
      <div v-if="isProtocolEnabled('rest_api')" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>{{ t('service.query.restApiEndpointTitle') }}</span>
        </div>
        <div class="endpoint-url">
          <el-input :value="restApiEndpoint" readonly />
          <el-button @click="copyEndpoint(restApiEndpoint)">{{ t('service.common.copy') }}</el-button>
          <el-button @click="testRestAPI">{{ t('service.common.test') }}</el-button>
        </div>

        <div style="margin-top: 12px; font-size: 13px; color: var(--addp-text-secondary)">
          <strong>{{ t('service.query.supportedParams') }}</strong>
          <ul style="margin: 8px 0; padding-left: 20px">
            <li><code>select</code>：{{ t('service.query.paramFields') }}</li>
            <li><code>filter</code>：{{ t('service.query.paramFilter') }}</li>
            <li><code>order_by</code>：{{ t('service.query.paramOrderBy') }}</li>
            <li><code>page.limit</code> {{ t('service.query.paramAnd') }} <code>page.cursor</code>：{{ t('service.query.paramPage') }}</li>
            <li><code>format</code>：{{ t('service.query.paramFormat') }}</li>
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
          {{ t('service.query.ogcAutoEnabled') }}
        </el-alert>

        <!-- Landing Page -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">Landing Page:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesLandingPage" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesLandingPage)">{{ t('service.common.copy') }}</el-button>
            <el-button @click="testEndpoint(ogcFeaturesLandingPage)">{{ t('service.common.test') }}</el-button>
          </div>
        </div>

        <!-- Collections -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">Collections:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesCollections" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesCollections)">{{ t('service.common.copy') }}</el-button>
            <el-button @click="testEndpoint(ogcFeaturesCollections)">{{ t('service.common.test') }}</el-button>
          </div>
        </div>

        <!-- Conformance -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">Conformance:</div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesConformance" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesConformance)">{{ t('service.common.copy') }}</el-button>
            <el-button @click="testEndpoint(ogcFeaturesConformance)">{{ t('service.common.test') }}</el-button>
          </div>
        </div>

        <!-- Items (实际数据) -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">
            {{ t('service.query.ogcItemsLabel') }}
          </div>
          <div class="endpoint-url">
            <el-input :value="ogcFeaturesItems" readonly />
            <el-button @click="copyEndpoint(ogcFeaturesItems)">{{ t('service.common.copy') }}</el-button>
            <el-button type="primary" @click="testEndpoint(ogcFeaturesItems)">{{ t('service.common.test') }}</el-button>
          </div>
        </div>
      </div>

      <div v-if="!isProtocolEnabled('rest_api') && !isProtocolEnabled('ogc_features')" class="no-endpoints">
        <el-empty :description="t('service.query.noProtocols')" />
      </div>
    </el-card>

    <!-- 数据预览卡片 -->
    <el-card :header="t('service.query.cardPreview')" style="margin-bottom: 20px">
      <div class="preview-controls">
        <el-button
          type="primary"
          @click="loadPreviewData"
          :loading="previewLoading"
          :disabled="!isProtocolEnabled('rest_api')"
        >
          {{ previewData.length > 0 ? t('service.query.refreshData') : t('service.query.loadData') }}
        </el-button>
        <div v-if="previewData.length > 0" style="color: var(--addp-text-secondary); font-size: 14px">
          {{ t('service.query.previewCount', { page: previewPagination.page, count: previewData.length }) }}
        </div>
      </div>

      <div v-if="previewData.length > 0" class="preview-content">
        <TablePreview
          :data="previewTableData"
          :loading="previewLoading"
          @page-change="handlePreviewPageChange"
        />
      </div>

      <!-- 空状态 -->
      <div v-if="previewData.length === 0 && !previewLoading" class="preview-empty">
        <el-empty :description="t('service.query.previewEmpty')" />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Link } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { TablePreview } from '@common-ui-map'
import queryServiceAPI from '@/api/queryService'
import { buildQueryServicePreview, queryServicePreviewFields } from '@/utils/queryServicePreview'
import { copyToClipboard } from '../utils/serviceHelper'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { useConsolePageDescriptor } from '@common-ui'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const service = ref(null)
useConsolePageDescriptor(router, 'service', {
  title: computed(() => t('service.query.recentVisitTitle')),
  subject: computed(() => service.value?.title || service.value?.name || ''),
  ready: computed(() => Boolean(service.value?.title || service.value?.name))
})
const loading = ref(false)
const snapshotChecking = ref(false)
const snapshotRefreshing = ref(false)
const snapshotDiff = ref(null)

// 数据预览相关状态
const previewData = ref([])
const previewLoading = ref(false)
const previewPagination = ref({
  page: 1,
  pageSize: 20,
  hasMore: false,
  nextCursor: '',
  cursors: ['']
})

const serviceId = computed(() => route.params.id)

const sourceSnapshot = computed(() => service.value?.data_config?.source_snapshot || null)
const spatialInfo = computed(() => sourceSnapshot.value?.spatial || null)
const primaryGeometry = computed(() => {
  const columns = spatialInfo.value?.geometry_columns || []
  const primaryName = spatialInfo.value?.primary_geometry_column
  return columns.find((column) => column.name === primaryName) || (columns.length === 1 ? columns[0] : null)
})
const hasGeometry = computed(() => !!primaryGeometry.value?.name)
const geometryTypes = computed(() => primaryGeometry.value?.geometry_type ? [primaryGeometry.value.geometry_type] : [])
const primaryCRSLabel = computed(() => {
  return primaryGeometry.value?.crs_ref || spatialInfo.value?.crs_ref || (primaryGeometry.value?.srid ? `EPSG:${primaryGeometry.value.srid}` : t('service.query.snapshotUnknownCRS'))
})
const snapshotStatusType = computed(() => ['changed', 'unverifiable'].includes(snapshotDisplayStatus.value) ? 'warning' : snapshotDisplayStatus.value === 'current' ? 'success' : 'info')
const snapshotDisplayStatus = computed(() => {
  if (snapshotDiff.value?.status) return snapshotDiff.value.status
  if (sourceSnapshot.value?.verification_status === 'unverifiable') return 'unverifiable'
  return 'unchecked'
})
const snapshotStatusText = computed(() => {
  return t(`service.query.snapshotStatus_${snapshotDisplayStatus.value}`)
})

const previewTableData = computed(() => buildQueryServicePreview({
  rows: previewData.value,
  pagination: {
    page: previewPagination.value.page,
    page_size: previewPagination.value.pageSize,
    has_more: previewPagination.value.hasMore
  },
	spatial: spatialInfo.value
}))

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
  return `${baseURL.value}/api/query/${service.value.service_name}/query`
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
    active: t('service.query.statusActive'),
    inactive: t('service.query.statusInactive'),
    error: t('service.query.statusError')
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
    ElMessage.success(t('service.common.copied'))
  } else {
    ElMessage.error(t('service.common.copyFailed'))
  }
}

// 方法：测试端点
const testEndpoint = (url) => {
  window.open(url, '_blank')
}

// 方法：测试 REST API
const testRestAPI = () => {
  previewPagination.value.page = 1
  previewPagination.value.cursors = ['']
  loadPreviewData()
}

// 方法：加载服务详情
const loadService = async () => {
  loading.value = true
  try {
    const response = await queryServiceAPI.getService(serviceId.value)
    service.value = response
  } catch (error) {
    ElMessage.error(t('service.query.loadServiceFailed') + ': ' + (error.message || t('service.common.unknownError')))
    console.error('Failed to load service:', error)
  } finally {
    loading.value = false
  }
}

// 方法：删除服务
const handleDelete = async () => {
  try {
    await ElMessageBox.confirm(
      t('service.query.deleteServiceConfirm'),
      t('service.common.deleteConfirmTitle'),
      {
        confirmButtonText: t('service.common.confirm'),
        cancelButtonText: t('service.common.cancel'),
        type: 'warning'
      }
    )

    await queryServiceAPI.deleteService(serviceId.value)
    ElMessage.success(t('service.query.deleteServiceSuccess'))
    await navigateServiceRoute(router, '/query-services', { history: 'replace' })
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error(t('service.query.deleteServiceFailed') + ': ' + (error.message || t('service.common.unknownError')))
    }
  }
}

// 方法：导航
const goBack = () => {
  navigateServiceRoute(router, '/query-services', { history: 'replace' })
}

const goToEdit = () => {
  navigateServiceRoute(router, `/query-services/${serviceId.value}/edit`)
}

const checkSourceSnapshot = async () => {
	snapshotChecking.value = true
	try {
	  snapshotDiff.value = await queryServiceAPI.checkSourceSnapshot(serviceId.value)
	} catch (error) {
	  ElMessage.error(t('service.query.snapshotCheckFailed') + ': ' + (error.message || t('service.common.unknownError')))
	} finally {
	  snapshotChecking.value = false
	}
}

const refreshSourceSnapshot = async () => {
	try {
	  await ElMessageBox.confirm(
		t('service.query.snapshotRefreshConfirm'),
		t('service.query.snapshotRefresh'),
		{ confirmButtonText: t('service.common.confirm'), cancelButtonText: t('service.common.cancel'), type: 'warning' }
	  )
	  snapshotRefreshing.value = true
	  service.value = await queryServiceAPI.refreshSourceSnapshot(serviceId.value)
	  snapshotDiff.value = null
	  ElMessage.success(t('service.query.snapshotRefreshSuccess'))
	} catch (error) {
	  if (error !== 'cancel') {
		ElMessage.error(t('service.query.snapshotRefreshFailed') + ': ' + (error.message || t('service.common.unknownError')))
	  }
	} finally {
	  snapshotRefreshing.value = false
	}
}

// 数据预览方法
const loadPreviewData = async () => {
  if (!service.value?.service_name) {
    ElMessage.warning(t('service.query.serviceNotLoaded'))
    return
  }

  previewLoading.value = true
  try {
    const request = {
      page: {
        limit: previewPagination.value.pageSize,
        cursor: previewPagination.value.cursors[previewPagination.value.page - 1] || ''
      },
      format: 'json'
    }
    const fields = queryServicePreviewFields({
      configType: service.value.config_type,
      defaultFields: defaultFields.value,
	  spatial: spatialInfo.value
    })
    if (fields.length > 0) request.select = fields

    const response = await queryServiceAPI.testQuery(service.value.service_name, request)

    if (response && Array.isArray(response.data)) {
      previewData.value = response.data

	  if (response.page) {
		previewPagination.value.pageSize = response.page.limit
		previewPagination.value.hasMore = response.page.has_more === true
		previewPagination.value.nextCursor = response.page.next_cursor || ''
		if (response.page.next_cursor) {
		  previewPagination.value.cursors[previewPagination.value.page] = response.page.next_cursor
		}
      }

      ElMessage.success(t('service.query.loadPreviewSuccess', { count: response.data.length }))
    } else {
      ElMessage.warning(t('service.query.noDataWarning'))
    }
  } catch (error) {
    console.error('加载数据预览失败:', error)
    ElMessage.error(t('service.query.loadPreviewFailed') + ': ' + (error.response?.data?.error || error.message))
  } finally {
    previewLoading.value = false
  }
}

const handlePreviewPageChange = ({ page, pageSize }) => {
	if (pageSize !== previewPagination.value.pageSize) {
	  previewPagination.value.page = 1
	  previewPagination.value.pageSize = pageSize
	  previewPagination.value.cursors = ['']
	  loadPreviewData()
	  return
	}
	if (!previewPagination.value.cursors[page - 1] && page !== 1) return
  previewPagination.value.page = page
  loadPreviewData()
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
  color: var(--addp-text-primary);
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
  color: var(--addp-text-primary);
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
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
}

.sql-query-box pre {
  margin: 0;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--addp-text-primary);
  white-space: pre-wrap;
  word-wrap: break-word;
}

code {
  background-color: var(--addp-bg-secondary);
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

.preview-content {
  height: 720px;
  min-height: 560px;
  margin-top: 16px;
}

.snapshot-card {
  margin-bottom: 20px;
}

.snapshot-diff {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}
</style>
