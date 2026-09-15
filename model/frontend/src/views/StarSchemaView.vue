<template>
  <div class="star-schema-view">
    <div class="view-header">
      <span class="view-title">{{ t('model.star_schema.title') }}</span>
      <div class="domain-filter">
        <span id="model-domain-label">{{ t('model.star_schema.domain') }}</span>
        <el-select
          :model-value="selectedDomainId || ''"
          aria-labelledby="model-domain-label"
          filterable
          @change="changeDomain"
        >
          <el-option :label="t('model.star_schema.all_domains')" value="" />
          <el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" />
        </el-select>
      </div>
    </div>

    <el-alert
      v-if="loadError"
      class="load-error"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
    >
      <el-button link type="danger" @click="reload">{{ t('model.common.retry') }}</el-button>
    </el-alert>

    <el-alert
      v-if="!loadError && referenceError"
      class="load-error"
      type="warning"
      :title="referenceError"
      show-icon
      :closable="false"
    />

    <el-row v-if="!loadError" :gutter="16" style="margin-top:12px">
      <!-- 左侧：事实表选择器 -->
      <el-col :xs="24" :md="6">
        <el-card shadow="never" class="fact-list-card">
          <template #header>
            <span class="card-title">{{ t('model.star_schema.fact_tables') }}</span>
          </template>
          <div v-loading="loadingTables">
            <div
              v-for="t2 in factTables"
              :key="t2.id"
              class="fact-item"
              :class="{ active: selectedTableId === t2.id }"
              @click="selectTable(t2)"
            >
              <div class="fact-item-name">{{ t2.name }}</div>
              <div class="fact-item-code">{{ t2.code }}</div>
              <el-tag v-if="t2.layer" type="info" size="small">{{ t2.layer.toUpperCase() }}</el-tag>
            </div>
            <el-empty v-if="!loadingTables && factTables.length === 0" :description="t('model.star_schema.no_fact_tables')" :image-size="60" />
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：维度建模 -->
      <el-col :xs="24" :md="18">
        <div v-if="!selectedTable" class="empty-placeholder">
          <el-empty :description="t('model.star_schema.select_fact_table')" />
        </div>

        <template v-else>
          <!-- 事实表卡片 -->
          <el-card shadow="never" class="fact-detail-card">
            <template #header>
              <div class="fact-detail-header">
                <span class="fact-detail-name">{{ selectedTable.name }}</span>
                <el-tag type="danger" size="small">{{ t('model.star_schema.fact_table_tag') }}</el-tag>
                <el-tag v-if="selectedTable.layer" type="info" size="small">{{ selectedTable.layer.toUpperCase() }}</el-tag>
                <el-tag type="info" size="small" class="owner-domain">{{ selectedTableDomainName }}</el-tag>
                <el-button
                  link
                  type="primary"
                  @click="openLogicalTable(selectedTable.id)"
                >
                  {{ t('model.star_schema.view_detail') }}
                </el-button>
              </div>
            </template>
            <div v-if="selectedTable.grain_description" class="grain-desc">
              <span class="grain-label">{{ t('model.star_schema.grain_label') }}</span>{{ selectedTable.grain_description }}
            </div>
            <div v-if="measureFields.length > 0" class="measure-fields">
              <div class="section-label">{{ t('model.star_schema.measure_fields') }}</div>
              <div class="field-tags">
                <el-tag
                  v-for="f in measureFields"
                  :key="f.id"
                  :type="measureTagType(f.field_role)"
                  size="small"
                >
                  {{ f.name }}
                  <span class="role-hint">{{ measureRoleHint(f.field_role) }}</span>
                </el-tag>
              </div>
            </div>
          </el-card>

          <!-- 关联维度表 + 关联指标 -->
          <el-row :gutter="16" style="margin-top:12px">
            <!-- 关联维度表 -->
            <el-col :xs="24" :lg="12">
              <el-card shadow="never" v-loading="loadingRelated">
                <template #header>
                  <div class="card-header-with-action">
                    <span class="card-title">{{ t('model.star_schema.dimension_relations') }}</span>
                    <el-button link type="primary" @click="openRelation()">
                      {{ t('model.table_relation.view_all') }}
                    </el-button>
                  </div>
                </template>
                <div v-if="dimensionRelations.length === 0" class="empty-hint">{{ t('model.star_schema.no_dimensions') }}</div>
                <div v-for="rel in dimensionRelations" :key="rel.id" class="dim-item">
                  <div class="dim-item-left">
                    <span class="dim-name">{{ rel.target_table_name }}</span>
                    <el-tag v-if="rel.target_scd_type > 0" type="warning" size="small">SCD{{ rel.target_scd_type }}</el-tag>
                    <span class="join-hint">{{ rel.source_table_code }}.{{ rel.source_field_code }} = {{ rel.target_table_code }}.{{ rel.target_field_code }}</span>
                    <el-tag size="small" type="info">{{ rel.relation_type === 'fk' ? 'FK' : 'JOIN' }}</el-tag>
                  </div>
                  <div class="dim-item-actions">
                    <el-button link type="primary" size="small" @click="openRelation(rel)">{{ t('model.table_relation.view') }}</el-button>
                    <el-button link type="primary" size="small" @click="openLogicalTable(rel.target_table)">{{ t('model.table_relation.view_dimension') }}</el-button>
                  </div>
                </div>
              </el-card>
            </el-col>

            <!-- 指标实现 -->
            <el-col :xs="24" :lg="12">
              <el-card shadow="never" v-loading="loadingMetrics">
                <template #header>
                  <span class="card-title">{{ t('model.metric.title') }}</span>
                </template>
                <el-alert v-if="metricLoadError" :title="metricLoadError" type="info" :closable="false" />
                <div v-else-if="factMetrics.length === 0" class="empty-hint">{{ t('model.star_schema.no_metrics') }}</div>
                <div v-for="m in factMetrics" :key="m.id" class="metric-item">
                  <el-button link type="primary" @click="navigateModelRoute(router, { path: `/metric-implementations/${m.id}` })">{{ m.name }}</el-button>
                  <el-tag :type="metricTypeTagType(metricTypeMap[m.metric_definition_id])" size="small">
                    {{ metricNameMap[m.metric_definition_id] || `指标#${m.metric_definition_id}` }} · {{ metricTypeLabel(metricTypeMap[m.metric_definition_id]) }}
                  </el-tag>
                </div>
              </el-card>
            </el-col>
          </el-row>

          <!-- 模型关系图 -->
          <el-card shadow="never" style="margin-top:12px">
            <template #header>
              <span class="card-title">{{ t('model.star_schema.topology') }}</span>
            </template>
            <div ref="mermaidContainer" class="mermaid-container"></div>
          </el-card>
        </template>
      </el-col>
    </el-row>


  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import mermaid from 'mermaid'
