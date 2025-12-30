<template>
  <div class="step2-select-target">
    <h3>选择目标数据库</h3>
    <p class="step-description">请选择目标数据库和表（数据将写入此处）</p>

    <el-form :model="formData" label-width="120px">
      <!-- 目标引擎 -->
      <el-form-item label="目标引擎">
        <el-select
          v-model="formData.engineID"
          placeholder="请选择目标引擎"
          filterable
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in targetEngines"
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

      <!-- 表（可选择已有表或创建新表） -->
      <el-form-item label="目标表">
        <el-radio-group v-model="tableMode" @change="handleTableModeChange">
          <el-radio label="existing">已有表</el-radio>
          <el-radio label="new">新建表</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="tableMode === 'existing'" label="选择表">
        <el-select
          v-model="formData.table"
          placeholder="请选择目标表"
          filterable
          :loading="loadingTables"
          :disabled="!formData.schema"
          @change="handleTableChange"
        >
          <el-option
            v-for="table in tables"
            :key="table.name"
            :label="table.name"
            :value="table.name"
          />
        </el-select>
      </el-form-item>

      <el-form-item v-else label="表名">
        <el-input
          v-model="formData.table"
          placeholder="请输入新表名称"
          @input="handleTableNameInput"
        />
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getEnginesByType, getSchemas, getTables, getTableFields } from '@/api/meta'

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

const tableMode = ref('existing')
const targetEngines = ref([])
const schemas = ref([])
const tables = ref([])
const loadingSchemas = ref(false)
const loadingTables = ref(false)

const canProceed = computed(() => {
  return formData.engineID && formData.schema && formData.table
})

watch(canProceed, (newVal) => {
  if (newVal) {
    props.wizardState.updateTarget({
      engineID: formData.engineID,
      schema: formData.schema,
      table: formData.table,
      extra: { mode: tableMode.value }
    })
  }
})

async function loadTargetEngines() {
  try {
    const response = await getEnginesByType(['postgresql', 'mysql', 'doris'])
    targetEngines.value = response.data || []
  } catch (error) {
    ElMessage.error('加载目标引擎失败')
  }
}

async function handleEngineChange() {
  formData.schema = ''
  formData.table = ''
  schemas.value = []
  tables.value = []

  if (!formData.engineID) return

  loadingSchemas.value = true
  try {
    const response = await getSchemas(formData.engineID)
    schemas.value = response.data || []
  } catch (error) {
    ElMessage.error('加载Schema列表失败')
  } finally {
    loadingSchemas.value = false
  }
}

async function handleSchemaChange() {
  formData.table = ''
  tables.value = []

  if (!formData.schema || tableMode.value === 'new') return

  loadingTables.value = true
  try {
    const response = await getTables(formData.engineID, formData.schema)
    tables.value = response.data || []
  } catch (error) {
    ElMessage.error('加载表列表失败')
  } finally {
    loadingTables.value = false
  }
}

async function handleTableChange() {
  if (!formData.table) return

  try {
    const response = await getTableFields(formData.engineID, formData.schema, formData.table)
    props.wizardState.loadTargetFields(response.data || [])
  } catch (error) {
    ElMessage.error('加载表字段失败')
  }
}

function handleTableModeChange() {
  formData.table = ''
  if (tableMode.value === 'existing') {
    handleSchemaChange()
  }
}

function handleTableNameInput() {
  if (tableMode.value === 'new') {
    props.wizardState.updateTarget({
      engineID: formData.engineID,
      schema: formData.schema,
      table: formData.table,
      extra: { mode: 'new' }
    })
  }
}

loadTargetEngines()
</script>

<style scoped>
.step2-select-target {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: #606266;
  margin-bottom: 30px;
}
</style>
