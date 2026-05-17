<template>
  <div class="step1-select-source">
    <h3>{{ t('transfer.taskWizard.selectSourcePage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.selectSourcePageDesc') }}</p>

    <el-form :model="formData" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.sourceEngineLabel')">
        <el-select
          v-model="formData.engineID"
          :placeholder="t('transfer.taskWizard.selectSourceEngine')"
          filterable
          :loading="loadingEngines"
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in engines"
            :key="engine.id"
            :label="`${engine.name} (${engine.engine_type})`"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="needsSchema" :label="t('transfer.taskWizard.schemaLabel')">
        <el-select
          v-model="formData.schema"
          :placeholder="namespacePlaceholder"
          filterable
          :loading="loadingSchemas"
          :disabled="!formData.engineID"
          @change="handleSchemaChange"
        >
          <el-option
            v-for="schema in schemas"
            :key="schema.schema_name"
            :label="schema.schema_name"
            :value="schema.schema_name"
          />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.dataTableLabel')">
        <el-select
          v-model="formData.table"
          :placeholder="t('transfer.taskWizard.dataTablePlaceholder')"
          filterable
          :loading="loadingTables"
          :disabled="!formData.engineID || (needsSchema && !formData.schema)"
          @change="handleTableChange"
        >
          <el-option
            v-for="table in tables"
            :key="table.name"
            :label="table.row_count !== undefined ? `${table.name} (${table.row_count || 0} ${t('transfer.taskWizard.rowsUnit')})` : table.name"
            :value="table.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-if="selectedTable && hasTableMetadata" :label="t('transfer.taskWizard.tableInfoLabel')">
        <div class="table-info">
          <p v-if="selectedTable.row_count !== undefined"><strong>{{ t('transfer.taskWizard.rowCountLabel') }}：</strong>{{ selectedTable.row_count || 0 }}</p>
          <p v-if="selectedTable.size_bytes !== undefined"><strong>{{ t('transfer.taskWizard.sizeLabel') }}：</strong>{{ formatBytes(selectedTable.size_bytes) }}</p>
          <p v-if="selectedTable.comment"><strong>{{ t('transfer.taskWizard.commentLabel') }}：</strong>{{ selectedTable.comment }}</p>
        </div>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { getTables, getTableFields, getSchemas } from '@/api/meta'
import { systemEnginesAPI } from '@/api/systemEngines'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formData = reactive({
  engineID: null,
  schema: '',
  table: ''
})

const engines = ref([])
const schemas = ref([])
const tables = ref([])
const selectedTable = ref(null)

const loadingEngines = ref(false)
const loadingSchemas = ref(false)
const loadingTables = ref(false)

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const needsSchema = computed(() => {
  const engineType = (selectedEngine.value?.engine_type || '').toLowerCase()
  return isNativeTableEngine(engineType)
})

const namespacePlaceholder = computed(() => {
  return t('transfer.taskWizard.schemaPlaceholder')
})

const canProceed = computed(() => {
  return !!(formData.engineID && formData.table && (!needsSchema.value || formData.schema))
})

const hasTableMetadata = computed(() => {
  if (!selectedTable.value) return false
  return selectedTable.value.row_count !== undefined ||
    selectedTable.value.size_bytes !== undefined ||
    !!selectedTable.value.comment
})

watch(canProceed, (ready) => {
  if (ready) {
    syncSource()
  }
})

function isNativeTableEngine(engineType) {
  const type = (engineType || '').toLowerCase()
  return [
    'postgres',
    'mysql',
    'doris',
    'clickhouse',
    'sqlite',
    'spatialite'
  ].some(token => type.includes(token))
}

function syncSource() {
  const engine = selectedEngine.value
  if (!engine) return

  props.wizardState.updateSource({
    engineID: formData.engineID,
    engineType: engine.engine_type,
    scope: 'system',
    schema: formData.schema,
    table: formData.table,
    sourceType: normalizeEngineType(engine.engine_type)
  })
}

async function loadEngines() {
  loadingEngines.value = true
  try {
    const data = await systemEnginesAPI.list()
    engines.value = (data || []).filter(engine =>
      engine?.id !== undefined &&
      engine?.id !== null &&
      isNativeTableEngine(engine.engine_type)
    )
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadEnginesFailedMsg'))
  } finally {
    loadingEngines.value = false
  }
}

async function handleEngineChange() {
  formData.schema = ''
  formData.table = ''
  schemas.value = []
  tables.value = []
  selectedTable.value = null

  if (!formData.engineID) return

  if (needsSchema.value) {
    await loadSchemas()
  } else {
    await loadTables()
  }
}

async function loadSchemas() {
  loadingSchemas.value = true
  try {
    const response = await getSchemas(formData.engineID)
    const schemaList = Array.isArray(response?.data) ? response.data : (response || [])
    schemas.value = schemaList.filter(schema => schema?.schema_name)
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadSchemaFailedMsg'))
  } finally {
    loadingSchemas.value = false
  }
}

async function handleSchemaChange() {
  formData.table = ''
  tables.value = []
  selectedTable.value = null
  if (formData.schema) {
    await loadTables()
  }
}

async function loadTables() {
  if (!formData.engineID) return

  loadingTables.value = true
  try {
    const response = await getTables(formData.engineID, formData.schema || null)
    const tableList = Array.isArray(response?.data) ? response.data : (response || [])
    tables.value = tableList
      .map(item => ({
        name: item.name || item,
        row_count: item.row_count || 0,
        size_bytes: item.size_bytes || 0,
        comment: item.comment || ''
      }))
      .filter(table => table.name)
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTablesFailedMsg'))
  } finally {
    loadingTables.value = false
  }
}

async function handleTableChange() {
  selectedTable.value = tables.value.find(table => table.name === formData.table) || null
  if (!formData.table) return

  try {
    const response = await getTableFields(formData.engineID, formData.schema, formData.table)
    const fieldList = Array.isArray(response?.data) ? response.data : (response || [])
    props.wizardState.loadSourceFields(fieldList)
    syncSource()
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTableFieldsFailed'))
  }
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i]
}

async function restoreState() {
  const state = props.wizardState

  if (!state.sourceEngineID.value) return

  formData.engineID = state.sourceEngineID.value
  await nextTick()

  if (state.sourceSchema.value) {
    await loadSchemas()
    formData.schema = state.sourceSchema.value
    await loadTables()
  } else {
    await loadTables()
  }

  if (state.sourceTable.value) {
    formData.table = state.sourceTable.value
    selectedTable.value = tables.value.find(table => table.name === state.sourceTable.value) || null
  }
}

function normalizeEngineType(engineType) {
  const type = String(engineType || '').toLowerCase()
  if (type.includes('postgres')) return 'postgresql'
  return type
}

onMounted(async () => {
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step1-select-source {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.table-info {
  padding: 12px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.table-info p {
  margin: 8px 0;
  color: var(--addp-text-secondary);
}
</style>