import { logicalTableAPI, standardMetricAPI, domainAPI } from '../api/model'
import { useI18n } from 'vue-i18n'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { initializeMermaidTheme, observeThemeChange, readMermaidTheme } from '../utils/mermaidTheme'
import { buildDimensionalModelRouteQuery, resolveDimensionalModelRouteState, buildTableRelationRoute } from '../utils/routeState'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const loadingTables = ref(false)
const loadingRelated = ref(false)
const loadingMetrics = ref(false)
const metricLoadError = ref('')
const loadError = ref('')
const referenceError = ref('')

const factTables = ref([])
const domains = ref([])
const selectedDomainId = computed(() => resolveDimensionalModelRouteState(route.query).domainId)
const selectedTableId = ref(null)
const selectedTable = ref(null)
const tableFields = ref([])
const dimensionRelations = ref([])
const factMetrics = ref([])
const allMetrics = ref([])
const selectedTableDomainName = computed(() => {
  const domainId = selectedTable.value?.domain_id
  if (!domainId) return t('model.star_schema.unassigned_domain')
  return domains.value.find(domain => domain.id === domainId)?.name ||
    t('model.star_schema.unknown_domain', { id: domainId })
})

const mermaidContainer = ref(null)
const mermaidThemeVersion = ref(0)
let stopThemeObserver = null

