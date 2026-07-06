<template>
  <div class="page">
    <div class="toolbar">
      <div class="field">{{ sourceLabel }}</div>
      <div class="spacer" />
      <div v-if="quickViewStatus" class="quick-view-actions">
        <el-tooltip
          v-if="quickViewStatus.can_use_quick_view"
          :content="quickViewStatusText"
          placement="bottom"
          :show-after="300"
        >
          <el-icon class="quick-view-status-icon is-success"><Select /></el-icon>
        </el-tooltip>
        <el-tooltip
          v-else-if="showQuickViewUnavailableNotice"
          :content="quickViewUnavailableText"
          placement="bottom"
          :show-after="300"
        >
          <el-icon class="quick-view-status-icon is-info"><InfoFilled /></el-icon>
        </el-tooltip>
        <el-button
          v-if="showBackToBasicPreviewAction"
          size="small"
          @click="backToBasicPreview"
        >
          {{ t('manager.spatialPreview.backToBasicPreview') }}
        </el-button>
        <el-button
          v-else-if="showSwitchQuickViewAction"
          type="primary"
          size="small"
          @click="switchToQuickView"
        >
          {{ t('manager.spatialPreview.switchQuickView') }}
        </el-button>
        <el-button
          v-if="showRasterCOGGenerationAction"
          type="primary"
          size="small"
          :loading="rasterCOGGenerationLoading"
          @click="generateRasterCOG"
        >
          {{ t('manager.spatialPreview.generateRasterCOG') }}
        </el-button>
        <el-button
          v-else-if="showTileCacheGenerationAction"
          type="primary"
          size="small"
          @click="openTileCacheCreate"
        >
          {{ t('manager.spatialPreview.generateTileCache') }}
        </el-button>
        <el-button
          v-if="showVectorMaterializedViewAction"
          size="small"
          type="warning"
          @click="openVectorMaterializedViewCreate"
        >
          {{ t('manager.spatialPreview.optimizeQuickView') }}
        </el-button>
      </div>
    </div>
    <div class="map-wrap">
      <FlatGeobufQuickView
        v-if="ready && isQuickViewActive && quickViewRenderSource === 'direct_flatgeobuf'"
        :status="quickViewStatus"
        class="preview-main"
      />
      <RasterTIFFQuickView
        v-else-if="ready && isQuickViewActive && isRasterQuickView"
        :status="quickViewStatus"
        class="preview-main"
      />
      <VectorTilePreview
        v-else-if="ready && isQuickViewActive && isTileQuickView"
        :locator="locator"
        :engine-id="sourceContext.engineId"
        :schema="sourceContext.schema"
        :table="sourceContext.table"
        :geom="sourceGeometryColumn"
        :cols="cols"
        :tile-url-template="quickViewStatus?.quick_view?.tile_url_template || ''"
        :tile-render-info="quickViewStatus?.quick_view || {}"
        :render-source="quickViewRenderSource"
        :default-tile-cache-id="quickViewStatus?.default_vector_tile_cache_id || ''"
        class="preview-main"
        @tile-advisory="handleTileAdvisory"
      />
      <TablePreview
        v-else-if="ready && basicPreviewData"
        :data="basicPreviewData"
        :loading="basicPreviewLoading"
        class="preview-main"
        @page-change="handleBasicPreviewPageChange"
      />
      <el-empty
        v-else-if="ready && !basicPreviewLoading"
        :description="t('manager.explorer.emptyPreview.noData')"
        :image-size="80"
        class="basic-preview-empty"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { InfoFilled, Select } from '@element-plus/icons-vue'
