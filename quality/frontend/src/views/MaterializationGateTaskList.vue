<template>
  <div class="materialization-gate-list">
    <div class="page-header">
      <div>
        <h2>{{ t('quality.materializationGate.title') }}</h2>
        <p>{{ t('quality.materializationGate.subtitle') }}</p>
      </div>
      <el-button v-if="can('quality.materialization_gate.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('quality.materializationGate.create') }}
      </el-button>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="load-error">
      <el-button link type="danger" @click="loadTasks">{{ t('quality.materializationGate.retry') }}</el-button>
    </el-alert>

    <el-table v-else :data="tasks" v-loading="loading" border :empty-text="t('quality.materializationGate.empty')">
      <el-table-column prop="name" :label="t('quality.materializationGate.name')" min-width="180" />
      <el-table-column prop="code" :label="t('quality.materializationGate.code')" min-width="180" />
      <el-table-column :label="t('quality.materializationGate.group')" min-width="200">
        <template #default="{ row }">{{ groupLabel(row.materialization_group_id) }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.materializationGate.frozenVersion')" width="110">
        <template #default="{ row }">v{{ row.materialization_group_version }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.materializationGate.assertionCount')" width="110">
        <template #default="{ row }">{{ assertionCount(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.materializationGate.lastExecution')" min-width="210">
        <template #default="{ row }">
          <template v-if="row.last_execution_id">
            <el-tag :type="statusType(row.last_execution_status)" size="small">{{ statusLabel(row.last_execution_status) }}</el-tag>
            <el-button link type="primary" @click="openExecution(row.last_execution_id)">{{ t('quality.materializationGate.executionDetail') }}</el-button>
          </template>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.materializationGate.actions')" width="150" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('quality.materialization_gate.update')" link type="primary" @click="openEdit(row)">{{ t('quality.materializationGate.edit') }}</el-button>
          <el-popconfirm
            v-if="can('quality.materialization_gate.delete')"
            :title="t('quality.materializationGate.deleteConfirm')"
            @confirm="deleteTask(row)"
          >
            <template #reference><el-button link type="danger">{{ t('quality.materializationGate.delete') }}</el-button></template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @current-change="changePage"
      @size-change="changePageSize"
    />

    <el-dialog
      v-model="dialogVisible"
      class="addp-dialog gate-dialog"
      :title="editingID ? t('quality.materializationGate.editTitle') : t('quality.materializationGate.createTitle')"
      width="min(1040px, calc(100vw - 32px))"
      :close-on-click-modal="!submitting"
      :close-on-press-escape="!submitting"
      @closed="clearDialogRoute"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="basic-grid">
          <el-form-item :label="t('quality.materializationGate.code')" prop="code">
            <el-input v-model="form.code" maxlength="100" :disabled="Boolean(editingID)" />
          </el-form-item>
          <el-form-item :label="t('quality.materializationGate.name')" prop="name">
            <el-input v-model="form.name" maxlength="200" />
          </el-form-item>
        </div>
        <el-form-item :label="t('quality.materializationGate.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('quality.materializationGate.group')" prop="materialization_group_id">
          <el-select
            v-model="form.materialization_group_id"
            filterable
            style="width:100%"
            :placeholder="t('quality.materializationGate.groupPlaceholder')"
            @change="changeGroup"
          >
            <el-option v-for="group in groups" :key="group.id" :label="`${group.name} (${group.code}) · v${group.version}`" :value="group.id" />
          </el-select>
        </el-form-item>

        <template v-if="bindings.length">
          <div class="section-heading">
            <div>
              <h3>{{ t('quality.materializationGate.bindings') }}</h3>
              <p>{{ t('quality.materializationGate.bindingsHelp') }}</p>
            </div>
          </div>
          <el-table :data="bindings" border size="small" class="binding-table">
            <el-table-column :label="t('quality.materializationGate.logicalTable')" min-width="260">
              <template #default="{ row }">{{ logicalTableLabel(row.logical_table_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('quality.materializationGate.alias')" min-width="220">
              <template #default="{ row }"><el-input v-model="row.alias" /></template>
            </el-table-column>
            <el-table-column :label="t('quality.materializationGate.columns')" min-width="300">
              <template #default="{ row }">{{ fieldsForAlias(row.alias).map(field => field.column_name).join(', ') || '-' }}</template>
            </el-table-column>
          </el-table>

          <div class="section-heading assertions-heading">
            <div>
              <h3>{{ t('quality.materializationGate.assertions') }}</h3>
              <p>{{ t('quality.materializationGate.assertionsHelp') }}</p>
            </div>
            <el-dropdown @command="addAssertion">
              <el-button type="primary" plain :icon="Plus">{{ t('quality.materializationGate.addAssertion') }}</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-for="type in assertionTypes" :key="type" :command="type">{{ assertionTypeLabel(type) }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>

          <el-empty v-if="!assertions.length" :description="t('quality.materializationGate.assertionsEmpty')" />
          <el-card v-for="(assertion, index) in assertions" :key="assertion.assertion_key" shadow="never" class="assertion-card">
            <template #header>
              <div class="assertion-header">
                <strong>{{ index + 1 }}. {{ assertionTypeLabel(assertion.type) }}</strong>
                <div>
                  <el-select v-model="assertion.severity" class="severity-select">
                    <el-option value="error" :label="t('quality.materializationGate.severityError')" />
                    <el-option value="warning" :label="t('quality.materializationGate.severityWarning')" />
                    <el-option value="info" :label="t('quality.materializationGate.severityInfo')" />
                  </el-select>
                  <el-button link type="danger" @click="assertions.splice(index, 1)">{{ t('quality.materializationGate.removeAssertion') }}</el-button>
                </div>
              </div>
            </template>

            <div class="assertion-grid">
              <el-form-item :label="t('quality.materializationGate.tableAlias')">
                <el-select v-model="assertion.params.table" filterable style="width:100%" @change="resetAssertionColumns(assertion)">
                  <el-option v-for="binding in bindings" :key="binding.logical_table_id" :label="binding.alias" :value="binding.alias" />
                </el-select>
              </el-form-item>

              <template v-if="assertion.type === 'not_null'">
                <el-form-item :label="t('quality.materializationGate.column')">
                  <el-select v-model="assertion.params.column" filterable style="width:100%">
                    <el-option v-for="field in fieldsForAlias(assertion.params.table)" :key="field.id" :label="field.column_name" :value="field.column_name" />
                  </el-select>
                </el-form-item>
              </template>

              <template v-else-if="assertion.type === 'allowed_values'">
                <el-form-item :label="t('quality.materializationGate.column')">
                  <el-select v-model="assertion.params.column" filterable class="full-width">
                    <el-option v-for="field in fieldsForAlias(assertion.params.table)" :key="field.id" :label="field.column_name" :value="field.column_name" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('quality.materializationGate.allowedValues')">
                  <el-select
                    v-model="assertion.params.values"
                    multiple
                    filterable
                    allow-create
                    default-first-option
                    class="full-width"
                    :placeholder="t('quality.materializationGate.allowedValuesPlaceholder')"
                  />
                </el-form-item>
              </template>

              <template v-else-if="assertion.type === 'unique_key'">
                <el-form-item :label="t('quality.materializationGate.columns')">
                  <el-select v-model="assertion.params.columns" multiple filterable style="width:100%">
                    <el-option v-for="field in fieldsForAlias(assertion.params.table)" :key="field.id" :label="field.column_name" :value="field.column_name" />
                  </el-select>
                </el-form-item>
              </template>

              <template v-else-if="assertion.type === 'foreign_key'">
                <el-form-item :label="t('quality.materializationGate.childColumns')">
                  <el-select v-model="assertion.params.columns" multiple filterable style="width:100%">
                    <el-option v-for="field in fieldsForAlias(assertion.params.table)" :key="field.id" :label="field.column_name" :value="field.column_name" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('quality.materializationGate.referenceTable')">
                  <el-select v-model="assertion.params.reference_table" filterable style="width:100%" @change="assertion.params.reference_columns = []">
                    <el-option v-for="binding in bindings" :key="binding.logical_table_id" :label="binding.alias" :value="binding.alias" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('quality.materializationGate.referenceColumns')">
                  <el-select v-model="assertion.params.reference_columns" multiple filterable style="width:100%">
                    <el-option v-for="field in fieldsForAlias(assertion.params.reference_table)" :key="field.id" :label="field.column_name" :value="field.column_name" />
                  </el-select>
                </el-form-item>
              </template>

              <template v-else-if="assertion.type === 'predicate_implication'">
                <condition-editor
                  :label="t('quality.materializationGate.whenCondition')"
                  :condition="assertion.params.when"
                  :fields="fieldsForAlias(assertion.params.table)"
                  :operator-label="operatorLabel"
                />
                <condition-editor
                  :label="t('quality.materializationGate.thenCondition')"
                  :condition="assertion.params.then"
                  :fields="fieldsForAlias(assertion.params.table)"
                  :operator-label="operatorLabel"
                />
              </template>

              <template v-else-if="assertion.type === 'row_count'">
                <el-form-item :label="t('quality.materializationGate.rowCountMode')">
                  <el-radio-group v-model="assertion.params.mode">
                    <el-radio value="exact">{{ t('quality.materializationGate.rowCountExact') }}</el-radio>
                    <el-radio value="range">{{ t('quality.materializationGate.rowCountRange') }}</el-radio>
                  </el-radio-group>
                </el-form-item>
                <el-form-item v-if="assertion.params.mode === 'exact'" :label="t('quality.materializationGate.exact')">
                  <el-input-number v-model="assertion.params.exact" :min="0" />
                </el-form-item>
                <template v-else>
                  <el-form-item :label="t('quality.materializationGate.min')"><el-input-number v-model="assertion.params.min" :min="0" /></el-form-item>
                  <el-form-item :label="t('quality.materializationGate.max')"><el-input-number v-model="assertion.params.max" :min="0" /></el-form-item>
                </template>
              </template>
            </div>
          </el-card>
        </template>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="dialogVisible = false">{{ t('quality.materializationGate.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('quality.materializationGate.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { defineComponent, h, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { materializationGateAPI, modelMaterializationAPI } from '../api/quality'
import { useAuthStore } from '../store/auth'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { executionDetailRoute } from '../utils/executionNavigation'
import {
  MATERIALIZATION_GATE_TYPES,
  MATERIALIZATION_GATE_OPERATORS,
  bindingAlias,
  buildMaterializationGateDocument,
  createMaterializationGateAssertion,
  parseMaterializationGateDocument
} from '../utils/materializationGateContract'
import {
  buildMaterializationGateRouteQuery,
  resolveMaterializationGateRouteState
} from '../utils/materializationGateRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const tasks = ref([])
const groups = ref([])
const logicalTables = ref([])
const fieldsByTableID = ref(new Map())
const bindings = ref([])
const assertions = ref([])
const loading = ref(false)
const loadError = ref('')
const dialogVisible = ref(false)
const submitting = ref(false)
const editingID = ref(null)
const formRef = ref(null)
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const form = reactive({ code: '', name: '', description: '', materialization_group_id: null, version: 0 })
const assertionTypes = MATERIALIZATION_GATE_TYPES
let routeReady = false
let listSequence = 0

const rules = {
  code: [
    { required: true, message: t('quality.materializationGate.codeRequired'), trigger: 'blur' },
    { pattern: /^[a-z][a-z0-9_]*$/, message: t('quality.materializationGate.codeFormat'), trigger: 'blur' }
  ],
  name: [{ required: true, message: t('quality.materializationGate.nameRequired'), trigger: 'blur' }],
  materialization_group_id: [{ required: true, message: t('quality.materializationGate.groupRequired'), trigger: 'change' }]
}

const parseJSON = value => typeof value === 'string' ? JSON.parse(value) : value
const groupByID = () => new Map(groups.value.map(group => [group.id, group]))
const tableByID = () => new Map(logicalTables.value.map(table => [table.id, table]))
const groupLabel = id => {
  const group = groupByID().get(id)
  return group ? `${group.name} (${group.code})` : `#${id}`
}
const logicalTableLabel = id => {
  const table = tableByID().get(id)
  return table ? `${table.name} (${table.code})` : `#${id}`
}
const assertionCount = task => parseJSON(task.assertions)?.assertions?.length || 0
const statusType = status => ({ success: 'success', failed: 'danger', timeout: 'danger', running: 'warning', pending: 'info' }[status] || 'info')
const statusLabel = status => ({
  pending: t('quality.execution.pending'), running: t('quality.execution.running'), success: t('quality.execution.success'),
  failed: t('quality.execution.failed'), timeout: t('quality.execution.timeout'), cancelled: t('quality.execution.cancelled')
}[status] || status || '-')
const assertionTypeLabel = type => t(`quality.materializationGate.type_${type}`)
const operatorLabel = operator => t(`quality.materializationGate.operator_${operator}`)

const aliasBinding = alias => bindings.value.find(binding => binding.alias === alias)
const fieldsForAlias = alias => {
  const binding = aliasBinding(alias)
  return binding ? fieldsByTableID.value.get(binding.logical_table_id) || [] : []
}

const ConditionEditor = defineComponent({
  name: 'ConditionEditor',
  props: { label: String, condition: Object, fields: Array, operatorLabel: Function },
  setup(props) {
    const needsValue = () => props.condition.operator === 'eq' || props.condition.operator === 'not_eq'
    return () => h('div', { class: 'condition-editor' }, [
      h('strong', props.label),
      h('div', { class: 'condition-grid' }, [
        h(resolveComponent('el-select'), { modelValue: props.condition.column, 'onUpdate:modelValue': value => { props.condition.column = value }, filterable: true }, () =>
          (props.fields || []).map(field => h(resolveComponent('el-option'), { key: field.id, label: field.column_name, value: field.column_name }))
        ),
        h(resolveComponent('el-select'), { modelValue: props.condition.operator, 'onUpdate:modelValue': value => { props.condition.operator = value } }, () =>
          MATERIALIZATION_GATE_OPERATORS.map(operator => h(resolveComponent('el-option'), { key: operator, label: props.operatorLabel(operator), value: operator }))
        ),
        needsValue() ? h(resolveComponent('el-select'), { modelValue: props.condition.value_type, 'onUpdate:modelValue': value => { props.condition.value_type = value } }, () => [
          h(resolveComponent('el-option'), { label: t('quality.materializationGate.valueString'), value: 'string' }),
          h(resolveComponent('el-option'), { label: t('quality.materializationGate.valueNumber'), value: 'number' }),
          h(resolveComponent('el-option'), { label: t('quality.materializationGate.valueBoolean'), value: 'boolean' })
        ]) : null,
        needsValue() && props.condition.value_type === 'boolean'
          ? h(resolveComponent('el-select'), { modelValue: props.condition.value, 'onUpdate:modelValue': value => { props.condition.value = value } }, () => [
            h(resolveComponent('el-option'), { label: 'true', value: true }), h(resolveComponent('el-option'), { label: 'false', value: false })
          ])
          : needsValue() ? h(resolveComponent('el-input'), { modelValue: props.condition.value, 'onUpdate:modelValue': value => { props.condition.value = value } }) : null
      ])
    ])
  }
})

// Keep runtime resolution local so the render-only condition editor uses the same Element Plus registration as templates.
import { resolveComponent } from 'vue'

const resetForm = () => {
  Object.assign(form, { code: '', name: '', description: '', materialization_group_id: null, version: 0 })
  bindings.value = []
  assertions.value = []
  fieldsByTableID.value = new Map()
}

const syncRoute = (mode = 'list', taskID = null, history = 'replace') => navigateQualityRoute(router, {
  path: route.path,
  query: buildMaterializationGateRouteQuery({ mode, taskID, page: pagination.page, pageSize: pagination.pageSize })
}, { history })

const loadTasks = async () => {
  const sequence = ++listSequence
  loading.value = true
  loadError.value = ''
  try {
    const response = await materializationGateAPI.list({ page: pagination.page, page_size: pagination.pageSize })
    if (sequence !== listSequence) return
    tasks.value = response?.data || []
    pagination.total = response?.total || 0
    const lastPage = Math.max(1, Math.ceil(pagination.total / pagination.pageSize))
    if (pagination.page > lastPage) {
      pagination.page = lastPage
      await syncRoute()
    }
  } catch (error) {
    if (sequence !== listSequence) return
    tasks.value = []
    pagination.total = 0
    loadError.value = error.response?.data?.error || t('quality.materializationGate.loadFailed')
  } finally {
    if (sequence === listSequence) loading.value = false
  }
}

const loadReferences = async () => {
  try {
    const [groupItems, tableItems] = await Promise.all([
      modelMaterializationAPI.listGroups(),
      modelMaterializationAPI.listLogicalTables({ status: 'approved' })
    ])
    groups.value = groupItems
    logicalTables.value = tableItems
  } catch (error) {
    groups.value = []
    logicalTables.value = []
    ElMessage.error(error.response?.data?.error || t('quality.materializationGate.referenceLoadFailed'))
  }
}

const loadGroupDefinition = async (groupID, existingBindings = null) => {
  if (!groupID) {
    bindings.value = []
    fieldsByTableID.value = new Map()
    return
  }
  const group = await modelMaterializationAPI.getGroup(groupID)
  const existingByID = new Map((existingBindings || []).map(binding => [binding.logical_table_id, binding.alias]))
  const members = [...(group.members || [])].sort((left, right) => left.position - right.position)
  bindings.value = members.map(member => ({
    logical_table_id: member.logical_table_id,
    alias: existingByID.get(member.logical_table_id) || bindingAlias(tableByID().get(member.logical_table_id)?.code, member.logical_table_id)
  }))
  const fieldEntries = await Promise.all(members.map(async member => [member.logical_table_id, await modelMaterializationAPI.listLogicalTableFields(member.logical_table_id)]))
  fieldsByTableID.value = new Map(fieldEntries)
}

const changeGroup = async groupID => {
  try {
    await loadGroupDefinition(groupID)
    assertions.value = []
  } catch (error) {
    bindings.value = []
    assertions.value = []
    ElMessage.error(error.response?.data?.error || t('quality.materializationGate.referenceLoadFailed'))
  }
}

const addAssertion = type => {
  const assertion = createMaterializationGateAssertion(type)
  assertion.params.table = bindings.value[0]?.alias || ''
  assertions.value.push(assertion)
}

const resetAssertionColumns = assertion => {
  if (assertion.type === 'not_null') assertion.params.column = ''
  else if (assertion.type === 'allowed_values') {
    assertion.params.column = ''
    assertion.params.values = []
  }
  else if (assertion.type === 'unique_key') assertion.params.columns = []
  else if (assertion.type === 'foreign_key') assertion.params.columns = []
  else if (assertion.type === 'predicate_implication') {
    assertion.params.when.column = ''
    assertion.params.then.column = ''
  }
}

const openCreate = () => syncRoute('create', null, 'push')
const openEdit = task => syncRoute('edit', task.id, 'push')
const openExecution = executionID => {
  const location = executionDetailRoute(executionID)
  if (location) navigateQualityRoute(router, location, { history: 'push' })
}
const clearDialogRoute = () => {
  editingID.value = null
  resetForm()
  if (routeReady && resolveMaterializationGateRouteState(route.query).mode !== 'list') syncRoute()
}

const restoreDialog = async state => {
  if (state.mode === 'list') {
    dialogVisible.value = false
    editingID.value = null
    resetForm()
    return
  }
  const permission = state.mode === 'edit' ? 'quality.materialization_gate.update' : 'quality.materialization_gate.create'
  if (!can(permission)) {
    ElMessage.error(t('quality.materializationGate.permissionDenied'))
    await syncRoute()
    return
  }
  if (state.mode === 'create') {
    editingID.value = null
    resetForm()
    dialogVisible.value = true
    return
  }
  try {
    const task = await materializationGateAPI.get(state.taskID)
    editingID.value = task.id
    Object.assign(form, {
      code: task.code,
      name: task.name,
      description: task.description || '',
      materialization_group_id: task.materialization_group_id,
      version: task.version
    })
    const existingBindings = parseJSON(task.table_bindings) || []
    await loadGroupDefinition(task.materialization_group_id, existingBindings)
    assertions.value = parseMaterializationGateDocument(parseJSON(task.assertions))
    dialogVisible.value = true
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('quality.materializationGate.loadFailed'))
    await syncRoute()
  }
}

const assertValid = () => {
  if (!bindings.value.length || !assertions.value.length) return t('quality.materializationGate.assertionsRequired')
  const aliases = bindings.value.map(binding => binding.alias)
  if (aliases.some(alias => !/^[a-z][a-z0-9_]*$/.test(alias)) || new Set(aliases).size !== aliases.length) return t('quality.materializationGate.aliasInvalid')
  for (const assertion of assertions.value) {
    const params = assertion.params
    if (!aliases.includes(params.table)) return t('quality.materializationGate.assertionInvalid')
    if (assertion.type === 'not_null' && !params.column) return t('quality.materializationGate.assertionInvalid')
    if (assertion.type === 'allowed_values') {
      if (!params.column || !params.values.length || params.values.length > 1000) return t('quality.materializationGate.assertionInvalid')
      if (params.values.some(value => typeof value !== 'string' || value.length === 0) || new Set(params.values).size !== params.values.length) return t('quality.materializationGate.assertionInvalid')
    }
    if (assertion.type === 'unique_key' && !params.columns.length) return t('quality.materializationGate.assertionInvalid')
    if (assertion.type === 'foreign_key' && (!params.columns.length || params.columns.length !== params.reference_columns.length || !aliases.includes(params.reference_table))) return t('quality.materializationGate.assertionInvalid')
    if (assertion.type === 'predicate_implication' && (!params.when.column || !params.then.column)) return t('quality.materializationGate.assertionInvalid')
    if (assertion.type === 'row_count') {
      if (params.mode === 'exact' && params.exact == null) return t('quality.materializationGate.assertionInvalid')
      if (params.mode === 'range' && params.min == null && params.max == null) return t('quality.materializationGate.assertionInvalid')
      if (params.mode === 'range' && params.min != null && params.max != null && params.min > params.max) return t('quality.materializationGate.assertionInvalid')
    }
  }
  return ''
}

const submit = async () => {
  const permission = editingID.value ? 'quality.materialization_gate.update' : 'quality.materialization_gate.create'
  if (!can(permission)) return ElMessage.error(t('quality.materializationGate.permissionDenied'))
  try { await formRef.value.validate() } catch { return }
  const validationError = assertValid()
  if (validationError) return ElMessage.error(validationError)
  submitting.value = true
  try {
    const payload = {
      code: form.code.trim(), name: form.name.trim(), description: form.description.trim(),
      materialization_group_id: form.materialization_group_id,
      table_bindings: bindings.value.map(binding => ({ alias: binding.alias.trim(), logical_table_id: binding.logical_table_id })),
      assertions: buildMaterializationGateDocument(assertions.value),
      version: editingID.value ? form.version : 0
    }
    if (editingID.value) await materializationGateAPI.update(editingID.value, payload)
    else await materializationGateAPI.create(payload)
    ElMessage.success(editingID.value ? t('quality.materializationGate.updateSuccess') : t('quality.materializationGate.createSuccess'))
    dialogVisible.value = false
    await loadTasks()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('quality.materializationGate.saveFailed'))
  } finally {
    submitting.value = false
  }
}

const deleteTask = async task => {
  if (!can('quality.materialization_gate.delete')) return ElMessage.error(t('quality.materializationGate.permissionDenied'))
  try {
    await materializationGateAPI.delete(task.id, task.version)
    ElMessage.success(t('quality.materializationGate.deleteSuccess'))
    await loadTasks()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('quality.materializationGate.deleteFailed'))
  }
}

const changePage = async page => { pagination.page = page; await syncRoute() }
const changePageSize = async size => { pagination.pageSize = size; pagination.page = 1; await syncRoute() }

watch(() => route.query, async query => {
  const state = resolveMaterializationGateRouteState(query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: route.path, query: state.query }, { history: 'replace' })
    return
  }
  const listChanged = pagination.page !== state.page || pagination.pageSize !== state.pageSize
  pagination.page = state.page
  pagination.pageSize = state.pageSize
  if (listChanged || !routeReady) await loadTasks()
  await restoreDialog(state)
  routeReady = true
}, { immediate: true })