const metricNameMap = computed(() => {
  const map = {}
  allMetrics.value.forEach(m => { map[m.id] = m.current_revision?.name || m.code })
  return map
})

const metricTypeMap = computed(() => {
  const map = {}
  allMetrics.value.forEach(m => { map[m.id] = m.current_revision?.metric_type })
  return map
})

const measureFields = computed(() =>
  tableFields.value.filter(f => f.field_role && f.field_role.startsWith('measure_'))
)

const mermaidCode = computed(() => {
  mermaidThemeVersion.value
  if (!selectedTable.value) return ''
  const theme = readMermaidTheme()
  const lines = ['flowchart LR']
  const factId = `F${selectedTable.value.id}`
  lines.push(`  ${factId}["${selectedTable.value.name}\\n${t('model.star_schema.fact_table_label')}"]`)
  lines.push(`  style ${factId} fill:${theme.categories[2]},stroke:${theme.nodeStroke},color:${theme.labelLight}`)

  dimensionRelations.value.forEach(rel => {
    const dimId = `D${rel.target_table}`
    lines.push(`  ${dimId}["${rel.target_table_name}\\n${t('model.star_schema.dim_label')}"]`)
    lines.push(`  style ${dimId} fill:${theme.categories[0]},stroke:${theme.nodeStroke},color:${theme.labelLight}`)
    lines.push(`  ${dimId} --> ${factId}`)
  })

  factMetrics.value.forEach(m => {
    const mId = `M${m.id}`
    const mName = m.name || metricNameMap.value[m.metric_definition_id] || `指标#${m.metric_definition_id}`
    lines.push(`  ${mId}["${mName}\\n${t('model.star_schema.metric_label')}"]`)
    lines.push(`  style ${mId} fill:${theme.categories[1]},stroke:${theme.nodeStroke},color:${theme.labelLight}`)
    lines.push(`  ${factId} --> ${mId}`)
  })

  return lines.join('\n')
})

const measureTagType = (role) => {
  if (role === 'measure_additive') return 'success'
  if (role === 'measure_semi') return 'warning'
  if (role === 'measure_non') return 'danger'
  return 'info'
}

const measureRoleHint = (role) => {
  if (role === 'measure_additive') return t('model.star_schema.measure_additive')
  if (role === 'measure_semi') return t('model.star_schema.measure_semi')
  if (role === 'measure_non') return t('model.star_schema.measure_non')
  return ''
}

const metricTypeTagType = (t) => {
  if (t === 'atomic') return 'success'
  if (t === 'derived') return 'warning'
  if (t === 'composite') return 'danger'
  return 'info'
}

const metricTypeLabel = (type) => {
  if (type === 'atomic') return t('model.star_schema.metric_atomic')
  if (type === 'derived') return t('model.star_schema.metric_derived')
  if (type === 'composite') return t('model.star_schema.metric_composite')
  return type || t('model.star_schema.metric_unknown')
}

let selectionGeneration = 0

const clearSelection = () => {
  selectionGeneration += 1
  selectedTableId.value = null
  selectedTable.value = null
  tableFields.value = []
  dimensionRelations.value = []
  factMetrics.value = []
  metricLoadError.value = ''
  loadingRelated.value = false
  loadingMetrics.value = false
}

