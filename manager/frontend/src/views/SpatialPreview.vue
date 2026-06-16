<template>
  <div class="page">
    <div class="toolbar">
      <div class="field">{{ schema }}.{{ table }}</div>
      <div class="spacer" />
      <div v-if="quickViewStatus" class="quick-view-actions">
        <span v-if="quickViewStatus.can_use_quick_view" class="status-text">
          {{ quickViewStatusText }}
        </span>
        <span v-else-if="!quickViewStatus.can_generate_tile_cache" class="status-text muted">
          {{ quickViewStatus.unavailable_reason || t('manager.spatialPreview.quickViewUnavailable') }}
        </span>
        <el-button
          v-if="isQuickViewActive"
          size="small"
          @click="backToBasicPreview"
        >
          {{ t('manager.spatialPreview.backToBasicPreview') }}
        </el-button>
        <el-button
          v-else-if="quickViewStatus.can_use_quick_view"
          type="primary"
          size="small"
          @click="switchToQuickView"
        >
          {{ t('manager.spatialPreview.switchQuickView') }}
        </el-button>
        <el-button
          v-else-if="quickViewStatus.can_generate_tile_cache"
          type="primary"
          size="small"
          @click="openTileCacheCreate"
        >
          {{ t('manager.spatialPreview.generateTileCache') }}
        </el-button>
        <el-button
          v-if="showRealtimeTileCacheGeneration"
          size="small"
          @click="openTileCacheCreate"
        >
          {{ t('manager.spatialPreview.generateTileCache') }}
        </el-button>
        <el-button
          v-if="showQuickViewOptimizationAction"
          size="small"
          type="warning"
          @click="openQuickViewOptimizationCreate"
        >
          {{ t('manager.spatialPreview.optimizeQuickView') }}
        </el-button>
      </div>
    </div>
    <div class="map-wrap">
      <el-alert
        v-if="showQuickViewTileAdvisory"
        class="tile-advisory"
        :title="t('manager.spatialPreview.tileTimeoutAdvisoryTitle')"
        type="warning"
        :closable="false"
        show-icon
      >
        <template #default>
          <div class="tile-advisory-body">
            <span>{{ quickViewTileAdvisoryMessage }}</span>
            <el-button
              size="small"
              :type="quickViewTileAdvisoryAction === 'tile_cache_generation' ? 'primary' : 'warning'"
              @click="handleQuickViewTileAdvisoryAction"
            >
              {{ quickViewTileAdvisoryActionLabel }}
            </el-button>
          </div>
        </template>
      </el-alert>
      <GeoJSONQuickView
        v-if="ready && isQuickViewActive && quickViewRenderSource === 'direct_geojson'"
        :status="quickViewStatus"
        class="preview-main"
      />
      <VectorTilePreview
        v-else-if="ready && isQuickViewActive && isTileQuickView"
        :locator="locator"
        :engine-id="engineId"
        :schema="schema"
        :table="table"
        :geom="geom"
        :cols="cols"
        :tile-url-template="quickViewStatus?.quick_view?.tile_url_template || ''"
        :tile-render-info="quickViewStatus?.quick_view || {}"
        :render-source="quickViewRenderSource"
        :default-tile-cache-id="quickViewStatus?.default_tile_cache_id || ''"
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
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import GeoJSONQuickView from '@/components/map/GeoJSONQuickView.vue'
import { quickViewAPI } from '@/api/quickView'
import client from '@/api/client'
import { TablePreview } from '@common-ui-map'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const engineId = computed(() => Number(route.query.engine_id || route.query.engineId || 0))
const schema = computed(() => String(route.query.schema || 'public'))
const table = computed(() => String(route.query.table || ''))
const locator = computed(() => String(route.query.locator || '').trim())
const itemId = computed(() => Number(route.query.item_id || route.query.itemId || 0))
const geom = computed(() => route.query.geom ? String(route.query.geom) : '')
const cols = computed(() => String(route.query.cols || '').split(',').filter(Boolean))
const ready = computed(() => !!locator.value)
const quickViewStatus = ref(null)
const activePreviewMode = ref('table_geojson')
const quickViewTileAdvisory = ref(null)
const quickViewRenderSource = computed(() => String(
  quickViewStatus.value?.render_source || quickViewStatus.value?.quick_view?.render_source || ''
).trim())
const isTileQuickView = computed(() => ['cached_tile', 'realtime_tile'].includes(quickViewRenderSource.value))
const isQuickViewActive = computed(() => {
  return activePreviewMode.value === 'quick_view' && !!quickViewStatus.value?.can_use_quick_view
})
const showRealtimeTileCacheGeneration = computed(() => {
  return quickViewRenderSource.value === 'realtime_tile' && !!quickViewStatus.value?.can_generate_tile_cache
})
const isExternalOptimizationTarget = computed(() => {
  return quickViewStatus.value?.optimization?.target_kind === 'external_3857_materialized_view'
})
const quickViewStatusText = computed(() => {
  if (isExternalOptimizationTarget.value) {
    return t('manager.spatialPreview.externalOptimizationTargetReady')
  }
  return t('manager.spatialPreview.quickViewReady')
})
const showQuickViewOptimizationAction = computed(() => {
  const realtime = quickViewStatus.value?.realtime_tile || {}
  const optimization = quickViewStatus.value?.optimization || {}
  return quickViewRenderSource.value === 'realtime_tile' &&
    optimization.available !== true &&
    Number(quickViewStatus.value?.render_facts?.source_srid || 0) !== 3857 &&
    (
      realtime.optimization_recommended === true ||
      realtime.performance_mode === 'source_transform_path' ||
      quickViewTileAdvisory.value?.recommendation === 'quick_view_optimization' ||
      quickViewTileAdvisory.value?.retryPolicy === 'suppress_tile'
    )
})
const quickViewTileAdvisoryAction = computed(() => {
  const advisory = quickViewTileAdvisory.value || {}
  if (advisory.recommendation === 'tile_cache_generation') return 'tile_cache_generation'
  if (advisory.recommendation === 'quick_view_optimization' || advisory.retryPolicy === 'suppress_tile') {
    return 'quick_view_optimization'
  }
  return ''
})
const quickViewTileAdvisoryMessage = computed(() => {
  if (quickViewTileAdvisoryAction.value === 'tile_cache_generation') {
    return t('manager.spatialPreview.tileTimeoutCacheRecommended')
  }
  if (quickViewTileAdvisoryAction.value === 'quick_view_optimization') {
    return t('manager.spatialPreview.tileTimeoutOptimizationRecommended')
  }
  return ''
})
const quickViewTileAdvisoryActionLabel = computed(() => {
  if (quickViewTileAdvisoryAction.value === 'tile_cache_generation') {
    return t('manager.spatialPreview.generateTileCache')
  }
  return t('manager.spatialPreview.optimizeQuickView')
})
const showQuickViewTileAdvisory = computed(() => {
  return isQuickViewActive.value && !!quickViewTileAdvisoryMessage.value && !!quickViewTileAdvisoryAction.value
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

const loadQuickViewStatus = async () => {
  quickViewStatus.value = null
  if (!ready.value) return
  try {
    quickViewStatus.value = await quickViewAPI.getQuickViewCapabilityByLocator(locator.value)
    quickViewTileAdvisory.value = null
    if (quickViewStatus.value?.preferred_mode === 'quick_view' && quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'quick_view'
    } else if (!quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'table_geojson'
    }
  } catch (error) {
    console.error('加载快显状态失败:', error)
  }
}

const switchToQuickView = async () => {
  try {
    await quickViewAPI.updatePreferredModeByLocator(locator.value, 'quick_view')
    ElMessage.success(t('manager.spatialPreview.switchQuickViewSuccess'))
    await loadQuickViewStatus()
    if (quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'quick_view'
    }
  } catch (error) {
    console.error('切换快显失败:', error)
    ElMessage.error(t('manager.spatialPreview.switchQuickViewFailed'))
  }
}

const backToBasicPreview = async () => {
  try {
    await quickViewAPI.updatePreferredModeByLocator(locator.value, 'table_geojson')
    activePreviewMode.value = 'table_geojson'
    await loadQuickViewStatus()
  } catch (error) {
    console.error('返回基础预览失败:', error)
    activePreviewMode.value = 'table_geojson'
  }
}

const openTileCacheCreate = () => {
  const quickView = quickViewStatus.value?.quick_view || {}
  const renderFacts = quickViewStatus.value?.render_facts || {}
  const renderExtent = Array.isArray(renderFacts.render_extent) ? renderFacts.render_extent : quickView.extent
  const renderExtentSRID = renderFacts.render_extent_srid || quickView.extent_srid
  const geometryColumn = quickView.geometry_column || geom.value
  const sourceSRID = renderFacts.source_srid || quickView.source_srid
  router.push({
    name: 'TileCache',
    query: {
      tab: 'tasks',
      create: '1',
      engine_id: String(engineId.value),
      schema: schema.value,
      table: table.value,
      locator: locator.value,
      ...(itemId.value ? { item_id: String(itemId.value) } : {}),
      ...(geometryColumn ? { geom: geometryColumn } : {}),
      ...(quickViewStatus.value?.item_fingerprint ? { item_fingerprint: quickViewStatus.value.item_fingerprint } : {}),
      ...(sourceSRID ? { source_srid: String(sourceSRID) } : {}),
      ...(renderExtentSRID ? { extent_srid: String(renderExtentSRID) } : {}),
      ...(Array.isArray(renderExtent) && renderExtent.length === 4 ? { extent: renderExtent.join(',') } : {})
    }
  })
}

const openQuickViewOptimizationCreate = () => {
  const quickView = quickViewStatus.value?.quick_view || {}
  const renderFacts = quickViewStatus.value?.render_facts || {}
  const geometryColumn = quickView.geometry_column || geom.value
  const geometryColumns = Array.isArray(quickView.geometry_columns) ? quickView.geometry_columns : []
  const sourceSRID = renderFacts.source_srid || quickView.source_srid
  router.push({
    name: 'QuickViewOptimization',
    query: {
      tab: 'tasks',
      create: '1',
      engine_id: String(engineId.value),
      schema: schema.value,
      table: table.value,
      locator: locator.value,
      ...(itemId.value ? { item_id: String(itemId.value) } : {}),
      ...(geometryColumn ? { geom: geometryColumn } : {}),
      ...(geometryColumns.length ? { geometry_columns: geometryColumns.join(',') } : {}),
      ...(quickViewStatus.value?.item_fingerprint ? { item_fingerprint: quickViewStatus.value.item_fingerprint } : {}),
      ...(sourceSRID ? { source_srid: String(sourceSRID) } : {})
    }
  })
}

const handleTileAdvisory = (advisory) => {
  quickViewTileAdvisory.value = advisory
  if (advisory?.retryPolicy === 'suppress_tile') {
    ElMessage.warning(t('manager.spatialPreview.tileTimeoutOptimizationRecommended'))
  } else if (advisory?.recommendation === 'tile_cache_generation') {
    ElMessage.warning(t('manager.spatialPreview.tileTimeoutCacheRecommended'))
  }
}

const handleQuickViewTileAdvisoryAction = () => {
  if (quickViewTileAdvisoryAction.value === 'tile_cache_generation') {
    openTileCacheCreate()
    return
  }
  if (quickViewTileAdvisoryAction.value === 'quick_view_optimization') {
    openQuickViewOptimizationCreate()
  }
}

onMounted(() => {
  loadQuickViewStatus()
  loadBasicPreview()
})
watch([engineId, schema, table, locator], () => {
  activePreviewMode.value = 'table_geojson'
  basicPreviewRequestSeq += 1
  basicPreviewData.value = null
  loadQuickViewStatus()
  loadBasicPreview()
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
.status-text { font-size: 13px; color: var(--addp-text-secondary); }
.status-text.muted { color: var(--addp-text-tertiary); }
.tile-advisory {
  margin: 10px 12px;
}
.tile-advisory-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
</style>