import { formatLocatorDisplayPath } from '@addp/common-frontend'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import FlatGeobufQuickView from '@/components/map/FlatGeobufQuickView.vue'
import RasterTIFFQuickView from '@/components/map/RasterTIFFQuickView.vue'
import { quickViewAPI } from '@/api/quickView'
import client from '@/api/client'
import {
  quickViewTileAdvisoryAction as tileAdvisoryAction,
  quickViewTileAdvisoryMessage as tileAdvisoryMessage,
  shouldShowQuickViewTileAdvisoryNotice
} from '@/utils/quickViewTileAdvisory'
import { quickViewReasonText } from '@/utils/quickViewReasonText'
import {
  buildVectorMaterializedViewCreateQuery,
  buildTileCacheCreateQuery
} from '@/utils/quickViewNavigationQuery'
import {
  waitForRasterCOGExecution
} from '@/utils/rasterCOGTask'
import {
  hasQuickViewAction,
  isRasterQuickViewRenderSource,
  isTileQuickViewRenderSource,
  resolveQuickViewRenderSource,
  shouldShowQuickViewUnavailableNotice as shouldShowQuickViewUnavailableNoticeForStatus,
  shouldLoadBasicPreview,
  shouldUseBackendQuickViewRenderer
} from '@/utils/quickViewRenderSource'
import { TablePreview } from '@common-ui-map'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const locator = computed(() => String(route.query.locator || '').trim())
const itemId = computed(() => Number(route.query.item_id || route.query.itemId || 0))
const cols = computed(() => String(route.query.cols || '').split(',').filter(Boolean))
const ready = computed(() => !!locator.value)
const quickViewStatus = ref(null)
const sourceContext = ref({
  engineId: 0,
  schema: '',
  table: '',
  geometryColumn: ''
})
const activePreviewMode = ref('basic_preview')
const quickViewTileAdvisory = ref(null)
const quickViewTileLastNotice = ref({ key: '', at: 0 })
const rasterCOGGenerationLoading = ref(false)
const quickViewRenderSource = computed(() => resolveQuickViewRenderSource(quickViewStatus.value))
const isRasterQuickView = computed(() => isRasterQuickViewRenderSource(quickViewRenderSource.value))
const isTileQuickView = computed(() => isTileQuickViewRenderSource(quickViewRenderSource.value))
const sourceGeometryColumn = computed(() => String(
  quickViewStatus.value?.quick_view?.geometry_column ||
  sourceContext.value.geometryColumn ||
  ''
).trim())
const sourceLabel = computed(() => {
  const schema = String(sourceContext.value.schema || '').trim()
  const table = String(sourceContext.value.table || '').trim()
  if (schema && table) return `${schema}.${table}`
  const parsed = quickViewStatus.value?.locator || locator.value
  return formatLocatorDisplayPath(parsed) || locator.value
})
const isQuickViewActive = computed(() => {
  return shouldUseBackendQuickViewRenderer(activePreviewMode.value, quickViewStatus.value)
})
const canUseQuickViewAction = (action) => hasQuickViewAction(quickViewStatus.value, action)
const showBackToBasicPreviewAction = computed(() => {
  return canUseQuickViewAction('back_to_basic_preview')
})
const showSwitchQuickViewAction = computed(() => {
  return canUseQuickViewAction('switch_quick_view')
})
const showRasterCOGGenerationAction = computed(() => {
  return canUseQuickViewAction('generate_raster_cog')
})
const showTileCacheGenerationAction = computed(() => {
  return canUseQuickViewAction('generate_vector_tile_cache')
})
const isExternalOptimizationTarget = computed(() => {
  return quickViewStatus.value?.optimization?.target_kind === 'external_3857_materialized_view'
})
const isStaleOptimizationTarget = computed(() => {
  return quickViewStatus.value?.optimization?.status === 'stale'
})
const quickViewStatusText = computed(() => {
  if (isExternalOptimizationTarget.value) {
    return t('manager.spatialPreview.externalOptimizationTargetReady')
  }
  if (isStaleOptimizationTarget.value) {
    return t('manager.spatialPreview.optimizationTargetStale')
  }
  return t('manager.spatialPreview.quickViewReady')
})

