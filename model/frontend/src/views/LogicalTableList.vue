<template>
  <div class="logical-table-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="5">
          <el-input v-model="searchForm.keyword" :placeholder="t('model.logical_table.search_placeholder')" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.domain_id" :placeholder="t('model.logical_table.domain_placeholder')" clearable @change="handleSearch" style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.layer" :placeholder="t('model.logical_table.layer_placeholder')" clearable @change="handleSearch" style="width:100%">
            <el-option :label="t('model.logical_table.layer_ods')" value="ods" />
            <el-option :label="t('model.logical_table.layer_dwd')" value="dwd" />
            <el-option :label="t('model.logical_table.layer_dws')" value="dws" />
            <el-option :label="t('model.logical_table.layer_ads')" value="ads" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.status" :placeholder="t('model.logical_table.status_placeholder')" clearable @change="handleSearch" style="width:100%">
            <el-option :label="t('model.common.status_draft')" value="draft" />
            <el-option :label="t('model.common.status_approved')" value="approved" />
            <el-option :label="t('model.common.status_materialized')" value="materialized" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('model.logical_table.new') }}
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 逻辑表列表 -->
    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="tables" v-loading="loading" stripe>
        <el-table-column :label="t('model.logical_table.name')" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.logical_table.code')" prop="code" width="200" />
        <el-table-column :label="t('model.logical_table.layer')" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.layer" :type="layerTagType(row.layer)" size="small">
              {{ row.layer.toUpperCase() }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.logical_table.table_type')" width="120">
          <template #default="{ row }">
            <el-tag type="info" size="small">{{ row.table_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.logical_table.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.logical_table.created_at')" width="160">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column :label="t('model.logical_table.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">{{ t('model.common.design') }}</el-button>
            <el-popconfirm :title="t('model.logical_table.delete_confirm')" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @change="loadTables"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" :title="t('model.logical_table.new')" width="540px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="100px">
        <el-form-item :label="t('model.logical_table.name')" prop="name">
          <el-input v-model="createForm.name" :placeholder="t('model.logical_table.name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.logical_table.code')" prop="code">
          <el-input v-model="createForm.code" :placeholder="t('model.logical_table.code_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.entity.domain')">
          <el-select v-model="createForm.domain_id" :placeholder="t('model.logical_table.domain_select_placeholder')" clearable style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.logical_table.table_type')" prop="table_type">
          <el-select v-model="createForm.table_type" style="width:100%">
            <el-option :label="t('model.logical_table.type_entity')" value="entity" />
            <el-option :label="t('model.logical_table.type_fact')" value="fact" />
            <el-option :label="t('model.logical_table.type_dimension')" value="dimension" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.logical_table.layer')">
          <el-select v-model="createForm.layer" :placeholder="t('model.logical_table.domain_select_placeholder')" clearable style="width:100%">
            <el-option label="ODS" value="ods" />
            <el-option label="DWD" value="dwd" />
            <el-option label="DWS" value="dws" />
            <el-option label="ADS" value="ads" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.entity.description')">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">{{ t('model.logical_table.create_and_design') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI } from '../api/model'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const tables = ref([])
const domains = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', domain_id: null, layer: '', status: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const createForm = reactive({ name: '', code: '', domain_id: null, table_type: 'entity', layer: '', description: '' })
const createRules = {
  name: [{ required: true, message: t('model.logical_table.name_required'), trigger: 'blur' }],
  code: [{ required: true, message: t('model.logical_table.code_required'), trigger: 'blur' }],
  table_type: [{ required: true, message: t('model.logical_table.type_required'), trigger: 'change' }]
}

const layerTagType = (layer) => ({ ods: '', dwd: 'success', dws: 'warning', ads: 'danger' }[layer] ?? 'info')
const statusTagType = (s) => ({ draft: 'info', approved: 'success', materialized: 'warning' }[s] ?? 'info')
const statusLabel = (s) => ({
  draft: t('model.common.status_draft'),
  approved: t('model.common.status_approved'),
  materialized: t('model.common.status_materialized')
}[s] ?? s)

const loadTables = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      domain_id: searchForm.domain_id || undefined,
      layer: searchForm.layer || undefined,
      status: searchForm.status || undefined
    }
    const res = await logicalTableAPI.list(params)
    tables.value = res.data || []
    pagination.total = res.total || 0
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
  loadTables()
}

const openCreateDialog = () => {
  Object.assign(createForm, { name: '', code: '', domain_id: null, table_type: 'entity', layer: '', description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await logicalTableAPI.create(createForm)
    ElMessage.success(t('model.common.create_success'))
    createDialogVisible.value = false
    router.push(`/modeling/logical-tables/${res.id}`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || t('model.common.create_failed'))
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await logicalTableAPI.delete(id)
    ElMessage.success(t('model.common.delete_success'))
    loadTables()
  } catch {
    ElMessage.error(t('model.common.delete_failed'))
  }
}

const goToDetail = (row) => {
  router.push(`/modeling/logical-tables/${row.id}`)
}

onMounted(async () => {
  const res = await domainAPI.list()
  domains.value = res.data || []
  loadTables()
})
</script>

<style scoped>
.logical-table-list {
  padding: 20px;
}

.search-card {
  margin-bottom: 0;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
