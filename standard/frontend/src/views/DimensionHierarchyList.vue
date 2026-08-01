<template>
  <div class="dim-hierarchy-list">
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="8">
          <el-input v-model="keyword" :placeholder="$t('standard.dimHierarchy.searchPlaceholder')" clearable @change="handleSearch">
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
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">
              {{ $t('standard.dimHierarchy.manageLevels') }}
            </el-button>
            <el-popconfirm :title="$t('standard.dimHierarchy.confirmDelete')" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger">{{ $t('standard.common.delete') }}</el-button>
              </template>
            </el-popconfirm>
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
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const list = ref([])
const keyword = ref('')
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
  } catch {
    ElMessage.error(t('standard.dimHierarchy.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleSearch() {}

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
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    creating.value = false
  }
}

const openDetail = row => navigateStandardRoute(router, `/dimension-hierarchies/${row.id}`)

async function handleDelete(id) {
  try {
    await dimensionHierarchyAPI.delete(id)
    ElMessage.success(t('standard.dimHierarchy.deleted'))
    loadList()
  } catch {
    ElMessage.error(t('standard.dimHierarchy.deleteFailed'))
  }
}

onMounted(loadList)
</script>

<style scoped>
.search-card { margin-bottom: 0; }
</style>