const quickViewUnavailableText = computed(() => {
  return quickViewReasonText(t, quickViewStatus.value?.unavailable_reason) ||
    t('manager.spatialPreview.quickViewUnavailable')
})

const showQuickViewUnavailableNotice = computed(() => {
  return shouldShowQuickViewUnavailableNoticeForStatus(quickViewStatus.value)
})

const showVectorMaterializedViewAction = computed(() => {
  return canUseQuickViewAction('generate_vector_materialized_view')
})
const basicPreviewData = ref(null)
const basicPreviewLoading = ref(false)
let basicPreviewRequestSeq = 0

const unpackPreviewResponse = (response) => {
  if (response?.preview_type && response?.data) return response.data
  return response?.data || response
}

const loadBasicPreview = async (page = 1, pageSize = 20) => {
  basicPreviewRequestSeq += 1
  const seq = basicPreviewRequestSeq
  basicPreviewData.value = null
  if (!ready.value) return
  basicPreviewLoading.value = true
  try {
    const response = await client.get('/manager/preview', {
      params: {
        locator: locator.value,
        page,
        page_size: pageSize
      }
    })
    if (seq === basicPreviewRequestSeq) {
      basicPreviewData.value = unpackPreviewResponse(response)
    }
  } catch (error) {
    console.error('加载基础预览失败:', error)
    if (seq === basicPreviewRequestSeq) {
      basicPreviewData.value = null
    }
  } finally {
    if (seq === basicPreviewRequestSeq) {
      basicPreviewLoading.value = false
    }
  }
}

const handleBasicPreviewPageChange = async (payload) => {
  const page = typeof payload === 'object' ? payload?.page || 1 : payload
  const pageSize = typeof payload === 'object' ? Number(payload?.pageSize || 20) : 20
  await loadBasicPreview(page, pageSize > 0 ? pageSize : 20)
}

const clearBasicPreview = () => {
  basicPreviewRequestSeq += 1
  basicPreviewData.value = null
  basicPreviewLoading.value = false
}

const loadQuickViewStatus = async () => {
  quickViewStatus.value = null
  if (!ready.value) return
  try {
    quickViewStatus.value = await quickViewAPI.getQuickViewCapabilityByLocator(locator.value)
    sourceContext.value = {
      engineId: Number(quickViewStatus.value?.source_engine_id || 0),
      schema: String(quickViewStatus.value?.source_schema || '').trim(),
      table: String(quickViewStatus.value?.source_table || '').trim(),
      geometryColumn: String(quickViewStatus.value?.quick_view?.geometry_column || '').trim()
    }
    quickViewTileAdvisory.value = null
    quickViewTileLastNotice.value = { key: '', at: 0 }
    if (quickViewStatus.value?.preferred_mode === 'map_quick_view' && quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'map_quick_view'
    } else {
      activePreviewMode.value = 'basic_preview'
    }
  } catch (error) {
    console.error('加载快显状态失败:', error)
  }
}

const switchToQuickView = async () => {
  try {
    await quickViewAPI.updatePreferredModeByLocator(locator.value, 'map_quick_view')
    ElMessage.success(t('manager.spatialPreview.switchQuickViewSuccess'))
    await loadQuickViewStatus()
    if (quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'map_quick_view'
    }
  } catch (error) {
    console.error('切换快显失败:', error)
    ElMessage.error(t('manager.spatialPreview.switchQuickViewFailed'))
  }
}

const backToBasicPreview = async () => {
  try {
    await quickViewAPI.updatePreferredModeByLocator(locator.value, 'basic_preview')
    activePreviewMode.value = 'basic_preview'
    await loadQuickViewStatus()
    if (shouldLoadBasicPreview(activePreviewMode.value, quickViewStatus.value)) {
      await loadBasicPreview()
    }
  } catch (error) {
    console.error('返回基础预览失败:', error)
    activePreviewMode.value = 'basic_preview'
    await loadBasicPreview()
  }
}

