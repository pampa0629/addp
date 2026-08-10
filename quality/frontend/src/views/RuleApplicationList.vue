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

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="id" :label="t('quality.ruleApplication.id')" width="80" />
      <el-table-column :label="t('quality.ruleApplication.element')" min-width="160">
        <template #default="{ row }">
          <span>{{ elementName(row.element_id) }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.ruleApplication.engine')" width="150">
        <template #default="{ row }">
          <span>{{ engineName(row.engine_id) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="schema_name" :label="t('quality.ruleApplication.schema')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.ruleApplication.tableName')" width="150" />
      <el-table-column prop="column_name" :label="t('quality.ruleApplication.column')" width="150" />
      <el-table-column prop="enabled" :label="t('quality.ruleApplication.enabled')" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? t('quality.ruleApplication.yes') : t('quality.ruleApplication.no') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.ruleApplication.actions')" width="120">
        <template #default="{ row }">
          <el-button size="small" type="danger" @click="deleteItem(row.id)">{{ t('quality.ruleApplication.delete') }}</el-button>
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
      @size-change="fetchList"
      @current-change="fetchList"
    />

    <el-dialog v-model="showCreateDialog" :title="t('quality.ruleApplication.createTitle')" width="520px">
      <el-form :model="form" label-width="80px">
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
          >
            <el-option
              v-for="el in elementOptions"
              :key="el.id"
              :label="`${el.name}（${el.code}）`"
              :value="el.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.engine')">
          <el-select v-model="form.engine_id" :placeholder="t('quality.ruleApplication.enginePlaceholder')" style="width:100%">
            <el-option
              v-for="eng in engines"
              :key="eng.id"
              :label="`${eng.name}（${eng.engine_type}）`"
              :value="eng.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.schema')" required>
          <el-input v-model="form.schema_name" :placeholder="t('quality.ruleApplication.schemaPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.tableName')">
          <el-input v-model="form.table_name" :placeholder="t('quality.ruleApplication.tableNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('quality.ruleApplication.column')">
          <el-input v-model="form.column_name" :placeholder="t('quality.ruleApplication.columnPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ t('quality.ruleApplication.cancel') }}</el-button>
        <el-button type="primary" @click="createItem">{{ t('quality.ruleApplication.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ruleApplicationAPI, standardElementAPI, systemEngineAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const list = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const filter = ref({ engine_id: null, schema_name: '', table_name: '' })
const form = ref({ element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' })
const pagination = ref({ page: 1, page_size: 20, total: 0 })

const engines = ref([])
const elementOptions = ref([])
const elementSearchLoading = ref(false)
const elementCache = ref({})

const fetchEngines = async () => {
  try {
    const res = await systemEngineAPI.list()
    engines.value = (res || []).filter(engine => engine.engine_type === 'postgresql' && engine.lifecycle_state === 'active')
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
    const params = { page: pagination.value.page, page_size: pagination.value.page_size }
    if (filter.value.engine_id) params.engine_id = filter.value.engine_id
    if (filter.value.schema_name) params.schema_name = filter.value.schema_name
    if (filter.value.table_name) params.table_name = filter.value.table_name
    const res = await ruleApplicationAPI.list(params)
    list.value = res?.data || []
    pagination.value.total = res?.total || 0
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.loadFailed'))
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {
  pagination.value.page = 1
  fetchList()
}

const openCreateDialog = () => {
  form.value = { element_id: null, engine_id: null, schema_name: '', table_name: '', column_name: '' }
  elementOptions.value = []
  showCreateDialog.value = true
}

const createItem = async () => {
  if (!form.value.element_id) return ElMessage.warning(t('quality.ruleApplication.elementRequired'))
  if (!form.value.engine_id) return ElMessage.warning(t('quality.ruleApplication.engineRequired'))
  if (!form.value.schema_name.trim()) return ElMessage.warning(t('quality.ruleApplication.schemaRequired'))
  if (!form.value.table_name) return ElMessage.warning(t('quality.ruleApplication.tableRequired'))
  if (!form.value.column_name) return ElMessage.warning(t('quality.ruleApplication.columnRequired'))
  try {
    await ruleApplicationAPI.create(form.value)
    ElMessage.success(t('quality.ruleApplication.createSuccess'))
    showCreateDialog.value = false
    await fetchList()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.deleteFailed'))
  }
}

const deleteItem = async (id) => {
  try {
    await ElMessageBox.confirm(t('quality.ruleApplication.deleteConfirm'), t('quality.ruleApplication.deleteTitle'), { type: 'warning' })
    await ruleApplicationAPI.delete(id)
    ElMessage.success(t('quality.ruleApplication.deleteSuccess'))
    await fetchList()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.ruleApplication.createFailed'))
  }
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
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
