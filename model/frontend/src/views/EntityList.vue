<template>
  <div class="entity-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="6">
          <el-input v-model="searchForm.keyword" :placeholder="t('model.entity.search_placeholder')" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="5">
          <el-select v-model="searchForm.domain_id" :placeholder="t('model.entity.domain')" clearable @change="handleSearch" style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.status" :placeholder="t('model.entity.status')" clearable @change="handleSearch" style="width:100%">
            <el-option :label="t('model.common.status_draft')" value="draft" />
            <el-option :label="t('model.common.status_approved')" value="approved" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button v-if="can('model.entity.create')" type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ t('model.entity.new') }}
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <el-alert
      v-if="loadError"
      class="load-error"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
    >
      <el-button link type="danger" @click="reload">{{ t('model.common.retry') }}</el-button>
    </el-alert>

    <!-- 实体列表 -->
    <el-card v-else shadow="never" style="margin-top:12px">
      <el-table :data="entities" v-loading="loading" stripe>
        <el-table-column :label="t('model.entity.name')" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.entity.code')" prop="code" width="160" />
        <el-table-column :label="t('model.entity.domain')" width="140">
          <template #default="{ row }">
            {{ getDomainName(row.domain_id) || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('model.entity.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'approved' ? 'success' : 'info'" size="small">
              {{ row.status === 'approved' ? t('model.common.status_approved') : t('model.common.status_draft') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.entity.created_at')" width="170">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column :label="t('model.attribute.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">{{ t('model.common.design') }}</el-button>
            <el-popconfirm v-if="can('model.entity.delete')" :title="t('model.entity.delete_confirm')" @confirm="handleDelete(row.id)">
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
          @change="loadEntities"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" :title="t('model.entity.new')" width="480px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="90px">
        <el-form-item :label="t('model.entity.name')" prop="name">
          <el-input v-model="createForm.name" :placeholder="t('model.entity.name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.entity.code')" prop="code">
          <el-input v-model="createForm.code" :placeholder="t('model.entity.code_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.entity.domain')">
          <el-select v-model="createForm.domain_id" :placeholder="t('model.entity.domain_placeholder')" clearable style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.entity.description')">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">{{ t('model.entity.create_and_detail') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { entityAPI, domainAPI } from '../api/model'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'

const { t } = useI18n()

const router = useRouter()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)
const loading = ref(false)
const loadError = ref('')
const creating = ref(false)
const entities = ref([])
const domains = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', domain_id: null, status: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

const createForm = reactive({ name: '', code: '', domain_id: null, description: '' })
const createRules = {
  name: [{ required: true, message: t('model.entity.name_required'), trigger: 'blur' }],
  code: [{ required: true, message: t('model.entity.code_required'), trigger: 'blur' }]
}

const getDomainName = (id) => domains.value.find(d => d.id === id)?.name

const loadEntities = async () => {
  loading.value = true
  loadError.value = ''
  if (!can('model.entity.read')) {
    entities.value = []
    pagination.total = 0
    loadError.value = t('model.common.permission_denied')
    loading.value = false
    return
  }
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      domain_id: searchForm.domain_id || undefined,
      status: searchForm.status || undefined
    }
    const res = await entityAPI.list(params)
    entities.value = res.data || []
    pagination.total = res.total || 0
  } catch (err) {
    entities.value = []
    pagination.total = 0
    loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
  } finally {
    loading.value = false
  }
}

const loadDomains = async () => {
  try {
    const res = await domainAPI.list()
    domains.value = res || []
  } catch (err) {
    loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
  }
}

const reload = async () => {
  await Promise.all([loadDomains(), loadEntities()])
}

const handleSearch = () => {
  pagination.page = 1
  loadEntities()
}

const openCreateDialog = () => {
  Object.assign(createForm, { name: '', code: '', domain_id: null, description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await entityAPI.create(createForm)
    ElMessage.success(t('model.common.create_success'))
    createDialogVisible.value = false
    navigateModelRoute(router, `/entities/${res.id}`, { history: 'replace' })
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.create_failed'))
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await entityAPI.delete(id)
    ElMessage.success(t('model.common.delete_success'))
    loadEntities()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

const goToDetail = (row) => {
  navigateModelRoute(router, `/entities/${row.id}`)
}

onMounted(() => {
  reload()
})
</script>

<style scoped>
.entity-list {
  padding: 20px;
}

.search-card {
  margin-bottom: 0;
}

.load-error {
  margin-top: 12px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