const openTileCacheCreate = () => {
  router.push({
    name: 'TileCache',
    query: buildTileCacheCreateQuery(spatialPreviewNavigationTarget(), quickViewStatus.value)
  })
}

const openVectorMaterializedViewCreate = () => {
  router.push({
    name: 'VectorMaterializedView',
    query: buildVectorMaterializedViewCreateQuery(spatialPreviewNavigationTarget(), quickViewStatus.value)
  })
}

const generateRasterCOG = async () => {
  if (!quickViewStatus.value || !locator.value) return
  rasterCOGGenerationLoading.value = true
  try {
    const execution = await quickViewAPI.executeQuickViewAction(locator.value, 'generate_raster_cog')
    ElMessage.success(t('manager.spatialPreview.generateRasterCOGSubmitted'))
    const executionID = String(execution?.execution_id || execution?.data?.execution_id || '').trim()
    if (executionID) {
      const result = await waitForRasterCOGExecution(
        executionID,
        (id) => quickViewAPI.getExecutionStatus(id)
      )
      if (result.success) {
        ElMessage.success(t('manager.spatialPreview.generateRasterCOGReady'))
      } else if (result.failed) {
        ElMessage.error(t('manager.spatialPreview.generateRasterCOGExecutionFailed'))
      } else if (!result.completed) {
        ElMessage.warning(t('manager.spatialPreview.generateRasterCOGTimeout'))
      }
    }
    await loadQuickViewStatus()
  } catch (error) {
    console.error('提交 栅格快显 COG生成失败:', error)
    ElMessage.error(t('manager.spatialPreview.generateRasterCOGFailed'))
  } finally {
    rasterCOGGenerationLoading.value = false
  }
}

const spatialPreviewNavigationTarget = () => ({
  engineId: sourceContext.value.engineId,
  schema: sourceContext.value.schema,
  table: sourceContext.value.table,
  locator: locator.value,
  itemID: itemId.value,
  geometryColumn: sourceGeometryColumn.value
})

const handleTileAdvisory = (advisory) => {
  quickViewTileAdvisory.value = advisory
  const action = tileAdvisoryAction(advisory, quickViewStatus.value)
  const notice = shouldShowQuickViewTileAdvisoryNotice(quickViewTileLastNotice.value, action, advisory, quickViewStatus.value)
  quickViewTileLastNotice.value = notice.lastNotice
  if (notice.show) {
    ElMessage.warning(tileAdvisoryMessage(t, action, advisory, quickViewStatus.value))
  }
}

const loadSpatialPreview = async ({ reset = false } = {}) => {
  if (reset) {
    activePreviewMode.value = 'basic_preview'
    clearBasicPreview()
    sourceContext.value = { engineId: 0, schema: '', table: '', geometryColumn: '' }
  }
  await loadQuickViewStatus()
  if (!shouldLoadBasicPreview(activePreviewMode.value, quickViewStatus.value)) {
    clearBasicPreview()
    return
  }
  await loadBasicPreview()
}

onMounted(() => {
  loadSpatialPreview()
})
watch(locator, () => {
  activePreviewMode.value = 'basic_preview'
  loadSpatialPreview({ reset: true })
})
</script>

<style scoped>
.page { position: relative; width: 100%; height: 100%; display: flex; flex-direction: column; }
.toolbar { height: 44px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--addp-border-color-light); }
.spacer { flex: 1; }
.map-wrap { position: relative; flex: 1; display: flex; flex-direction: column; min-height: 0; }
.preview-main { flex: 1; min-height: 0; }
.basic-preview-empty { height: 100%; display: flex; align-items: center; justify-content: center; }
.quick-view-actions { display: flex; align-items: center; gap: 10px; }
.quick-view-status-icon {
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 24px;
  font-size: 16px;
  cursor: help;
}
.quick-view-status-icon.is-success { color: var(--el-color-success); }
.quick-view-status-icon.is-info { color: var(--el-color-info); }
</style>
