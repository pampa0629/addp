<template>
  <div class="step1-select-source">
    <h3>选择数据源</h3>
    <p class="step-description">请选择数据源引擎、Schema和表</p>

    <el-form :model="formData" label-width="120px">
      <!-- 数据源引擎 -->
      <el-form-item label="数据源引擎">
        <el-select
          v-model="formData.engineID"
          placeholder="请选择数据源引擎"
          filterable
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in sourceEngines"
            :key="engine.id"
            :label="engine.engine_name"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <!-- Schema -->
      <el-form-item label="Schema">
        <el-select
          v-model="formData.schema"
          placeholder="请选择Schema"
          filterable
          :loading="loadingSchemas"
          :disabled="!formData.engineID"
          @change="handleSchemaChange"
        >
          <el-option
            v-for="schema in schemas"
            :key="schema.name"
            :label="schema.name"
            :value="schema.name"
          />
        </el-select>
      </el-form-item>

      <!-- 表 -->
      <el-form-item label="数据表">
        <el-select
          v-model="formData.table"
          placeholder="请选择数据表"
          filterable
          :loading="loadingTables"
          :disabled="!formData.schema"
          @change="handleTableChange"
        >
          <el-option
            v-for="table in tables"
            :key="table.name"
            :label="`${table.name} (${table.row_count || 0} 行)`"
            :value="table.name"
          />
        </el-select>
      </el-form-item>

      <!-- 表信息预览 -->
      <el-form-item v-if="selectedTable" label="表信息">
        <div class="table-info">
          <p><strong>行数：</strong>{{ selectedTable.row_count || 0 }}</p>
          <p><strong>大小：</strong>{{ formatBytes(selectedTable.size_bytes) }}</p>
          <p v-if="selectedTable.comment"><strong>备注：</strong>{{ selectedTable.comment }}</p>
        </div>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getEnginesByType, getSchemas, getTables, getTableFields } from '@/api/meta'

// Props
const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

// Emits
const emit = defineEmits(['next', 'prev'])

// State
const formData = reactive({
  engineID: null,
  schema: '',
  table: ''
})

const sourceEngines = ref([])
const schemas = ref([])
const tables = ref([])
const selectedTable = ref(null)

const loadingSchemas = ref(false)
const loadingTables = ref(false)

// Computed
const canProceed = computed(() => {
  return formData.engineID && formData.schema && formData.table
})

// 监听变化
watch(canProceed, (newVal) => {
  if (newVal) {
    // 更新向导状态
    props.wizardState.updateSource({
      engineID: formData.engineID,
      schema: formData.schema,
      table: formData.table
    })
  }
})

// Methods
async function loadSourceEngines() {
  try {
    const response = await getEnginesByType(['postgresql', 'mysql', 'doris', 'mongodb'])
    sourceEngines.value = response.data || []
  } catch (error) {
    ElMessage.error('加载数据源引擎失败')
    console.error(error)
  }
}

async function handleEngineChange() {
  formData.schema = ''
  formData.table = ''
  schemas.value = []
  tables.value = []
  selectedTable.value = null

  if (!formData.engineID) return

  loadingSchemas.value = true
  try {
    const response = await getSchemas(formData.engineID)
    schemas.value = response.data || []
  } catch (error) {
    ElMessage.error('加载Schema列表失败')
    console.error(error)
  } finally {
    loadingSchemas.value = false
  }
}

async function handleSchemaChange() {
  formData.table = ''
  tables.value = []
  selectedTable.value = null

  if (!formData.schema) return

  loadingTables.value = true
  try {
    const response = await getTables(formData.engineID, formData.schema)
    tables.value = response.data || []
  } catch (error) {
    ElMessage.error('加载表列表失败')
    console.error(error)
  } finally {
    loadingTables.value = false
  }
}

async function handleTableChange() {
  selectedTable.value = tables.value.find(t => t.name === formData.table)

  if (!formData.table) return

  // 加载表字段
  try {
    const response = await getTableFields(formData.engineID, formData.schema, formData.table)
    props.wizardState.loadSourceFields(response.data || [])
  } catch (error) {
    ElMessage.error('加载表字段失败')
    console.error(error)
  }
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i]
}

// Lifecycle
loadSourceEngines()
</script>

<style scoped>
.step1-select-source {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: #606266;
  margin-bottom: 30px;
}

.table-info {
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
}

.table-info p {
  margin: 8px 0;
  color: #606266;
}
</style>