const loadSelectedTable = async (table) => {
  const requestGeneration = ++selectionGeneration
  selectedTableId.value = table.id
  selectedTable.value = table
  tableFields.value = []
  dimensionRelations.value = []
  factMetrics.value = []
  metricLoadError.value = ''

  loadingRelated.value = true
  loadingMetrics.value = true
  try {
    const [fieldsRes, relationsRes, metricsRes] = await Promise.all([
      logicalTableAPI.getFields(table.id),
      logicalTableAPI.listDimensionRelations(table.id),
      authStore.hasPermission('model.metric_implementation.read')
        ? logicalTableAPI.listMetricImplementations(table.id).then(data => ({ data }), error => ({ error: getModelErrorMessage(error, t, 'model.metric_workspace.load_failed') }))
        : Promise.resolve({ error: t('model.metric_workspace.unavailable') }),
    ])
    if (requestGeneration !== selectionGeneration) return
    tableFields.value = fieldsRes || []
    dimensionRelations.value = relationsRes || []
    factMetrics.value = metricsRes.data || []
    metricLoadError.value = metricsRes.error || ''
  } catch (err) {
    if (requestGeneration === selectionGeneration) {
      loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
    }
  } finally {
    if (requestGeneration === selectionGeneration) {
      loadingRelated.value = false
      loadingMetrics.value = false
    }
  }

}

const selectTable = async (table) => {
  if (selectedTableId.value === table.id) return
  await navigateModelRoute(router, {
    path: '/star-schema',
    query: buildDimensionalModelRouteQuery({ domainId: selectedDomainId.value, tableId: table.id })
  })
}

const changeDomain = async (value) => {
  const domainId = value || null
  const tableId = !domainId || selectedTable.value?.domain_id === domainId
    ? selectedTableId.value
    : null
  await navigateModelRoute(router, {
    path: '/star-schema',
    query: buildDimensionalModelRouteQuery({ domainId, tableId })
  })
}

let routeGeneration = 0
let loadedDomainId

const syncSelectedTableFromRoute = async (forceReload = false) => {
  const generation = ++routeGeneration
  const state = resolveDimensionalModelRouteState(route.query)
  if (state.changed) {
    await navigateModelRoute(router, { path: '/star-schema', query: state.query }, { history: 'replace' })
    return
  }
  loadError.value = ''
  if (!authStore.hasPermission('model.logical_model.read')) {
    loadError.value = t('model.common.permission_denied')
    return
  }

  if (forceReload || loadedDomainId !== state.domainId) {
    clearSelection()
    factTables.value = []
    loadedDomainId = undefined
    loadingTables.value = true
    try {
      const tables = await logicalTableAPI.listAll({
        table_type: 'fact',
        domain_id: state.domainId || undefined
      })
      if (generation !== routeGeneration) return
      factTables.value = tables
      loadedDomainId = state.domainId
    } catch (err) {
      if (generation === routeGeneration) loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
      return
    } finally {
      if (generation === routeGeneration) loadingTables.value = false
    }
  }

  if (!state.tableId) {
    clearSelection()
    return
  }
  const table = factTables.value.find(item => item.id === state.tableId)

  if (!table) {
    clearSelection()
    ElMessage.error(t('model.star_schema.table_unavailable'))
    await navigateModelRoute(router, {
      path: '/star-schema',
      query: buildDimensionalModelRouteQuery({ domainId: state.domainId })
    }, { history: 'replace' })
    return
  }

  if (selectedTableId.value !== table.id) await loadSelectedTable(table)
}

const openLogicalTable = (tableId) => {
  navigateModelRoute(router, `/logical-tables/${tableId}`)
}

const openRelation = relation => navigateModelRoute(router,
  buildTableRelationRoute(selectedTable.value.id, relation?.id, { domain_id: selectedDomainId.value })
)

let renderGeneration = 0
const renderMermaid = async () => {
  const generation = ++renderGeneration
  const container = mermaidContainer.value
  const code = mermaidCode.value
  if (!container || !code) return
  try {
    const { svg } = await mermaid.render('mermaid-model-' + crypto.randomUUID(), code)
    if (generation === renderGeneration && container === mermaidContainer.value && code === mermaidCode.value) {
      container.innerHTML = svg
    }
  } catch (err) {
    console.error('Mermaid渲染错误:', err)
  }
}

