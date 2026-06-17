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
import {
  quickViewTileAdvisoryAction as tileAdvisoryAction,
  quickViewTileAdvisoryMessage as tileAdvisoryMessage,
  shouldShowQuickViewTileAdvisoryNotice
} from '@/utils/quickViewTileAdvisory'
import {
  buildQuickViewOptimizationCreateQuery,
  buildTileCacheCreateQuery
} from '@/utils/quickViewNavigationQuery'
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
const quickViewTileLastNotice = ref({ key: '', at: 0 })
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
const showQuickViewOptimizationAction = computed(() => {
  const realtime = quickViewStatus.value?.realtime_tile || {}
  const optimization = quickViewStatus.value?.optimization || {}
  return quickViewRenderSource.value === 'realtime_tile' &&
    optimization.available !== true &&
    Number(quickViewStatus.value?.render_facts?.source_srid || 0) !== 3857 &&
    (
      optimization.status === 'stale' ||
      realtime.optimization_recommended === true ||
      realtime.performance_mode === 'source_transform_path' ||
      quickViewTileAdvisory.value?.recommendation === 'quick_view_optimization' ||
      quickViewTileAdvisory.value?.retryPolicy === 'suppress_tile'
    )
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
    quickViewTileLastNotice.value = { key: '', at: 0 }
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
  router.push({
    name: 'TileCache',
    query: buildTileCacheCreateQuery(spatialPreviewNavigationTarget(), quickViewStatus.value)
  })
}

const openQuickViewOptimizationCreate = () => {
  router.push({
    name: 'QuickViewOptimization',
    query: buildQuickViewOptimizationCreateQuery(spatialPreviewNavigationTarget(), quickViewStatus.value)
  })
}

const spatialPreviewNavigationTarget = () => ({
  engineId: engineId.value,
  schema: schema.value,
  table: table.value,
  locator: locator.value,
  itemID: itemId.value,
  geometryColumn: geom.value
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
</style>
