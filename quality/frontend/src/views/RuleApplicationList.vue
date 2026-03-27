<template>
  <div>
    <div class="page-header">
      <h2>规则应用配置</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新增映射</el-button>
    </div>

    <el-form :inline="true" style="margin-bottom:16px">
      <el-form-item label="引擎">
        <el-select
          v-model="filter.engine_id"
          placeholder="全部引擎"
          clearable
          style="width:160px"
          @change="fetchList"
        >
          <el-option v-for="eng in engines" :key="eng.id" :label="eng.name" :value="eng.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="Schema">
        <el-input v-model="filter.schema_name" placeholder="全部" style="width:130px" clearable @change="fetchList" />
      </el-form-item>
      <el-form-item label="表名">
        <el-input v-model="filter.table_name" placeholder="全部" style="width:130px" clearable @change="fetchList" />
      </el-form-item>
    </el-form>

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column label="数据元" min-width="160">
        <template #default="{ row }">
          <span>{{ elementName(row.element_id) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="引擎" width="150">
        <template #default="{ row }">
          <span>{{ engineName(row.engine_id) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="schema_name" label="Schema" width="120" />
      <el-table-column prop="table_name" label="表名" width="150" />
      <el-table-column prop="column_name" label="字段" width="150" />
      <el-table-column prop="enabled" label="启用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <el-button size="small" type="danger" @click="deleteItem(row.id)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="showCreateDialog" title="新增规则应用" width="520px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="数据元">
          <el-select
            v-model="form.element_id"
            filterable
            remote
            :remote-method="searchElements"
            :loading="elementSearchLoading"
            placeholder="输入关键字搜索数据元"
            style="width:100%"
            reserve-keyword
          >
            <el-option
              v-for="el in elementOptions"
              :key="el.id"
              :label="`${el.name}（${el.code}）`"
              :value="el.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="引擎">
          <el-select v-model="form.engine_id" placeholder="选择引擎" style="width:100%">
            <el-option
              v-for="eng in engines"
              :key="eng.id"
              :label="`${eng.name}（${eng.engine_type}）`"
              :value="eng.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="Schema">
          <el-input v-model="form.schema_name" placeholder="可选，留空表示默认 Schema" />
        </el-form-item>
        <el-form-item label="表名">
          <el-input v-model="form.table_name" placeholder="数据表名称" />
        </el-form-item>
        <el-form-item label="字段名">
          <el-input v-model="form.column_name" placeholder="字段名称" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="createItem">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ruleApplicationAPI, standardElementAPI, systemEngineAPI } from '../api/quality'

const list = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const filter = ref({ engine_id: null, schema_name: '', table_name: '' })
const form = ref({ element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' })

// 引擎数据
const engines = ref([])
// 数据元搜索
const elementOptions = ref([])
const elementSearchLoading = ref(false)
// 列表中数据元的缓存（id -> 名称）
const elementCache = ref({})

const fetchEngines = async () => {
  try {
    const res = await systemEngineAPI.list()
    engines.value = res.data || res || []
  } catch {
    engines.value = []
  }
}

const searchElements = async (keyword) => {
  if (!keyword) return
  elementSearchLoading.value = true
  try {
    const res = await standardElementAPI.list({ keyword, page: 1, page_size: 20 })
    elementOptions.value = res.data || []
    // 缓存到 elementCache 以便表格显示
    elementOptions.value.forEach(el => {
      elementCache.value[el.id] = `${el.name}（${el.code}）`
    })
  } finally {
    elementSearchLoading.value = false
  }
}

const engineName = (id) => {
  const eng = engines.value.find(e => e.id === id)
  return eng ? eng.name : id
}

const elementName = (id) => {
  return elementCache.value[id] || `ID: ${id}`
}

const fetchList = async () => {
  loading.value = true
  try {
    const params = {}
    if (filter.value.engine_id) params.engine_id = filter.value.engine_id
    if (filter.value.schema_name) params.schema_name = filter.value.schema_name
    if (filter.value.table_name) params.table_name = filter.value.table_name
    const res = await ruleApplicationAPI.list(params)
    list.value = res || []
  } finally {
    loading.value = false
  }
}

const openCreateDialog = () => {
  form.value = { element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' }
  elementOptions.value = []
  showCreateDialog.value = true
}

const createItem = async () => {
  if (!form.value.element_id) return ElMessage.warning('请选择数据元')
  if (!form.value.engine_id) return ElMessage.warning('请选择引擎')
  if (!form.value.table_name) return ElMessage.warning('请填写表名')
  if (!form.value.column_name) return ElMessage.warning('请填写字段名')
  try {
    await ruleApplicationAPI.create(form.value)
    ElMessage.success('创建成功')
    showCreateDialog.value = false
    await fetchList()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  }
}

const deleteItem = async (id) => {
  await ElMessageBox.confirm('确认删除？', '提示', { type: 'warning' })
  await ruleApplicationAPI.delete(id)
  ElMessage.success('已删除')
  await fetchList()
}

onMounted(async () => {
  await fetchEngines()
  await fetchList()
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
