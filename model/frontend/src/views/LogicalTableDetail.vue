<template>
  <div class="logical-table-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="backToList">
          <el-icon><ArrowLeft /></el-icon>
          {{ t('model.common.back') }}
        </el-button>
        <span class="table-name">{{ table.name || t('model.common.loading') }}</span>
        <el-tag :type="statusTagType(table.status)" size="small">
          {{ statusLabel(table.status) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button v-if="canEdit" type="primary" @click="handleSave" :loading="saving">{{ t('model.common.save') }}</el-button>
        <el-button v-if="table.status === 'draft' && authStore.hasPermission('model.logical_model.update')" type="success" @click="handleApprove">
          {{ t('model.common.approve') }}
        </el-button>
        <el-button v-if="table.status === 'approved' && authStore.hasPermission('model.logical_model.update')" @click="handleReopen">
          {{ t('model.common.reopen') }}
        </el-button>
        <el-button type="success" @click="handlePreviewDDL">
          <el-icon><View /></el-icon>
          {{ t('model.logical_table.preview_ddl') }}
        </el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">{{ t('model.logical_table.basic_info') }}</span></template>
          <el-form :model="form" label-width="100px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item :label="t('model.logical_table.name')">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="t('model.logical_table.code')">
                  <el-input :value="table.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.entity.domain')">
                  <el-select v-model="form.domain_id" clearable style="width:100%">
                    <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.logical_table.table_type')">
                  <el-select v-model="form.table_type" style="width:100%">
                    <el-option :label="t('model.logical_table.type_entity')" value="entity" />
                    <el-option :label="t('model.logical_table.type_fact')" value="fact" />
                    <el-option :label="t('model.logical_table.type_dimension')" value="dimension" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.logical_table.layer')">
                  <el-select v-model="form.layer" style="width:100%">
                    <el-option v-for="layer in layers" :key="layer.layer_code" :label="layer.layer_name" :value="layer.layer_code" />
                  </el-select>
                </el-form-item>
              </el-col>
              <!-- 事实表专属：粒度声明 -->
              <el-col v-if="form.table_type === 'fact'" :span="24">
                <el-form-item :label="t('model.logical_table.grain_description')">
                  <el-input
                    v-model="form.grain_description"
                    type="textarea"
                    :rows="2"
                    :placeholder="t('model.logical_table.grain_placeholder')"
                  />
                </el-form-item>
              </el-col>
              <!-- 维度表专属：SCD 类型 -->
              <el-col v-if="form.table_type === 'dimension'" :span="8">
                <el-form-item :label="t('model.logical_table.scd_type')">
                  <el-select v-model="form.scd_type" style="width:100%">
                    <el-option :label="t('model.logical_table.scd_0')" :value="0" />
                    <el-option :label="t('model.logical_table.scd_1')" :value="1" />
                    <el-option :label="t('model.logical_table.scd_2')" :value="2" />
                    <el-option :label="t('model.logical_table.scd_3')" :value="3" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="t('model.entity.description')">
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
              <span class="card-title">{{ t('model.field.title') }}</span>
              <el-button v-if="canEdit" type="primary" size="small" @click="openFieldDialog()">
                <el-icon><Plus /></el-icon>
                {{ t('model.field.add') }}
              </el-button>
            </div>
          </template>

          <el-table :data="fields" v-loading="fieldLoading" stripe>
            <el-table-column :label="t('model.field.index')" type="index" width="60" />
            <el-table-column :label="t('model.field.name')" prop="name" min-width="120" />
            <el-table-column :label="t('model.field.column_name')" prop="column_name" min-width="140" />
            <el-table-column :label="t('model.field.data_type')" prop="data_type" width="110">
              <template #default="{ row }">
                <el-tag type="info" size="small">
                  {{ row.data_type.toUpperCase() }}{{ row.length ? `(${row.length})` : '' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.field_role')" width="130">
              <template #default="{ row }">
                <el-tag :type="fieldRoleTagType(row.field_role)" size="small">
                  {{ fieldRoleLabel(row.field_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.constraints')" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
                <el-tag v-if="row.is_partition" type="success" size="small">{{ t('model.field.is_partition') }}</el-tag>
                <el-tag v-if="!row.nullable" type="danger" size="small">NOT NULL</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.description')" prop="description" show-overflow-tooltip />
            <el-table-column :label="t('model.field.actions')" width="130" fixed="right">
              <template #default="{ row }">
                <el-button v-if="canEdit" link type="primary" @click="openFieldDialog(row)">{{ t('model.common.edit') }}</el-button>
                <el-popconfirm v-if="canEdit" :title="t('model.field.delete_confirm')" @confirm="deleteField(row.id)">
                  <template #reference>
                    <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
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
              <span class="card-title">{{ t('model.metric.title') }}</span>
              <el-button v-if="canEdit" type="primary" size="small" @click="openMetricDialog">
                <el-icon><Plus /></el-icon>
                {{ t('model.metric.add') }}
              </el-button>
            </div>
          </template>
          <el-table :data="metrics" v-loading="metricLoading" stripe>
            <el-table-column :label="t('model.metric.metric_id')" prop="metric_id" width="90" />
            <el-table-column :label="t('model.metric.metric_name')" min-width="160">
              <template #default="{ row }">
                {{ metricNameMap[row.metric_id] || `指标#${row.metric_id}` }}
              </template>
            </el-table-column>
            <el-table-column :label="t('model.metric.calc_field')" width="160">
              <template #default="{ row }">
                <span v-if="row.field_id">{{ fieldNameMap[row.field_id] || `字段#${row.field_id}` }}</span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.metric.note')" prop="note" show-overflow-tooltip />
            <el-table-column :label="t('model.metric.actions')" width="80" fixed="right">
              <template #default="{ row }">
                <el-popconfirm v-if="canEdit" :title="t('model.metric.remove_confirm')" @confirm="removeMetric(row.id)">
                  <template #reference>
                    <el-button link type="danger">{{ t('model.metric.remove') }}</el-button>
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
            <span class="card-title">{{ t('model.materialization.title') }}</span>
          </template>
          <el-form :model="materializationForm" label-width="110px">
            <el-row :gutter="16">
              <el-col :span="8">
                <el-form-item :label="t('model.materialization.schema_name')">
                  <el-input v-model="materializationForm.schema_name" :placeholder="t('model.materialization.schema_placeholder')" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.materialization.table_name')">
                  <el-input v-model="materializationForm.table_name" :placeholder="t('model.materialization.table_placeholder')" />
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.materialization.partition_by')">
                  <el-select v-model="materializationForm.partition_by" placeholder="可选" clearable style="width:100%">
                    <el-option v-for="f in fields" :key="f.id" :label="f.column_name" :value="f.column_name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item :label="t('model.materialization.partition_type')">
                  <el-select v-model="materializationForm.partition_type" placeholder="RANGE" style="width:100%">
                    <el-option label="RANGE" value="range" />
                    <el-option label="LIST" value="list" />
                    <el-option label="HASH" value="hash" />
                  </el-select>
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
      :title="editingField ? t('model.field.edit') : t('model.field.add')"
      width="580px"
    >
      <el-form ref="fieldFormRef" :model="fieldForm" :rules="fieldRules" label-width="110px">
        <el-form-item :label="t('model.field.display_name')" prop="name">
          <el-input v-model="fieldForm.name" :placeholder="t('model.field.display_name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.field.column_name')" prop="column_name">
          <el-input v-model="fieldForm.column_name" :placeholder="t('model.field.column_name_placeholder')" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item :label="t('model.field.data_type')" prop="data_type">
              <el-select v-model="fieldForm.data_type" style="width:100%">
                <el-option label="string" value="string" />
                <el-option label="int" value="int" />
                <el-option label="bigint" value="bigint" />
                <el-option label="float" value="float" />
                <el-option label="decimal" value="decimal" />
                <el-option label="date" value="date" />
                <el-option label="datetime" value="datetime" />
                <el-option label="bool" value="bool" />
                <el-option label="json" value="json" />
                <el-option label="text" value="text" />
                <el-option label="geometry" value="geometry" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="t('model.field.length')">
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
        <el-form-item :label="t('model.field.field_role')">
          <el-select v-model="fieldForm.field_role" style="width:100%">
            <el-option :label="t('model.field.role_regular')" value="regular" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_additive')" value="measure_additive" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_semi')" value="measure_semi" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_non')" value="measure_non" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_dimension_fk')" value="dimension_fk" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_degenerate_dim')" value="degenerate_dim" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.field.element')">
          <el-select
            v-model="fieldForm.element_id"
            :placeholder="t('model.field.element_placeholder')"
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
            <el-form-item :label="t('model.field.is_pk')">
              <el-switch v-model="fieldForm.is_pk" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('model.field.nullable')">
              <el-switch v-model="fieldForm.nullable" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('model.field.is_partition')">
              <el-switch v-model="fieldForm.is_partition" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="t('model.field.default_value')">
          <el-input v-model="fieldForm.default_value" :placeholder="t('model.field.default_value_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.field.description')">
          <el-input v-model="fieldForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="fieldDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleFieldSubmit" :loading="fieldSubmitting">
          {{ editingField ? t('model.common.save') : t('model.common.add') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关联指标对话框 -->
    <el-dialog v-model="metricDialogVisible" :title="t('model.metric.add')" width="520px">
      <el-form :model="metricForm" label-width="90px">
        <el-form-item :label="t('model.metric.metric_name')" required>
          <el-select
            v-model="metricForm.metric_id"
            filterable
            :placeholder="t('model.metric.select_placeholder')"
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
        <el-form-item :label="t('model.metric.calc_field')">
          <el-select v-model="metricForm.field_id" clearable :placeholder="t('model.metric.field_placeholder')" style="width:100%">
            <el-option
              v-for="f in measureFields"
              :key="f.id"
              :label="`${f.name} (${f.column_name})`"
              :value="f.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.metric.note')">
          <el-input v-model="metricForm.note" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="metricDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleAddMetric" :loading="metricSubmitting">{{ t('model.metric.associate') }}</el-button>
      </template>
    </el-dialog>

    <!-- DDL 预览对话框 -->
    <DDLPreviewDialog v-model="ddlDialogVisible" :ddl="ddlContent" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, View } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI, elementAPI, standardMetricAPI, dwLayerAPI } from '../api/model'
import DDLPreviewDialog from '../components/DDLPreviewDialog.vue'
import { useI18n } from 'vue-i18n'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const tableId = parseInt(route.params.id)

const backToList = () => navigateModelRoute(router, '/logical-tables', { history: 'replace' })

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
const canEdit = computed(() => table.value.status === 'draft' && authStore.hasPermission('model.logical_model.update'))
const form = reactive({
  name: '', domain_id: null, table_type: 'entity', layer: '',
  grain_description: '', scd_type: 0, description: ''
})
const materializationForm = reactive({
  schema_name: '', table_name: '', partition_by: '', partition_type: 'range'
})
const fields = ref([])
const domains = ref([])
const layers = ref([])
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
  name: [{ required: true, message: t('model.field.name_required'), trigger: 'blur' }],
  column_name: [{ required: true, message: t('model.field.column_required'), trigger: 'blur' }],
  data_type: [{ required: true, message: t('model.field.type_required'), trigger: 'change' }]
}

const statusTagType = (s) => ({ draft: 'info', approved: 'success' }[s] ?? 'info')
const statusLabel = (s) => ({
  draft: t('model.common.status_draft'),
  approved: t('model.common.status_approved'),
}[s] ?? s)

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
    regular: t('model.field.role_label_regular'),
    measure_additive: t('model.field.role_label_additive'),
    measure_semi: t('model.field.role_label_semi'),
    measure_non: t('model.field.role_label_non'),
    dimension_fk: t('model.field.role_label_fk'),
    degenerate_dim: t('model.field.role_label_degenerate')
  }
  return (map[role] ?? role) || t('model.field.role_label_regular')
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
    const res = await standardMetricAPI.listAll()
    availableMetrics.value = res
  } catch {
    ElMessage.error(t('model.metric.load_failed'))
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    const updateData = { ...form, materialization: materializationForm }
    await logicalTableAPI.update(tableId, updateData)
    ElMessage.success(t('model.common.save_success'))
    loadTable()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || t('model.common.save_failed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try { await logicalTableAPI.approve(tableId); ElMessage.success(t('model.common.approve_success')); loadTable() }
  catch (err) { ElMessage.error(err.response?.data?.error || t('model.common.op_failed')) }
}

const handleReopen = async () => {
  try { await logicalTableAPI.reopen(tableId); ElMessage.success(t('model.common.reopen_success')); loadTable() }
  catch (err) { ElMessage.error(err.response?.data?.error || t('model.common.op_failed')) }
}

const handlePreviewDDL = async () => {
  try {
    const res = await logicalTableAPI.previewDDL(tableId)
    ddlContent.value = res.ddl || ''
    ddlDialogVisible.value = true
  } catch (err) {
    ElMessage.error(err.response?.data?.error || t('model.logical_table.ddl_failed'))
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
      ElMessage.success(t('model.common.update_success'))
    } else {
      await logicalTableAPI.createField(tableId, fieldForm)
      ElMessage.success(t('model.common.add_success'))
    }
    fieldDialogVisible.value = false
    loadFields()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || t('model.common.op_failed'))
  } finally {
    fieldSubmitting.value = false
  }
}

const deleteField = async (fieldId) => {
  try {
    await logicalTableAPI.deleteField(tableId, fieldId)
    ElMessage.success(t('model.common.delete_success'))
    loadFields()
  } catch {
    ElMessage.error(t('model.common.delete_failed'))
  }
}

const openMetricDialog = () => {
  Object.assign(metricForm, { metric_id: null, field_id: null, note: '' })
  metricDialogVisible.value = true
  loadAvailableMetrics()
}

const handleAddMetric = async () => {
  if (!metricForm.metric_id) {
    ElMessage.warning(t('model.metric.select_required'))
    return
  }
  metricSubmitting.value = true
  try {
    await logicalTableAPI.addMetric(tableId, metricForm)
    ElMessage.success(t('model.metric.associate_success'))
    metricDialogVisible.value = false
    loadMetrics()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || t('model.metric.associate_failed'))
  } finally {
    metricSubmitting.value = false
  }
}

const removeMetric = async (mappingId) => {
  try {
    await logicalTableAPI.removeMetric(tableId, mappingId)
    ElMessage.success(t('model.metric.remove_success'))
    loadMetrics()
  } catch {
    ElMessage.error(t('model.common.op_failed'))
  }
}

onMounted(async () => {
  await loadTable()
  loadFields()
  loadMetrics()
  const [domainsRes, elementsRes, layersRes] = await Promise.all([
    domainAPI.list(),
    elementAPI.listAll(),
    dwLayerAPI.list()
  ])
  domains.value = domainsRes || []
  elements.value = elementsRes
  layers.value = layersRes || []
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
  color: var(--addp-text-tertiary);
}
</style>