onMounted(loadReferences)
</script>

<style scoped>
.materialization-gate-list { padding:20px; }
.page-header { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:16px; }
.page-header h2 { margin:0; font-size:18px; }
.page-header p, .section-heading p { margin:6px 0 0; color:var(--el-text-color-secondary); }
.load-error { margin-bottom:16px; }
.pagination { margin-top:16px; justify-content:flex-end; }
.basic-grid, .assertion-grid { display:grid; grid-template-columns:repeat(2, minmax(0, 1fr)); gap:0 16px; }
.section-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin:18px 0 10px; }
.section-heading h3 { margin:0; font-size:16px; }
.binding-table { margin-bottom:18px; }
.assertions-heading { align-items:center; }
.assertion-card { margin-bottom:12px; }
.assertion-header { display:flex; justify-content:space-between; align-items:center; gap:12px; }
.severity-select { width:120px; margin-right:10px; }
.full-width { width:100%; }
.condition-editor { grid-column:1 / -1; margin-bottom:14px; }
.condition-grid { display:grid; grid-template-columns:repeat(4, minmax(0, 1fr)); gap:10px; margin-top:8px; }
@media (max-width: 760px) {
  .basic-grid, .assertion-grid, .condition-grid { grid-template-columns:1fr; }
}
</style>
