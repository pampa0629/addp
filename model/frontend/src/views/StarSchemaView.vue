<template>
  <div class="star-schema-view">
    <div class="view-header">
      <span class="view-title">星型建模视图</span>
    </div>

    <el-row :gutter="16" style="margin-top:12px">
      <!-- 左侧：事实表选择器 -->
      <el-col :span="6">
        <el-card shadow="never" class="fact-list-card">
          <template #header>
            <span class="card-title">事实表</span>
          </template>
          <div v-loading="loadingTables">
            <div
              v-for="t in factTables"
              :key="t.id"
              class="fact-item"
              :class="{ active: selectedTableId === t.id }"
              @click="selectTable(t)"
            >
              <div class="fact-item-name">{{ t.name }}</div>
              <div class="fact-item-code">{{ t.code }}</div>
              <el-tag v-if="t.layer" type="info" size="small">{{ t.layer.toUpperCase() }}</el-tag>
            </div>
            <el-empty v-if="!loadingTables && factTables.length === 0" description="暂无事实表" :image-size="60" />
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：星型图 -->
      <el-col :span="18">
        <div v-if="!selectedTable" class="empty-placeholder">
          <el-empty description="请在左侧选择一张事实表" />
        </div>

        <template v-else>
          <!-- 事实表卡片 -->
          <el-card shadow="never" class="fact-detail-card">
            <template #header>
              <div class="fact-detail-header">
                <span class="fact-detail-name">{{ selectedTable.name }}</span>
                <el-tag type="danger" size="small">事实表</el-tag>
                <el-tag v-if="selectedTable.layer" type="info" size="small">{{ selectedTable.layer.toUpperCase() }}</el-tag>
                <el-button
                  link
                  type="primary"
                  @click="$router.push(`/modeling/logical-tables/${selectedTable.id}`)"
                >
                  查看详情
                </el-button>
              </div>
            </template>
            <div v-if="selectedTable.grain_description" class="grain-desc">
              <span class="grain-label">粒度：</span>{{ selectedTable.grain_description }}
            </div>
            <div v-if="measureFields.length > 0" class="measure-fields">
              <div class="section-label">度量字段：</div>
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
            <el-col :span="12">
              <el-card shadow="never" v-loading="loadingRelated">
                <template #header>
                  <div class="card-header-with-action">
                    <span class="card-title">关联维度表</span>
                    <el-button type="primary" size="small" @click="openAddDimDialog">
                      添加关联
                    </el-button>
                  </div>
                </template>
                <div v-if="dimensionRelations.length === 0" class="empty-hint">暂无关联维度表</div>
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
                      @click="$router.push(`/modeling/logical-tables/${rel.target_table}`)"
                    >
                      详情
                    </el-button>
                    <el-button
                      link
                      type="danger"
                      size="small"
                      @click="handleRemoveDimRelation(rel)"
                    >
                      移除
                    </el-button>
                  </div>
                </div>
              </el-card>
            </el-col>

            <!-- 关联指标 -->
            <el-col :span="12">
              <el-card shadow="never" v-loading="loadingMetrics">
                <template #header>
                  <span class="card-title">关联指标</span>
                </template>
                <div v-if="factMetrics.length === 0" class="empty-hint">暂无关联指标</div>
                <div v-for="m in factMetrics" :key="m.id" class="metric-item">
                  <span class="metric-name">{{ metricNameMap[m.metric_id] || `指标#${m.metric_id}` }}</span>
                  <el-tag :type="metricTypeTagType(metricTypeMap[m.metric_id])" size="small">
                    {{ metricTypeLabel(metricTypeMap[m.metric_id]) }}
                  </el-tag>
                </div>
              </el-card>
            </el-col>
          </el-row>

          <!-- Mermaid 星型图 -->
          <el-card shadow="never" style="margin-top:12px">
            <template #header>
              <span class="card-title">拓扑图</span>
            </template>
            <div ref="mermaidContainer" class="mermaid-container">
              <pre class="mermaid">{{ mermaidCode }}</pre>
            </div>
          </el-card>
        </template>
      </el-col>
    </el-row>

    <!-- 添加维度关联对话框 -->
    <el-dialog v-model="addDimDialogVisible" title="添加维度表关联" width="480px" :close-on-click-modal="false">
      <el-form :model="addDimForm" label-width="100px" ref="addDimFormRef">
        <el-form-item label="维度表" prop="target_table" :rules="[{ required: true, message: '请选择维度表' }]">
          <el-select
            v-model="addDimForm.target_table"
            placeholder="选择维度表"
            style="width:100%"
            filterable
            @change="onDimTableChange"
          >
            <el-option
              v-for="dim in allDimensionTables"
              :key="dim.id"
              :label="dim.name"
              :value="dim.id"
            >
              <span>{{ dim.name }}</span>
              <span style="color:var(--el-text-color-secondary);font-size:12px;margin-left:8px">{{ dim.code }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="事实表字段" prop="source_field" :rules="[{ required: true, message: '请选择事实表字段' }]">
          <el-select v-model="addDimForm.source_field" placeholder="选择事实表外键字段" style="width:100%" filterable>
            <el-option
              v-for="f in tableFields"
              :key="f.id"
              :label="f.name"
              :value="f.id"
            >
              <span>{{ f.name }}</span>
              <span style="color:var(--el-text-color-secondary);font-size:12px;margin-left:8px">{{ f.column_name }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="维度表字段" prop="target_field" :rules="[{ required: true, message: '请选择维度表字段' }]">
          <el-select
            v-model="addDimForm.target_field"
            placeholder="选择维度表主键字段"
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
              <span style="color:var(--el-text-color-secondary);font-size:12px;margin-left:8px">{{ f.column_name }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="关联类型">
          <el-radio-group v-model="addDimForm.relation_type">
            <el-radio value="fk">外键（FK）</el-radio>
            <el-radio value="join">关联（JOIN）</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDimDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="addingDim" @click="handleAddDimRelation">确认关联</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import mermaid from 'mermaid'
import { logicalTableAPI, standardMetricAPI } from '../api/model'

const router = useRouter()

const loadingTables = ref(false)
const loadingRelated = ref(false)
const loadingMetrics = ref(false)

const factTables = ref([])
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
const addDimForm = ref({
  target_table: null,
  source_field: null,
  target_field: null,
  relation_type: 'fk'
})

const metricNameMap = computed(() => {
  const map = {}
  allMetrics.value.forEach(m => { map[m.id] = m.name })
  return map
})

const metricTypeMap = computed(() => {
  const map = {}
  allMetrics.value.forEach(m => { map[m.id] = m.metric_type })
  return map
})

const measureFields = computed(() =>
  tableFields.value.filter(f => f.field_role && f.field_role.startsWith('measure_'))
)

const mermaidCode = computed(() => {
  if (!selectedTable.value) return ''
  const lines = ['flowchart LR']
  const factId = `F${selectedTable.value.id}`
  lines.push(`  ${factId}["${selectedTable.value.name}\\n(事实表)"]`)
  lines.push(`  style ${factId} fill:#fef3c7,stroke:#f59e0b`)

  dimensionRelations.value.forEach(rel => {
    const dimId = `D${rel.target_table}`
    lines.push(`  ${dimId}["${rel.target_table_name}\\n(维度)"]`)
    lines.push(`  style ${dimId} fill:#dbeafe,stroke:#3b82f6`)
    lines.push(`  ${dimId} --> ${factId}`)
  })

  factMetrics.value.forEach(m => {
    const mId = `M${m.id}`
    const mName = metricNameMap.value[m.metric_id] || `指标#${m.metric_id}`
    lines.push(`  ${mId}["${mName}\\n(指标)"]`)
    lines.push(`  style ${mId} fill:#dcfce7,stroke:#22c55e`)
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
  if (role === 'measure_additive') return ' [可加]'
  if (role === 'measure_semi') return ' [半可加]'
  if (role === 'measure_non') return ' [不可加]'
  return ''
}

const metricTypeTagType = (t) => {
  if (t === 'atomic') return 'success'
  if (t === 'derived') return 'warning'
  if (t === 'composite') return 'danger'
  return 'info'
}

const metricTypeLabel = (t) => {
  if (t === 'atomic') return '原子'
  if (t === 'derived') return '派生'
  if (t === 'composite') return '复合'
  return t || '未知'
}

const loadFactTables = async () => {
  loadingTables.value = true
  try {
    const res = await logicalTableAPI.list({ table_type: 'fact', page_size: 200 })
    factTables.value = res.data.data || res.data || []
  } finally {
    loadingTables.value = false
  }
}

const loadAllDimensionTables = async () => {
  try {
    const res = await logicalTableAPI.list({ table_type: 'dimension', page_size: 200 })
    allDimensionTables.value = res.data.data || res.data || []
  } catch {}
}

const selectTable = async (t) => {
  selectedTableId.value = t.id
  selectedTable.value = t
  tableFields.value = []
  dimensionRelations.value = []
  factMetrics.value = []

  loadingRelated.value = true
  loadingMetrics.value = true
  try {
    const [fieldsRes, relationsRes, metricsRes] = await Promise.all([
      logicalTableAPI.getFields(t.id),
      logicalTableAPI.listDimensionRelations(t.id),
      logicalTableAPI.listMetrics(t.id),
    ])
    tableFields.value = fieldsRes.data.data || fieldsRes.data || []
    dimensionRelations.value = relationsRes.data.data || relationsRes.data || []
    factMetrics.value = metricsRes.data.data || metricsRes.data || []
  } finally {
    loadingRelated.value = false
    loadingMetrics.value = false
  }

  await nextTick()
  renderMermaid()
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
    dimTableFields.value = res.data.data || res.data || []
  } catch {}
}

const handleAddDimRelation = async () => {
  if (!addDimFormRef.value) return
  await addDimFormRef.value.validate(async (valid) => {
    if (!valid) return
    addingDim.value = true
    try {
      await logicalTableAPI.addDimensionRelation(selectedTable.value.id, {
        target_table: addDimForm.value.target_table,
        source_field: addDimForm.value.source_field,
        target_field: addDimForm.value.target_field,
        relation_type: addDimForm.value.relation_type,
      })
      ElMessage.success('关联添加成功')
      addDimDialogVisible.value = false
      // 刷新关联列表
      const res = await logicalTableAPI.listDimensionRelations(selectedTable.value.id)
      dimensionRelations.value = res.data.data || res.data || []
    } catch (e) {
      ElMessage.error(e?.response?.data?.error || '添加失败')
    } finally {
      addingDim.value = false
    }
  })
}

const handleRemoveDimRelation = async (rel) => {
  try {
    await ElMessageBox.confirm(
      `确认移除与「${rel.target_table_name}」的关联吗？`,
      '移除关联',
      { type: 'warning', confirmButtonText: '移除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await logicalTableAPI.removeDimensionRelation(selectedTable.value.id, rel.id)
    ElMessage.success('已移除')
    dimensionRelations.value = dimensionRelations.value.filter(r => r.id !== rel.id)
  } catch (e) {
    ElMessage.error(e?.response?.data?.error || '移除失败')
  }
}

const renderMermaid = async () => {
  if (!mermaidContainer.value) return
  const mermaidEl = mermaidContainer.value.querySelector('.mermaid')
  if (!mermaidEl || !mermaidCode.value) return
  mermaidEl.removeAttribute('data-processed')
  mermaidEl.textContent = mermaidCode.value
  try {
    await mermaid.run({ nodes: [mermaidEl] })
  } catch (err) {
    console.error('Mermaid渲染错误:', err)
  }
}

watch(mermaidCode, async () => {
  await nextTick()
  renderMermaid()
})

onMounted(async () => {
  mermaid.initialize({ startOnLoad: false, theme: 'default' })
  loadFactTables()
  loadAllDimensionTables()
  try {
    const res = await standardMetricAPI.list({ page_size: 500 })
    allMetrics.value = res.data.data || res.data || []
  } catch {}
})
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
  height: calc(100vh - 160px);
  overflow-y: auto;
}

.fact-item {
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 6px;
  border: 1px solid var(--el-border-color-lighter);
  transition: all 0.2s;
}

.fact-item:hover {
  border-color: var(--el-color-primary-light-5);
  background: var(--el-color-primary-light-9);
}

.fact-item.active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.fact-item-name {
  font-weight: 500;
  font-size: 14px;
}

.fact-item-code {
  color: var(--el-text-color-secondary);
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
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin-bottom: 8px;
}

.grain-label {
  color: var(--el-text-color-primary);
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
  border-bottom: 1px solid var(--el-border-color-lighter);
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
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.metric-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.metric-item:last-child {
  border-bottom: none;
}

.metric-name {
  font-size: 14px;
}

.mermaid-container {
  overflow-x: auto;
}

.empty-hint {
  color: var(--el-text-color-placeholder);
  font-size: 13px;
  text-align: center;
  padding: 20px 0;
}

.card-title {
  font-weight: 600;
}
</style>