watch(mermaidCode, async () => {
  await nextTick()
  renderMermaid()
})

watch(() => route.query, () => syncSelectedTableFromRoute())

const reload = async () => {
  clearSelection()
  loadError.value = ''
  referenceError.value = ''
  if (!authStore.hasPermission('model.logical_model.read')) {
    loadError.value = t('model.common.permission_denied')
    return
  }
  await syncSelectedTableFromRoute(true)
  if (loadError.value) return
  const [domainResult, metricResult] = await Promise.allSettled([domainAPI.list(), standardMetricAPI.listAll()])
  domains.value = domainResult.status === 'fulfilled' ? domainResult.value : []
  allMetrics.value = metricResult.status === 'fulfilled' ? metricResult.value : []
  if (domainResult.status === 'rejected' || metricResult.status === 'rejected') {
    referenceError.value = t('model.common.reference_data_unavailable')
  }
}

onMounted(async () => {
  initializeMermaidTheme(mermaid)
  stopThemeObserver = observeThemeChange(async () => {
    initializeMermaidTheme(mermaid)
    mermaidThemeVersion.value += 1
  })
  await reload()
})

onBeforeUnmount(() => {
  routeGeneration += 1
  selectionGeneration += 1
  renderGeneration += 1
  stopThemeObserver?.()
})
</script>

<style scoped>
.star-schema-view {
  padding: 20px;
}

.view-header {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px 32px;
}

.domain-filter {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  color: var(--addp-text-secondary);
}

.domain-filter > span {
  flex-shrink: 0;
}

.domain-filter .el-select {
  width: 220px;
  min-width: 0;
}

.view-title {
  font-size: 18px;
  font-weight: 600;
}

.fact-list-card {
  overflow-y: visible;
}

.fact-item {
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 6px;
  border: 1px solid var(--addp-border-color-light);
  transition: all 0.2s;
}

.fact-item:hover {
  border-color: var(--el-color-primary);
  background: var(--addp-bg-secondary);
}

.fact-item.active {
  border-color: var(--el-color-primary);
  background: var(--addp-bg-secondary);
}

.fact-item-name {
  font-weight: 500;
  font-size: 14px;
}

.fact-item-code {
  color: var(--addp-text-secondary);
  font-size: 12px;
  margin: 2px 0 4px;
}

.empty-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 400px;
}

.fact-detail-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.fact-detail-name {
  font-size: 16px;
  font-weight: 600;
}

.grain-desc {
  color: var(--addp-text-secondary);
  font-size: 13px;
  margin-bottom: 8px;
}

.grain-label {
  color: var(--addp-text-primary);
  font-weight: 500;
}

.measure-fields {
  margin-top: 6px;
}

.section-label {
  font-weight: 500;
  font-size: 13px;
  margin-bottom: 6px;
}

.field-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.role-hint {
  color: inherit;
  opacity: 0.7;
  font-size: 11px;
}

.card-header-with-action {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.dim-item {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  flex-direction: column;
  gap: 8px;
  padding: 12px 0;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.dim-item:last-child {
  border-bottom: none;
}

.dim-item-left {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.dim-item-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.dim-name {
  font-size: 14px;
  font-weight: 500;
}

.join-hint {
  width: 100%;
  overflow-wrap: anywhere;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.metric-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.metric-item:last-child {
  border-bottom: none;
}

.metric-name {
  font-size: 14px;
}

.mermaid-container {
  overflow-x: auto;
  min-height: 240px;
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
}

.empty-hint {
  color: var(--addp-text-tertiary);
  font-size: 13px;
  text-align: center;
  padding: 20px 0;
}

.card-title {
  font-weight: 600;
}

.load-error {
  margin-top: 12px;
}

@media (max-width: 767px) {
  .star-schema-view {
    padding: 12px;
  }

  .fact-list-card {
    margin-bottom: 16px;
  }

  .fact-detail-header {
    flex-wrap: wrap;
  }
}
</style>
