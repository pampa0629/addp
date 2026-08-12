<template>
  <div class="code-set-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="6">
          <el-input v-model="searchForm.keyword" :placeholder="$t('standard.codeSet.searchPlaceholder')" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.type" :placeholder="$t('standard.common.type')" clearable @change="handleSearch" style="width:100%">
            <el-option :label="$t('standard.codeSet.typeSystem')" value="system" />
            <el-option :label="$t('standard.codeSet.typeCustom')" value="custom" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ $t('standard.codeSet.create') }}
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 码值集列表 -->
    <el-card shadow="never" class="list-card">
      <el-table :data="codeSets" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.codeSet.codeLabel')" prop="code" width="180" />
        <el-table-column :label="$t('standard.codeSet.nameLabel')" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.type')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.type === 'system' ? 'success' : 'info'" size="small">
              {{ row.type === 'system' ? $t('standard.codeSet.typeSystem') : $t('standard.codeSet.typeCustom') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.description')" prop="description" show-overflow-tooltip />
        <el-table-column :label="$t('standard.common.createdAt')" width="160">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="160" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" @click="goToDetail(row)">{{ $t('standard.common.edit') }}</el-button>
              <el-popconfirm
                :title="$t('standard.codeSet.confirmDelete')"
                @confirm="handleDelete(row.id)"
                :disabled="row.type === 'system'"
              >
                <template #reference>
                  <el-button link type="danger" :disabled="row.type === 'system'">{{ $t('standard.common.delete') }}</el-button>
                </template>
              </el-popconfirm>
            </div>
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
          @change="handlePageChange"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" :title="$t('standard.codeSet.createTitle')" width="540px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="100px">
        <el-form-item :label="$t('standard.codeSet.codeLabel')" prop="code">
          <el-input v-model="createForm.code" :placeholder="$t('standard.codeSet.codePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.codeSet.nameLabel')" prop="name">
          <el-input v-model="createForm.name" :placeholder="$t('standard.codeSet.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.type')" prop="type">
          <el-select v-model="createForm.type" style="width:100%">
            <el-option :label="$t('standard.codeSet.typeCustom')" value="custom" />
            <el-option :label="$t('standard.codeSet.typeSystem')" value="system" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">{{ $t('standard.codeSet.createAndEdit') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { codeSetAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage } from '../utils/apiError'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const loading = ref(false)
const creating = ref(false)
const codeSets = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({
  keyword: typeof route.query.keyword === 'string' ? route.query.keyword : '',
  type: typeof route.query.type === 'string' ? route.query.type : ''
})
const pagination = reactive({
  page: Number(route.query.page) > 0 ? Number(route.query.page) : 1,
  pageSize: Number(route.query.page_size) > 0 ? Number(route.query.page_size) : 20,
  total: 0
})
const createForm = reactive({ code: '', name: '', type: 'custom', description: '' })
const createRules = computed(() => ({
  code: [{ required: true, message: t('standard.codeSet.codeRequired'), trigger: 'blur' }],
  name: [{ required: true, message: t('standard.codeSet.nameRequired'), trigger: 'blur' }],
  type: [{ required: true, message: t('standard.codeSet.typeRequired'), trigger: 'change' }]
}))

const loadCodeSets = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      type: searchForm.type || undefined
    }
    const res = await codeSetAPI.list(params)
    codeSets.value = res.data || []
    pagination.total = res.total || 0
  } catch (err) {
    codeSets.value = []
    pagination.total = 0
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
  syncQuery()
  loadCodeSets()
}

const syncQuery = () => {
  const query = {}
  if (searchForm.keyword) query.keyword = searchForm.keyword
  if (searchForm.type) query.type = searchForm.type
  if (pagination.page !== 1) query.page = String(pagination.page)
  if (pagination.pageSize !== 20) query.page_size = String(pagination.pageSize)
  navigateStandardRoute(router, { path: '/code-sets', query }, { history: 'replace' })
}

const handlePageChange = () => {
  syncQuery()
  loadCodeSets()
}

const openCreateDialog = () => {
  Object.assign(createForm, { code: '', name: '', type: 'custom', description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await codeSetAPI.create(createForm)
    ElMessage.success(t('standard.common.createSuccess'))
    createDialogVisible.value = false
    await navigateStandardRoute(router, `/code-sets/${res.id}`, { history: 'replace' })
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t))
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await codeSetAPI.delete(id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    loadCodeSets()
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.deleteFailed'))
  }
}

const goToDetail = (row) => {
  navigateStandardRoute(router, { path: `/code-sets/${row.id}`, query: route.query })
}

onMounted(() => {
  loadCodeSets()
})
</script>

<style scoped>
.code-set-list {
  min-height: 100%;
  padding: 20px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
}

.search-card {
  margin-bottom: 0;
}

.list-card { margin-top: 12px; }

.code-set-list :deep(.el-card) {
  background: var(--addp-bg-primary);
  border-color: var(--addp-border-color);
  box-shadow: var(--addp-shadow-card);
}

.table-actions { display: inline-flex; align-items: center; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
