<template>
  <div class="dim-hierarchy-list">
    <div class="page-header">
      <h2>{{ $t('standard.dimHierarchy.title') }}</h2>
    </div>
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="8">
          <el-input v-model="keyword" :placeholder="$t('standard.dimHierarchy.searchPlaceholder')" clearable @change="syncSearch" @clear="syncSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            {{ $t('standard.dimHierarchy.create') }}
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="filteredList" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.dimHierarchy.nameLabel')" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="openDetail(row)">
              {{ row.name }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.code')" prop="code" width="140" />
        <el-table-column :label="$t('standard.dimHierarchy.levelCountLabel')" width="80">
          <template #default="{ row }">
            <el-tag type="info" size="small">{{ $t('standard.dimHierarchy.levelCount', { count: row.levels?.length ?? 0 }) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.description')" prop="description" min-width="200" show-overflow-tooltip />
        <el-table-column :label="$t('standard.common.createdAt')" width="160">
          <template #default="{ row }">
            {{ formatStandardDateTime(row.created_at, locale) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="220" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
              <el-button link type="primary" @click="openDetail(row)">
                {{ $t('standard.dimHierarchy.manageLevels') }}
              </el-button>
              <el-button link type="danger" @click="handleDelete(row)">{{ $t('standard.common.delete') }}</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createVisible" :title="$t('standard.dimHierarchy.createTitle')" width="480px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="90px">
        <el-form-item :label="$t('standard.dimHierarchy.nameLabel')" prop="name">
          <el-input v-model="createForm.name" :placeholder="$t('standard.dimHierarchy.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.code')" prop="code">
          <el-input v-model="createForm.code" :placeholder="$t('standard.dimHierarchy.codePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">{{ $t('standard.dimHierarchy.createAndEdit') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const list = ref([])
const keyword = ref(typeof route.query.keyword === 'string' ? route.query.keyword : '')
const createVisible = ref(false)
const createFormRef = ref(null)

const createForm = reactive({ name: '', code: '', description: '' })
const createRules = computed(() => ({
  name: [{ required: true, message: t('standard.dimHierarchy.nameRequired'), trigger: 'blur' }],
  code: [{ required: true, message: t('standard.dimHierarchy.codeRequired'), trigger: 'blur' }]
}))

const filteredList = computed(() => {
  if (!keyword.value) return list.value
  const kw = keyword.value.toLowerCase()
  return list.value.filter(h => h.name.toLowerCase().includes(kw) || h.code.toLowerCase().includes(kw))
})

async function loadList() {
  loading.value = true
  try {
    const res = await dimensionHierarchyAPI.list()
    list.value = res || []
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.dimHierarchy.loadFailed'))
  } finally {
    loading.value = false
  }
}

function syncSearch() {
  navigateStandardRoute(router, {
    path: '/dimension-hierarchies',
    query: keyword.value ? { keyword: keyword.value } : {}
  }, { history: 'replace' })
}

function openCreateDialog() {
  Object.assign(createForm, { name: '', code: '', description: '' })
  createVisible.value = true
}

async function handleCreate() {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await dimensionHierarchyAPI.create({ ...createForm })
    createVisible.value = false
    await navigateStandardRoute(router, `/dimension-hierarchies/${res.id}`, { history: 'replace' })
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    creating.value = false
  }
}

const openDetail = row => navigateStandardRoute(router, {
  path: `/dimension-hierarchies/${row.id}`,
  query: keyword.value ? { keyword: keyword.value } : {}
})

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(t('standard.dimHierarchy.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await dimensionHierarchyAPI.delete(row.id)
    ElMessage.success(t('standard.dimHierarchy.deleted'))
    loadList()
  } catch (err) {
    if (!isCanceledInteraction(err)) ElMessage.error(getStandardErrorMessage(err, t, 'standard.dimHierarchy.deleteFailed'))
  }
}

watch(() => route.query.keyword, value => {
  const next = typeof value === 'string' ? value : ''
  if (next !== keyword.value) keyword.value = next
})

onMounted(loadList)
</script>

<style scoped>
.dim-hierarchy-list { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.page-header { margin-bottom: 16px; }
.page-header h2 { margin: 0; font-size: 18px; color: var(--addp-text-primary); }
.search-card { margin-bottom: 0; }
.dim-hierarchy-list :deep(.el-card) { color: var(--addp-text-primary); background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }
.dim-hierarchy-list :deep(.el-table) {
  --el-table-bg-color: var(--addp-bg-primary);
  --el-table-tr-bg-color: var(--addp-bg-primary);
  --el-table-header-bg-color: var(--addp-bg-secondary);
  --el-table-border-color: var(--addp-border-color-light);
  --el-table-text-color: var(--addp-text-primary);
  --el-table-header-text-color: var(--addp-text-secondary);
}
.table-actions { display: inline-flex; align-items: center; justify-content: flex-start; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }
</style>
