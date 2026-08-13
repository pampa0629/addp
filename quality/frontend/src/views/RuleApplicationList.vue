<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.ruleApplication.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">{{ t('quality.ruleApplication.addMapping') }}</el-button>
    </div>

    <el-form :inline="true" style="margin-bottom:16px">
      <el-form-item :label="t('quality.ruleApplication.engine')">
        <el-select
          v-model="filter.engine_id"
          :placeholder="t('quality.ruleApplication.allEngines')"
          clearable
          style="width:160px"
          @change="applyFilters"
        >
          <el-option v-for="eng in engines" :key="eng.id" :label="eng.name" :value="eng.id" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('quality.ruleApplication.schema')">
        <el-input v-model="filter.schema_name" :placeholder="t('quality.ruleApplication.all')" style="width:130px" clearable @change="applyFilters" />
      </el-form-item>
      <el-form-item :label="t('quality.ruleApplication.tableName')">
        <el-input v-model="filter.table_name" :placeholder="t('quality.ruleApplication.all')" style="width:130px" clearable @change="applyFilters" />
      </el-form-item>
    </el-form>

    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="load-error" />
    <el-table :data="list" v-loading="loading" :empty-text="emptyText" border>
      <el-table-column prop="id" :label="t('quality.ruleApplication.id')" width="80" />
      <el-table-column :label="t('quality.ruleApplication.element')" min-width="160">
        <template #default="{ row }">
          <div class="element-cell">
            <span>{{ row.element.name }}（{{ row.element.code }}）</span>
            <span class="element-id">{{ t('quality.ruleApplication.elementIdValue', { id: row.element.id }) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.ruleApplication.engine')" width="150">
        <template #default="{ row }">
          <div class="engine-cell">
            <span>{{ engineName(row.engine_id) }}</span>
            <span class="engine-id">{{ t('quality.ruleApplication.engineIdValue', { id: row.engine_id }) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="schema_name" :label="t('quality.ruleApplication.schema')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.ruleApplication.tableName')" width="150" />
      <el-table-column prop="column_name" :label="t('quality.ruleApplication.column')" width="150" />
      <el-table-column prop="enabled" :label="t('quality.ruleApplication.enabled')" width="80">
        <template #default="{ row }">
          <el-switch
            v-model="row.enabled"
            :loading="updatingIds.has(row.id)"
            :disabled="updatingIds.has(row.id) || deletingIds.has(row.id)"
            :aria-label="t('quality.ruleApplication.enabled')"
            @change="updateEnabled(row, $event)"
          />
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.ruleApplication.actions')" width="120">
        <template #default="{ row }">
          <el-button
            size="small"
            type="danger"
            :loading="deletingIds.has(row.id)"
            :disabled="deletingIds.has(row.id)"
            @click="deleteItem(row.id)"
          >{{ t('quality.ruleApplication.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.page_size"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @size-change="changePageSize"
      @current-change="changePage"
    />

    <el-dialog
      v-model="showCreateDialog"
      class="addp-dialog"
      :title="t('quality.ruleApplication.createTitle')"
      width="min(520px, calc(100vw - 24px))"
      :show-close="!submitting"
      :close-on-click-modal="!submitting"
      :close-on-press-escape="!submitting"
    >
      <el-form :model="form" label-position="top">
        <el-form-item :label="t('quality.ruleApplication.element')">
          <el-select
            v-model="form.element_id"
            filterable
            remote
            :remote-method="searchElements"
            :loading="elementSearchLoading"
            :placeholder="t('quality.ruleApplication.elementPlaceholder')"
            style="width:100%"
            reserve-keyword
            clearable
            @change="onElementChange"
          >
            <el-option
              v-for="el in elementOptions"
              :key="el.id"
              :label="`${el.name}（${el.code}）`"
              :value="el.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item v-if="selectedElement" :label="t('quality.ruleApplication.enabledRules')">
          <el-alert
            v-if="enabledRules.length === 0"
            :title="t('quality.ruleApplication.noEnabledRules')"
            type="warning"
            show-icon
            :closable="false"
          />
          <el-table v-else :data="enabledRules" border size="small" class="rule-preview">
            <el-table-column :label="t('quality.ruleApplication.ruleType')" width="110">
              <template #default="{ row }">{{ ruleTypeLabel(row.type) }}</template>
            </el-table-column>
            <el-table-column :label="t('quality.ruleApplication.ruleSeverity')" width="90">
              <template #default="{ row }">
                <el-tag :type="ruleSeverityType(row.severity)" size="small">{{ ruleSeverityLabel(row.severity) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('quality.ruleApplication.ruleParams')" min-width="120" show-overflow-tooltip>
              <template #default="{ row }">{{ ruleParamsSummary(row) }}</template>
            </el-table-column>
            <el-table-column prop="message" :label="t('quality.ruleApplication.ruleMessage')" min-width="120" show-overflow-tooltip />
          </el-table>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.engine')">
          <el-select v-model="form.engine_id" :placeholder="t('quality.ruleApplication.enginePlaceholder')" style="width:100%" @change="onEngineChange">
            <el-option
              v-for="eng in activeEngines"
              :key="eng.id"
              :label="`${eng.name}（${eng.engine_type}）`"
              :value="eng.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.schema')" required>
          <el-select v-model="form.schema_name" :loading="catalogLoading" :disabled="!form.engine_id" :placeholder="t('quality.ruleApplication.schemaPlaceholder')" filterable style="width:100%" @change="onSchemaChange">
            <el-option v-for="schema in schemaOptions" :key="schema.pathKey" :label="schema.name" :value="schema.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.tableName')">
          <el-select v-model="form.table_name" :loading="catalogLoading" :disabled="!form.schema_name" :placeholder="t('quality.ruleApplication.tableNamePlaceholder')" filterable style="width:100%" @change="onTableChange">
            <el-option v-for="table in tableOptions" :key="table.pathKey" :label="table.name" :value="table.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.column')">
          <el-select v-model="form.column_name" :loading="catalogLoading" :disabled="!form.table_name" :placeholder="t('quality.ruleApplication.columnPlaceholder')" filterable style="width:100%">
            <el-option v-for="column in columnOptions" :key="column.name" :label="column.name" :value="column.name" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="showCreateDialog = false">{{ t('quality.ruleApplication.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" :disabled="submitting" @click="createItem">{{ t('quality.ruleApplication.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ruleApplicationAPI, systemCatalogAPI, systemEngineAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { buildRuleApplicationListRouteQuery, resolveRuleApplicationListRouteState } from '../utils/ruleApplicationListRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const list = ref([])
const loading = ref(false)
const loadError = ref('')
const showCreateDialog = ref(false)
const submitting = ref(false)
const updatingIds = ref(new Set())
const deletingIds = ref(new Set())
const filter = ref({ engine_id: null, schema_name: '', table_name: '' })
const form = ref({ element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' })
const pagination = ref({ page: 1, page_size: 20, total: 0 })

const engines = ref([])
const activeEngines = computed(() => engines.value.filter(engine => engine.lifecycle_state === 'active'))
const engineByID = computed(() => new Map(engines.value.map(engine => [engine.id, engine])))
const elementOptions = ref([])
const selectedElement = ref(null)
const elementSearchLoading = ref(false)
const schemaOptions = ref([])
const tableOptions = ref([])
const columnOptions = ref([])
const catalogLoading = ref(false)
let catalogRequestSequence = 0
let listRequestSequence = 0
let elementSearchSequence = 0
let routeReady = false
const hasFilters = computed(() => Boolean(filter.value.engine_id || filter.value.schema_name || filter.value.table_name))
const emptyText = computed(() => t(hasFilters.value
  ? 'quality.ruleApplication.filteredEmpty'
  : 'quality.ruleApplication.empty'))
const enabledRules = computed(() => {
  const document = selectedElement.value?.quality_rules
  if (document?.schema_version !== 'addp.quality.rules/v1' || !Array.isArray(document.rules)) return []
  return document.rules.filter(rule => rule?.enabled === true)
})

const fetchEngines = async () => {
  try {
    const res = await systemEngineAPI.list({
      engine_type: 'postgresql',
      lifecycle_states: 'active,disabled'
    })
    engines.value = (res || []).filter(engine => engine.engine_type === 'postgresql')
  } catch {
    engines.value = []
  }
}

const searchElements = async (keyword) => {
  const searchSequence = ++elementSearchSequence
  if (!keyword) {
    elementOptions.value = []
    elementSearchLoading.value = false
    return
  }
  elementSearchLoading.value = true
  try {
    const res = await ruleApplicationAPI.listElementCandidates({ keyword, page: 1, page_size: 20 })
    if (searchSequence !== elementSearchSequence) return
    elementOptions.value = res.data || []
  } catch (error) {
    if (searchSequence !== elementSearchSequence) return
    elementOptions.value = []
    ElMessage.error(error.response?.data?.error || t('quality.ruleApplication.elementSearchFailed'))
  } finally {
    if (searchSequence === elementSearchSequence) elementSearchLoading.value = false
  }
}

const onElementChange = (elementID) => {
  selectedElement.value = elementOptions.value.find(element => element.id === elementID) || null
}

const ruleTypeLabel = (type) => ({
  not_null: t('quality.ruleApplication.ruleNotNull'),
  unique: t('quality.ruleApplication.ruleUnique'),
  format: t('quality.ruleApplication.ruleFormat'),
  length: t('quality.ruleApplication.ruleLength'),
  value_range: t('quality.ruleApplication.ruleValueRange'),
  allowed_values: t('quality.ruleApplication.ruleAllowedValues')
}[type] || type)

const ruleSeverityType = (severity) => ({ error: 'danger', warning: 'warning', info: 'info' }[severity] || 'info')
const ruleSeverityLabel = (severity) => ({
  error: t('quality.ruleApplication.severityError'),
  warning: t('quality.ruleApplication.severityWarning'),
  info: t('quality.ruleApplication.severityInfo')
}[severity] || severity)
const ruleParamsSummary = (rule) => {
  const params = rule?.params || {}
  if (rule?.type === 'format') return params.pattern || '-'
  if (rule?.type === 'length' || rule?.type === 'value_range') {
    return t('quality.ruleApplication.ruleRange', { min: params.min ?? '-', max: params.max ?? '-' })
  }
  if (rule?.type === 'allowed_values') return Array.isArray(params.values) ? params.values.join(', ') : '-'
  return '-'
}

const engineName = (id) => engineByID.value.get(id)?.name || t('quality.ruleApplication.engineUnknown')
const isActiveEngine = (id) => engineByID.value.get(id)?.lifecycle_state === 'active'

const fetchList = async () => {
  const requestSequence = ++listRequestSequence
  loading.value = true
  loadError.value = ''
  try {
    const params = { page: pagination.value.page, page_size: pagination.value.page_size }
    if (filter.value.engine_id) params.engine_id = filter.value.engine_id
    if (filter.value.schema_name) params.schema_name = filter.value.schema_name
    if (filter.value.table_name) params.table_name = filter.value.table_name
    const res = await ruleApplicationAPI.list(params)
    if (requestSequence !== listRequestSequence) return
    list.value = res?.data || []
    pagination.value.total = res?.total || 0
    const lastPage = Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
    if (pagination.value.page > lastPage) {
      pagination.value.page = lastPage
      await syncRoute()
      return
    }
  } catch (e) {
    if (requestSequence !== listRequestSequence) return
    list.value = []
    pagination.value.total = 0
    loadError.value = e.response?.data?.error || t('quality.ruleApplication.loadFailed')
    ElMessage.error(loadError.value)
  } finally {
    if (requestSequence === listRequestSequence) loading.value = false
  }
}

const buildRouteQuery = () => buildRuleApplicationListRouteQuery({
  engineID: filter.value.engine_id,
  schemaName: filter.value.schema_name.trim(),
  tableName: filter.value.table_name.trim(),
  page: pagination.value.page,
  pageSize: pagination.value.page_size
})

const syncRoute = () => navigateQualityRoute(router, {
  path: '/rule-applications',
  query: buildRouteQuery()
}, { history: 'replace' })

const applyFilters = async () => {
  pagination.value.page = 1
  await syncRoute()
}

const changePage = async (page) => {
  pagination.value.page = page
  await syncRoute()
}

const changePageSize = async (pageSize) => {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  await syncRoute()
}

const applyRouteState = (query) => {
  const state = resolveRuleApplicationListRouteState(query)
  filter.value.engine_id = state.engineID
  filter.value.schema_name = state.schemaName
  filter.value.table_name = state.tableName
  pagination.value.page = state.page
  pagination.value.page_size = state.pageSize
  return state
}

const restoreListFromRoute = async (query) => {
  if (!routeReady) return
  const state = applyRouteState(query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/rule-applications', query: state.query }, { history: 'replace' })
    return
  }
  await fetchList()
}

const openCreateDialog = () => {
  form.value = { element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' }
  elementOptions.value = []
  selectedElement.value = null
  schemaOptions.value = []
  tableOptions.value = []
  columnOptions.value = []
  showCreateDialog.value = true
}

const catalogNodes = (response) => response?.nodes || response?.data?.nodes || []

const loadSchemas = async () => {
  const engineID = form.value.engine_id
  const requestSequence = ++catalogRequestSequence
  schemaOptions.value = []
  tableOptions.value = []
  form.value.schema_name = ''
  form.value.table_name = ''
  form.value.column_name = ''
  if (!engineID) return
  catalogLoading.value = true
  try {
    const response = await systemCatalogAPI.listChildren(engineID, { segments: [] }, { limit: 100 })
    if (requestSequence !== catalogRequestSequence) return
    const root = catalogNodes(response).find(node => node.role === 'branch' && (node.path?.segments || []).length === 1)
    if (!root) return
    const children = await systemCatalogAPI.listChildren(engineID, root.path, { limit: 1000 })
    if (requestSequence !== catalogRequestSequence) return
    schemaOptions.value = catalogNodes(children)
      .filter(node => node.role === 'branch')
      .map(node => ({ ...node, pathKey: JSON.stringify(node.path) }))
  } catch (error) {
    if (requestSequence === catalogRequestSequence) ElMessage.error(error.response?.data?.error || t('quality.ruleApplication.catalogLoadFailed'))
  } finally {
    if (requestSequence === catalogRequestSequence) catalogLoading.value = false
  }
}

const onEngineChange = () => loadSchemas()

const onSchemaChange = async () => {
  const schema = schemaOptions.value.find(item => item.name === form.value.schema_name)
  const engineID = form.value.engine_id
  const requestSequence = ++catalogRequestSequence
  tableOptions.value = []
  form.value.table_name = ''
  form.value.column_name = ''
  if (!schema || !engineID) return
  catalogLoading.value = true
  try {
    const response = await systemCatalogAPI.listChildren(engineID, schema.path, { limit: 1000 })
    if (requestSequence !== catalogRequestSequence) return
    tableOptions.value = catalogNodes(response)
      .filter(node => node.role === 'leaf')
      .map(node => ({ ...node, pathKey: JSON.stringify(node.path) }))
  } catch (error) {
    if (requestSequence === catalogRequestSequence) ElMessage.error(error.response?.data?.error || t('quality.ruleApplication.catalogLoadFailed'))
  } finally {
    if (requestSequence === catalogRequestSequence) catalogLoading.value = false
  }
}

const onTableChange = async () => {
  const table = tableOptions.value.find(item => item.name === form.value.table_name)
  const engineID = form.value.engine_id
  const requestSequence = ++catalogRequestSequence
  columnOptions.value = []
  form.value.column_name = ''
  if (!table || !engineID) return
  catalogLoading.value = true
  try {
    const response = await systemCatalogAPI.describeFacts(engineID, table.path)
    if (requestSequence !== catalogRequestSequence) return
    const facts = response?.table || response?.data?.table
    columnOptions.value = Array.isArray(facts?.fields) ? facts.fields.filter(field => field?.name) : []
  } catch (error) {
    if (requestSequence === catalogRequestSequence) ElMessage.error(error.response?.data?.error || t('quality.ruleApplication.catalogLoadFailed'))
  } finally {
    if (requestSequence === catalogRequestSequence) catalogLoading.value = false
  }
}

const createItem = async () => {
  if (submitting.value) return
  if (!form.value.element_id) return ElMessage.warning(t('quality.ruleApplication.elementRequired'))
  if (!selectedElement.value || enabledRules.value.length === 0) return ElMessage.warning(t('quality.ruleApplication.noEnabledRules'))
  if (!form.value.engine_id) return ElMessage.warning(t('quality.ruleApplication.engineRequired'))
  if (!isActiveEngine(form.value.engine_id)) return ElMessage.warning(t('quality.ruleApplication.engineUnavailable'))
  if (!form.value.schema_name.trim()) return ElMessage.warning(t('quality.ruleApplication.schemaRequired'))
  if (!form.value.table_name) return ElMessage.warning(t('quality.ruleApplication.tableRequired'))
  if (!form.value.column_name) return ElMessage.warning(t('quality.ruleApplication.columnRequired'))
  submitting.value = true
  try {
    await ruleApplicationAPI.create(form.value)
    ElMessage.success(t('quality.ruleApplication.createSuccess'))
    showCreateDialog.value = false
    await fetchList()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.createFailed'))
  } finally {
    submitting.value = false
  }
}

const deleteItem = async (id) => {
  if (deletingIds.value.has(id)) return
  deletingIds.value.add(id)
  try {
    await ElMessageBox.confirm(t('quality.ruleApplication.deleteConfirm'), t('quality.ruleApplication.deleteTitle'), {
      type: 'warning',
      customClass: 'addp-message-box',
      confirmButtonText: t('quality.ruleApplication.confirm'),
      cancelButtonText: t('quality.ruleApplication.cancel'),
      confirmButtonClass: 'el-button--danger'
    })
    await ruleApplicationAPI.delete(id)
    ElMessage.success(t('quality.ruleApplication.deleteSuccess'))
    await fetchList()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.deleteFailed'))
  } finally {
    deletingIds.value.delete(id)
  }
}

const updateEnabled = async (row, enabled) => {
  if (updatingIds.value.has(row.id)) return
  updatingIds.value.add(row.id)
  try {
    const updated = await ruleApplicationAPI.update(row.id, { enabled })
    row.enabled = updated.enabled
    ElMessage.success(t('quality.ruleApplication.updateSuccess'))
  } catch (e) {
    row.enabled = !enabled
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.updateFailed'))
  } finally {
    updatingIds.value.delete(row.id)
  }
}

watch(() => route.query, restoreListFromRoute, { deep: true })

onMounted(async () => {
  const state = applyRouteState(route.query)
  if (state.changed) {
    await navigateQualityRoute(router, { path: '/rule-applications', query: state.query }, { history: 'replace' })
  }
  await Promise.all([fetchEngines(), fetchList()])
  routeReady = true
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.load-error {
  margin-bottom: 16px;
}
.rule-preview {
  width: 100%;
}
.element-cell,
.engine-cell {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.element-id,
.engine-id {
  color: var(--addp-text-secondary);
  font-size: 12px;
}
</style>
