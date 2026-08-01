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
    <el-card shadow="never" style="margin-top:12px">
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
        <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
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
          @change="loadCodeSets"
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
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { codeSetAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const router = useRouter()
const { t } = useI18n()
const loading = ref(false)
const creating = ref(false)
const codeSets = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', type: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
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
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
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
    ElMessage.error(err.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await codeSetAPI.delete(id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    loadCodeSets()
  } catch {
    ElMessage.error(t('standard.common.deleteFailed'))
  }
}

const goToDetail = (row) => {
  navigateStandardRoute(router, `/code-sets/${row.id}`)
}

onMounted(() => {
  loadCodeSets()
})
</script>

<style scoped>
.code-set-list {
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
