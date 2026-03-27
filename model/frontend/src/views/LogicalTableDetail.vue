<template>
  <div class="logical-table-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="$router.back()">
          <el-icon><ArrowLeft /></el-icon>
          返回
        </el-button>
        <span class="table-name">{{ table.name || '加载中...' }}</span>
        <el-tag :type="statusTagType(table.status)" size="small">
          {{ statusLabel(table.status) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
        <el-button type="success" @click="handlePreviewDDL">
          <el-icon><View /></el-icon>
          预览 DDL
        </el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">基本信息</span></template>
          <el-form :model="form" label-width="100px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="逻辑表名">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="物理表名">
                  <el-input :value="table.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="业务域">
                  <el-select v-model="form.domain_id" clearable style="width:100%">
                    <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="表类型">
                  <el-select v-model="form.table_type" style="width:100%">
                    <el-option label="实体表 (entity)" value="entity" />
                    <el-option label="事实表 (fact)" value="fact" />
                    <el-option label="维度表 (dimension)" value="dimension" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="数仓分层">
                  <el-select v-model="form.layer" clearable style="width:100%">
                    <el-option label="ODS" value="ods" />
                    <el-option label="DWD" value="dwd" />
                    <el-option label="DWS" value="dws" />
                    <el-option label="ADS" value="ads" />
                  </el-select>
                </el-form-item>
              </el-col>
              <!-- 事实表专属：粒度声明 -->
              <el-col v-if="form.table_type === 'fact'" :span="24">
                <el-form-item label="粒度声明">
                  <el-input
                    v-model="form.grain_description"
                    type="textarea"
                    :rows="2"
                    placeholder="描述每行记录代表什么，如：每行代表一笔支付事务"
                  />
                </el-form-item>
              </el-col>
              <!-- 维度表专属：SCD 类型 -->
              <el-col v-if="form.table_type === 'dimension'" :span="8">
                <el-form-item label="SCD 类型">
                  <el-select v-model="form.scd_type" style="width:100%">
                    <el-option label="0 - 静态（不变化）" :value="0" />
                    <el-option label="1 - 覆盖（无历史）" :value="1" />
                    <el-option label="2 - 拉链（保留历史）" :value="2" />
                    <el-option label="3 - 混合（部分历史）" :value="3" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item label="描述">
                  <el-input v-model="form.description" type="textarea" :rows="2" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>

      <!-- 字段定义 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">字段定义</span>
              <el-button type="primary" size="small" @click="openFieldDialog()">
                <el-icon><Plus /></el-icon>
                添加字段
              </el-button>
            </div>
          </template>

          <el-table :data="fields" v-loading="fieldLoading" stripe>
            <el-table-column label="序号" type="index" width="60" />
            <el-table-column label="字段名" prop="name" min-width="120" />
            <el-table-column label="物理列名" prop="column_name" min-width="140" />
            <el-table-column label="数据类型" prop="data_type" width="110">
              <template #default="{ row }">
                <el-tag type="info" size="small">
                  {{ row.data_type.toUpperCase() }}{{ row.length ? `(${row.length})` : '' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="字段角色" width="130">
              <template #default="{ row }">
                <el-tag :type="fieldRoleTagType(row.field_role)" size="small">
                  {{ fieldRoleLabel(row.field_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="约束" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
                <el-tag v-if="row.is_partition" type="success" size="small">分区</el-tag>
                <el-tag v-if="!row.nullable" type="danger" size="small">NOT NULL</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="描述" prop="description" show-overflow-tooltip />
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openFieldDialog(row)">编辑</el-button>
                <el-popconfirm title="确定删除该字段吗？" @confirm="deleteField(row.id)">
                  <template #reference>
                    <el-button link type="danger">删除</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <!-- 关联指标（仅事实表） -->
      <el-col v-if="form.table_type === 'fact'" :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">关联指标</span>
              <el-button type="primary" size="small" @click="openMetricDialog">
                <el-icon><Plus /></el-icon>
                关联指标
              </el-button>
            </div>
          </template>
          <el-table :data="metrics" v-loading="metricLoading" stripe>
            <el-table-column label="指标 ID" prop="metric_id" width="90" />
            <el-table-column label="指标名称" min-width="160">
              <template #default="{ row }">
                {{ metricNameMap[row.metric_id] || `指标#${row.metric_id}` }}
              </template>
            </el-table-column>
            <el-table-column label="计算字段" width="160">
              <template #default="{ row }">
                <span v-if="row.field_id">{{ fieldNameMap[row.field_id] || `字段#${row.field_id}` }}</span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="备注" prop="note" show-overflow-tooltip />
            <el-table-column label="操作" width="80" fixed="right">
              <template #default="{ row }">
                <el-popconfirm title="确定解除关联？" @confirm="removeMetric(row.id)">
                  <template #reference>
                    <el-button link type="danger">解除</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <!-- 物化配置 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <span class="card-title">物化配置</span>
          </template>
          <el-form :model="materializationForm" label-width="110px">
            <el-row :gutter="16">
              <el-col :span="8">
                <el-form-item label="Schema 名">
                  <el-input v-model="materializationForm.schema_name" placeholder="默认 public" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="物理表名">
                  <el-input v-model="materializationForm.table_name" placeholder="默认使用 code" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="分区字段">
                  <el-select v-model="materializationForm.partition_by" placeholder="可选" clearable style="width:100%">
                    <el-option v-for="f in fields" :key="f.id" :label="f.column_name" :value="f.column_name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="分区类型">
                  <el-select v-model="materializationForm.partition_type" placeholder="RANGE" style="width:100%">
                    <el-option label="RANGE" value="range" />
                    <el-option label="LIST" value="list" />
                    <el-option label="HASH" value="hash" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="16">
                <el-form-item label="额外 SQL 选项">
                  <el-input v-model="materializationForm.extra_options" placeholder="如：WITH (autovacuum_enabled = true)" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>
    </el-row>

    <!-- 字段对话框 -->
    <el-dialog
      v-model="fieldDialogVisible"
      :title="editingField ? '编辑字段' : '添加字段'"
      width="580px"
    >
      <el-form ref="fieldFormRef" :model="fieldForm" :rules="fieldRules" label-width="110px">
        <el-form-item label="字段显示名" prop="name">
          <el-input v-model="fieldForm.name" placeholder="中文名称" />
        </el-form-item>
        <el-form-item label="物理列名" prop="column_name">
          <el-input v-model="fieldForm.column_name" placeholder="英文列名（snake_case）" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="数据类型" prop="data_type">
              <el-select v-model="fieldForm.data_type" style="width:100%">
                <el-option label="string" value="string" />
                <el-option label="int" value="int" />
                <el-option label="bigint" value="bigint" />
                <el-option label="float" value="float" />
                <el-option label="decimal" value="decimal" />
                <el-option label="date" value="date" />
                <el-option label="datetime" value="datetime" />
                <el-option label="timestamp" value="timestamp" />
                <el-option label="bool" value="bool" />
                <el-option label="json" value="json" />
                <el-option label="text" value="text" />
                <el-option label="geometry" value="geometry" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="长度">
              <el-input-number
                v-model="fieldForm.length"
                :min="0"
                :disabled="!['string'].includes(fieldForm.data_type)"
                style="width:100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <!-- 字段角色 -->
        <el-form-item label="字段角色">
          <el-select v-model="fieldForm.field_role" style="width:100%">
            <el-option label="普通字段" value="regular" />
            <el-option v-if="form.table_type === 'fact'" label="可加度量" value="measure_additive" />
            <el-option v-if="form.table_type === 'fact'" label="半可加度量" value="measure_semi" />
            <el-option v-if="form.table_type === 'fact'" label="不可加度量" value="measure_non" />
            <el-option v-if="form.table_type === 'fact'" label="维度外键" value="dimension_fk" />
            <el-option v-if="form.table_type === 'fact'" label="退化维度" value="degenerate_dim" />
          </el-select>
        </el-form-item>
        <el-form-item label="关联数据元">
          <el-select
            v-model="fieldForm.element_id"
            placeholder="可选，选择后自动关联"
            clearable
            filterable
            style="width:100%"
            @change="handleElementChange"
          >
            <el-option
              v-for="e in elements"
              :key="e.id"
              :label="`${e.name} (${e.code})`"
              :value="e.id"
            />
          </el-select>
        </el-form-item>
        <el-row :gutter="8">
          <el-col :span="8">
            <el-form-item label="主键">
              <el-switch v-model="fieldForm.is_pk" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="允许为空">
              <el-switch v-model="fieldForm.nullable" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="分区字段">
              <el-switch v-model="fieldForm.is_partition" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="默认值">
          <el-input v-model="fieldForm.default_value" placeholder="如：CURRENT_TIMESTAMP / NULL / 0" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="fieldForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="fieldDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleFieldSubmit" :loading="fieldSubmitting">
          {{ editingField ? '保存' : '添加' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关联指标对话框 -->
    <el-dialog v-model="metricDialogVisible" title="关联指标" width="520px">
      <el-form :model="metricForm" label-width="90px">
        <el-form-item label="选择指标" required>
          <el-select
            v-model="metricForm.metric_id"
            filterable
            placeholder="搜索指标名称"
            style="width:100%"
            @focus="loadAvailableMetrics"
          >
            <el-option
              v-for="m in availableMetrics"
              :key="m.id"
              :label="`${m.name} (${m.metric_type})`"
              :value="m.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="计算字段">
          <el-select v-model="metricForm.field_id" clearable placeholder="可选，指标来自哪个字段" style="width:100%">
            <el-option
              v-for="f in measureFields"
              :key="f.id"
              :label="`${f.name} (${f.column_name})`"
              :value="f.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="metricForm.note" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="metricDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleAddMetric" :loading="metricSubmitting">关联</el-button>
      </template>
    </el-dialog>

    <!-- DDL 预览对话框 -->
    <DDLPreviewDialog v-model="ddlDialogVisible" :ddl="ddlContent" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, View } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI, elementAPI, standardMetricAPI } from '../api/model'
import DDLPreviewDialog from '../components/DDLPreviewDialog.vue'

const route = useRoute()
const tableId = parseInt(route.params.id)

const saving = ref(false)
const fieldLoading = ref(false)
const fieldSubmitting = ref(false)
const fieldDialogVisible = ref(false)
const ddlDialogVisible = ref(false)
const editingField = ref(null)
const fieldFormRef = ref(null)

// 指标关联相关
const metricLoading = ref(false)
const metricDialogVisible = ref(false)
const metricSubmitting = ref(false)
const metrics = ref([])
const availableMetrics = ref([])

const table = ref({})
const form = reactive({
  name: '', domain_id: null, table_type: 'entity', layer: '',
  grain_description: '', scd_type: 0, description: ''
})
const materializationForm = reactive({
  schema_name: '', table_name: '', partition_by: '', partition_type: 'range', extra_options: ''
})
const fields = ref([])
const domains = ref([])
const elements = ref([])
const ddlContent = ref('')

const metricForm = reactive({ metric_id: null, field_id: null, note: '' })

// 度量字段（field_role 为 measure_* 的字段）
const measureFields = computed(() =>
  fields.value.filter(f => f.field_role && f.field_role.startsWith('measure_'))
)

// 指标名称映射（id -> name）
const metricNameMap = computed(() => {
  const map = {}
  availableMetrics.value.forEach(m => { map[m.id] = m.name })
  return map
})

// 字段名称映射（id -> name）
const fieldNameMap = computed(() => {
  const map = {}
  fields.value.forEach(f => { map[f.id] = `${f.name}(${f.column_name})` })
  return map
})

const fieldForm = reactive({
  name: '', column_name: '', data_type: 'string', length: null,
  nullable: true, is_pk: false, is_partition: false,
  default_value: '', element_id: null, description: '',
  field_role: 'regular', hierarchy_id: null, hierarchy_level: null
})
const fieldRules = {
  name: [{ required: true, message: '请输入字段名', trigger: 'blur' }],
  column_name: [{ required: true, message: '请输入物理列名', trigger: 'blur' }],
  data_type: [{ required: true, message: '请选择数据类型', trigger: 'change' }]
}

const statusTagType = (s) => ({ draft: 'info', approved: 'success', materialized: 'warning' }[s] ?? 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', materialized: '已物化' }[s] ?? s)

const fieldRoleTagType = (role) => {
  const map = {
    regular: 'info',
    measure_additive: 'success',
    measure_semi: 'warning',
    measure_non: 'danger',
    dimension_fk: 'primary',
    degenerate_dim: ''
  }
  return map[role] ?? 'info'
}

const fieldRoleLabel = (role) => {
  const map = {
    regular: '普通',
    measure_additive: '可加度量',
    measure_semi: '半可加',
    measure_non: '不可加',
    dimension_fk: '维度FK',
    degenerate_dim: '退化维'
  }
  return (map[role] ?? role) || '普通'
}

const loadTable = async () => {
  const res = await logicalTableAPI.get(tableId)
  table.value = res || {}
  Object.assign(form, {
    name: table.value.name,
    domain_id: table.value.domain_id,
    table_type: table.value.table_type,
    layer: table.value.layer || '',
    grain_description: table.value.grain_description || '',
    scd_type: table.value.scd_type ?? 0,
    description: table.value.description || ''
  })

  const mat = table.value.materialization || {}
  Object.assign(materializationForm, {
    schema_name: mat.schema_name || '',
    table_name: mat.table_name || '',
    partition_by: mat.partition_by || '',
    partition_type: mat.partition_type || 'range',
    extra_options: mat.extra_options || ''
  })
}

const loadFields = async () => {
  fieldLoading.value = true
  try {
    const res = await logicalTableAPI.getFields(tableId)
    fields.value = res || []
  } finally {
    fieldLoading.value = false
  }
}

const loadMetrics = async () => {
  if (form.table_type !== 'fact') return
  metricLoading.value = true
  try {
    const res = await logicalTableAPI.listMetrics(tableId)
    metrics.value = res || []
  } finally {
    metricLoading.value = false
  }
}

const loadAvailableMetrics = async () => {
  if (availableMetrics.value.length > 0) return
  try {
    const res = await standardMetricAPI.list({ page_size: 500 })
    availableMetrics.value = res.data.data || res.data || []
  } catch {
    ElMessage.error('加载指标列表失败')
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    const updateData = { ...form, materialization: materializationForm }
    await logicalTableAPI.update(tableId, updateData)
    ElMessage.success('保存成功')
    loadTable()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const handlePreviewDDL = async () => {
  try {
    const res = await logicalTableAPI.previewDDL(tableId)
    ddlContent.value = res.ddl || ''
    ddlDialogVisible.value = true
  } catch (err) {
    ElMessage.error(err.response?.data?.error || 'DDL 预览失败')
  }
}

const openFieldDialog = (field = null) => {
  editingField.value = field
  if (field) {
    Object.assign(fieldForm, {
      name: field.name,
      column_name: field.column_name,
      data_type: field.data_type,
      length: field.length || null,
      nullable: field.nullable,
      is_pk: field.is_pk,
      is_partition: field.is_partition,
      default_value: field.default_value || '',
      element_id: field.element_id || null,
      description: field.description || '',
      field_role: field.field_role || 'regular',
      hierarchy_id: field.hierarchy_id || null,
      hierarchy_level: field.hierarchy_level || null
    })
  } else {
    Object.assign(fieldForm, {
      name: '', column_name: '', data_type: 'string', length: null,
      nullable: true, is_pk: false, is_partition: false,
      default_value: '', element_id: null, description: '',
      field_role: 'regular', hierarchy_id: null, hierarchy_level: null
    })
  }
  fieldDialogVisible.value = true
}

const handleElementChange = (elementId) => {
  if (!elementId) return
  const el = elements.value.find(e => e.id === elementId)
  if (el) {
    fieldForm.name = el.name
    fieldForm.data_type = el.data_type
    if (el.length) fieldForm.length = el.length
  }
}

const handleFieldSubmit = async () => {
  await fieldFormRef.value.validate()
  fieldSubmitting.value = true
  try {
    if (editingField.value) {
      await logicalTableAPI.updateField(tableId, editingField.value.id, fieldForm)
      ElMessage.success('更新成功')
    } else {
      await logicalTableAPI.createField(tableId, fieldForm)
      ElMessage.success('添加成功')
    }
    fieldDialogVisible.value = false
    loadFields()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    fieldSubmitting.value = false
  }
}

const deleteField = async (fieldId) => {
  try {
    await logicalTableAPI.deleteField(tableId, fieldId)
    ElMessage.success('删除成功')
    loadFields()
  } catch {
    ElMessage.error('删除失败')
  }
}

const openMetricDialog = () => {
  Object.assign(metricForm, { metric_id: null, field_id: null, note: '' })
  metricDialogVisible.value = true
  loadAvailableMetrics()
}

const handleAddMetric = async () => {
  if (!metricForm.metric_id) {
    ElMessage.warning('请选择指标')
    return
  }
  metricSubmitting.value = true
  try {
    await logicalTableAPI.addMetric(tableId, metricForm)
    ElMessage.success('关联成功')
    metricDialogVisible.value = false
    loadMetrics()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '关联失败')
  } finally {
    metricSubmitting.value = false
  }
}

const removeMetric = async (mappingId) => {
  try {
    await logicalTableAPI.removeMetric(tableId, mappingId)
    ElMessage.success('已解除关联')
    loadMetrics()
  } catch {
    ElMessage.error('操作失败')
  }
}

onMounted(async () => {
  await loadTable()
  loadFields()
  loadMetrics()
  const [domainsRes, elementsRes] = await Promise.all([
    domainAPI.list(),
    elementAPI.list({ page_size: 500 })
  ])
  domains.value = domainsRes.data?.data || domainsRes.data || []
  elements.value = elementsRes.data?.data || elementsRes.data || []
})
</script>

<style scoped>
.logical-table-detail {
  padding: 20px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.table-name {
  font-size: 18px;
  font-weight: 600;
}

.info-card {
  margin-bottom: 0;
}

.card-title {
  font-weight: 600;
}

.card-header-with-action {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.text-muted {
  color: var(--el-text-color-placeholder);
}
</style>
