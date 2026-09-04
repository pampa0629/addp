<template>
  <div class="star-schema-view">
    <div class="view-header">
      <span class="view-title">{{ t('model.star_schema.title') }}</span>
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

      <!-- 右侧：星型图 -->
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
                    <el-button v-if="canEditSelectedTable" type="primary" size="small" @click="openAddDimDialog">
                      {{ t('model.star_schema.add_relation') }}
                    </el-button>
                  </div>
                </template>
                <div v-if="dimensionRelations.length === 0" class="empty-hint">{{ t('model.star_schema.no_dimensions') }}</div>
                <div v-for="rel in dimensionRelations" :key="rel.id" class="dim-item">
                  <div class="dim-item-left">
                    <span class="dim-name">{{ rel.target_table_name }}</span>
                    <el-tag v-if="rel.target_scd_type > 0" type="warning" size="small">SCD{{ rel.target_scd_type }}</el-tag>
                    <span class="join-hint">{{ rel.source_field_name }} → {{ rel.target_field_name }}</span>
                    <el-tag size="small" type="info">{{ rel.relation_type === 'fk' ? 'FK' : 'JOIN' }}</el-tag>
                  </div>
                  <div class="dim-item-actions">
                    <el-button
                      link
                      type="primary"
                      size="small"
                      @click="openLogicalTable(rel.target_table)"
                    >
                      {{ t('model.star_schema.detail') }}
                    </el-button>
                    <el-button
                      v-if="canModifyDimensionRelation(rel)"
                      link
                      type="danger"
                      size="small"
                      @click="handleRemoveDimRelation(rel)"
                    >
                      {{ t('model.star_schema.remove') }}
                    </el-button>
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
                <div v-if="factMetrics.length === 0" class="empty-hint">{{ t('model.star_schema.no_metrics') }}</div>
                <div v-for="m in factMetrics" :key="m.id" class="metric-item">
                  <span class="metric-name">{{ m.name }}</span>
                  <el-tag :type="metricTypeTagType(metricTypeMap[m.metric_definition_id])" size="small">
                    {{ metricNameMap[m.metric_definition_id] || `指标#${m.metric_definition_id}` }} · {{ metricTypeLabel(metricTypeMap[m.metric_definition_id]) }}
                  </el-tag>
                </div>
              </el-card>
            </el-col>
          </el-row>

          <!-- Mermaid 星型图 -->
          <el-card shadow="never" style="margin-top:12px">
            <template #header>
              <span class="card-title">{{ t('model.star_schema.topology') }}</span>
            </template>
            <div ref="mermaidContainer" class="mermaid-container"></div>
          </el-card>
        </template>
      </el-col>
    </el-row>

    <!-- 添加维度关联对话框 -->
    <el-dialog v-model="addDimDialogVisible" class="addp-dialog" :title="t('model.star_schema.add_dim_title')" width="min(480px, calc(100vw - 32px))" :close-on-click-modal="false">
      <el-form :model="addDimForm" label-width="100px" ref="addDimFormRef">
        <el-form-item :label="t('model.star_schema.dim_table')" prop="target_table" :rules="[{ required: true, message: t('model.star_schema.dim_table_required') }]">
          <el-select
            v-model="addDimForm.target_table"
            :placeholder="t('model.star_schema.dim_table_placeholder')"
            style="width:100%"
            filterable
            @change="onDimTableChange"
          >
            <el-option
              v-for="dim in editableDimensionTables"
              :key="dim.id"
              :label="dim.name"
              :value="dim.id"
            >
              <span>{{ dim.name }}</span>
              <span style="color:var(--addp-text-secondary);font-size:12px;margin-left:8px">{{ dim.code }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.star_schema.source_field')" prop="source_field" :rules="[{ required: true, message: t('model.star_schema.source_field_required') }]">
          <el-select v-model="addDimForm.source_field" :placeholder="t('model.star_schema.source_field_placeholder')" style="width:100%" filterable>
            <el-option
              v-for="f in tableFields"
              :key="f.id"
              :label="f.name"
              :value="f.id"
            >
              <span>{{ f.name }}</span>
              <span style="color:var(--addp-text-secondary);font-size:12px;margin-left:8px">{{ f.column_name }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.star_schema.target_field')" prop="target_field" :rules="[{ required: true, message: t('model.star_schema.target_field_required') }]">
          <el-select
            v-model="addDimForm.target_field"
            :placeholder="t('model.star_schema.target_field_placeholder')"
            style="width:100%"
            filterable
            :disabled="!addDimForm.target_table"
          >
            <el-option
              v-for="f in dimTableFields"
              :key="f.id"
              :label="f.name"
              :value="f.id"
            >
              <span>{{ f.name }}</span>
              <span style="color:var(--addp-text-secondary);font-size:12px;margin-left:8px">{{ f.column_name }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.star_schema.relation_type')">
          <el-radio-group v-model="addDimForm.relation_type">
            <el-radio value="fk">{{ t('model.star_schema.fk') }}</el-radio>
            <el-radio value="join">{{ t('model.star_schema.join') }}</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDimDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" :loading="addingDim" @click="handleAddDimRelation">{{ t('model.star_schema.confirm_relation') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import mermaid from 'mermaid'
import { logicalTableAPI, standardMetricAPI } from '../api/model'
import { useI18n } from 'vue-i18n'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { initializeMermaidTheme, observeThemeChange, readMermaidTheme } from '../utils/mermaidTheme'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const loadingTables = ref(false)
const loadingRelated = ref(false)
const loadingMetrics = ref(false)
const loadError = ref('')
const referenceError = ref('')

const factTables = ref([])
const factTablesReady = ref(false)
const selectedTableId = ref(null)
const selectedTable = ref(null)
const tableFields = ref([])
const dimensionRelations = ref([])
const factMetrics = ref([])
const allMetrics = ref([])

// 添加维度关联对话框
const addDimDialogVisible = ref(false)
const addingDim = ref(false)
const allDimensionTables = ref([])
const dimTableFields = ref([])
const addDimFormRef = ref(null)
const mermaidContainer = ref(null)
const mermaidThemeVersion = ref(0)
let stopThemeObserver = null
const addDimForm = ref({
  target_table: null,
  source_field: null,
  target_field: null,
  relation_type: 'fk'
})

const canEditSelectedTable = computed(() =>
  selectedTable.value?.status === 'draft' && authStore.hasPermission('model.logical_model.update')
)
const editableDimensionTables = computed(() =>
  allDimensionTables.value.filter(table => table.status === 'draft')
)
const canModifyDimensionRelation = relation => {
  if (!canEditSelectedTable.value) return false
  return allDimensionTables.value.find(table => table.id === relation.target_table)?.status === 'draft'
}

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

const loadFactTables = async () => {
  loadingTables.value = true
  loadError.value = ''
  try {
    const res = await logicalTableAPI.listAll({ table_type: 'fact' })
    factTables.value = res
  } catch (err) {
    factTables.value = []
    loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
  } finally {
    loadingTables.value = false
    factTablesReady.value = true
  }
}

const loadAllDimensionTables = async () => {
  try {
    const res = await logicalTableAPI.listAll({ table_type: 'dimension' })
    allDimensionTables.value = res
  } catch (err) {
    allDimensionTables.value = []
    referenceError.value = t('model.common.reference_data_unavailable')
  }
}

let selectionGeneration = 0

const clearSelection = () => {
  selectionGeneration += 1
  selectedTableId.value = null
  selectedTable.value = null
  tableFields.value = []
  dimensionRelations.value = []
  factMetrics.value = []
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

  loadingRelated.value = true
  loadingMetrics.value = true
  try {
    const [fieldsRes, relationsRes, metricsRes] = await Promise.all([
      logicalTableAPI.getFields(table.id),
      logicalTableAPI.listDimensionRelations(table.id),
      logicalTableAPI.listMetricImplementations(table.id),
    ])
    if (requestGeneration !== selectionGeneration) return
    tableFields.value = fieldsRes || []
    dimensionRelations.value = relationsRes || []
    factMetrics.value = metricsRes || []
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

  if (requestGeneration !== selectionGeneration) return
  await nextTick()
  renderMermaid()
}

const selectTable = async (table) => {
  const tableId = String(table.id)
  if (route.query.table_id === tableId) {
    if (selectedTableId.value !== table.id) await loadSelectedTable(table)
    return
  }
  await navigateModelRoute(router, {
    path: '/star-schema',
    query: { table_id: tableId }
  }, { history: 'replace' })
}

const syncSelectedTableFromRoute = async () => {
  if (!factTablesReady.value) return

  const tableId = route.query.table_id
  if (tableId === undefined) {
    clearSelection()
    return
  }

  const table = typeof tableId === 'string'
    ? factTables.value.find(item => String(item.id) === tableId)
    : null

  if (!table) {
    clearSelection()
    await navigateModelRoute(router, '/star-schema', { history: 'replace' })
    return
  }

  if (selectedTableId.value !== table.id) await loadSelectedTable(table)
}

const openLogicalTable = (tableId) => {
  navigateModelRoute(router, `/logical-tables/${tableId}`)
}

const openAddDimDialog = () => {
  addDimForm.value = { target_table: null, source_field: null, target_field: null, relation_type: 'fk' }
  dimTableFields.value = []
  addDimDialogVisible.value = true
}

const onDimTableChange = async (dimTableId) => {
  addDimForm.value.target_field = null
  dimTableFields.value = []
  if (!dimTableId) return
  try {
    const res = await logicalTableAPI.getFields(dimTableId)
    dimTableFields.value = res || []
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.star_schema.load_failed'))
  }
}

const handleAddDimRelation = async () => {
  if (!canEditSelectedTable.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (!addDimFormRef.value) return
  try {
    await addDimFormRef.value.validate()
  } catch {
    return
  }
  const targetTableIsEditable = editableDimensionTables.value.some(
    table => table.id === addDimForm.value.target_table
  )
  if (!targetTableIsEditable) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  addingDim.value = true
  try {
    const result = await logicalTableAPI.addDimensionRelation(selectedTable.value.id, {
      version: selectedTable.value.version,
      target_table: addDimForm.value.target_table,
      source_field: addDimForm.value.source_field,
      target_field: addDimForm.value.target_field,
      relation_type: addDimForm.value.relation_type,
    })
    selectedTable.value.version = result.version
    ElMessage.success(t('model.star_schema.add_success'))
    addDimDialogVisible.value = false
    const res = await logicalTableAPI.listDimensionRelations(selectedTable.value.id)
    dimensionRelations.value = res || []
  } catch (e) {
    ElMessage.error(getModelErrorMessage(e, t, 'model.star_schema.add_failed'))
  } finally {
    addingDim.value = false
  }
}

const handleRemoveDimRelation = async (rel) => {
  if (!canModifyDimensionRelation(rel)) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await ElMessageBox.confirm(
      t('model.star_schema.remove_confirm', { name: rel.target_table_name }),
      t('model.star_schema.remove_title'),
      { type: 'warning', confirmButtonText: t('model.star_schema.remove_btn'), cancelButtonText: t('model.common.cancel') }
    )
  } catch {
    return
  }
  try {
    const result = await logicalTableAPI.removeDimensionRelation(
      selectedTable.value.id,
      rel.id,
      selectedTable.value.version
    )
    selectedTable.value.version = result.version
    ElMessage.success(t('model.star_schema.remove_success'))
    dimensionRelations.value = dimensionRelations.value.filter(r => r.id !== rel.id)
  } catch (e) {
    ElMessage.error(getModelErrorMessage(e, t, 'model.star_schema.remove_failed'))
  }
}

const renderMermaid = async () => {
  if (!mermaidContainer.value || !mermaidCode.value) return
  try {
    const { svg } = await mermaid.render('mermaid-star-schema-' + Date.now(), mermaidCode.value)
    mermaidContainer.value.innerHTML = svg
  } catch (err) {
    console.error('Mermaid渲染错误:', err)
  }
}

watch(mermaidCode, async () => {
  await nextTick()
  renderMermaid()
})

watch(() => route.query.table_id, syncSelectedTableFromRoute)

const reload = async () => {
  clearSelection()
  loadError.value = ''
  referenceError.value = ''
  if (!authStore.hasPermission('model.logical_model.read')) {
    loadError.value = t('model.common.permission_denied')
    return
  }
  await loadFactTables()
  if (loadError.value) return
  await syncSelectedTableFromRoute()
  if (loadError.value) return
  await loadAllDimensionTables()
  try {
    const res = await standardMetricAPI.listAll()
    allMetrics.value = res
  } catch (err) {
    allMetrics.value = []
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

onBeforeUnmount(() => stopThemeObserver?.())
</script>

<style scoped>
.star-schema-view {
  padding: 20px;
}

.view-header {
  display: flex;
  align-items: center;
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
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.dim-item:last-child {
  border-bottom: none;
}

.dim-item-left {
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
