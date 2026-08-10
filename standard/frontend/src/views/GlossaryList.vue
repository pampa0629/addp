<template>
  <div class="glossary-list">
    <div class="page-header">
      <h2>{{ $t('standard.glossary.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.glossary.create') }}</el-button>
    </div>

    <!-- 筛选栏 -->
    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="8">
          <el-input
            v-model="filters.keyword"
            :placeholder="$t('standard.glossary.searchPlaceholder')"
            clearable
            @change="handleFilterChange"
            :prefix-icon="Search"
          />
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.domain_id" :placeholder="$t('standard.common.selectDomain')" clearable @change="handleFilterChange">
            <el-option
              v-for="domain in domainList"
              :key="domain.id"
              :label="domain.name"
              :value="domain.id"
            />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.status" :placeholder="$t('standard.common.selectStatus')" clearable @change="handleFilterChange">
            <el-option :label="$t('standard.common.draft')" value="draft" />
            <el-option :label="$t('standard.common.approved')" value="approved" />
            <el-option :label="$t('standard.common.deprecated')" value="deprecated" />
          </el-select>
        </el-col>
      </el-row>
    </el-card>

    <!-- 列表 -->
    <el-card class="table-card">
      <el-table :data="glossaries" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.glossary.nameLabel')" min-width="150">
          <template #default="{ row }">
            <span class="term-name">{{ row.name }}</span>
            <div v-if="row.alias && row.alias.length > 0" class="alias-list">
              <el-tag v-for="a in row.alias" :key="a" size="small" type="info" class="alias-tag">{{ a }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.glossary.domainLabel')" width="120">
          <template #default="{ row }">
            <span>{{ getDomainName(row.domain_id) || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.glossary.definitionLabel')" prop="definition" show-overflow-tooltip />
        <el-table-column :label="$t('standard.common.status')" width="90">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.tags')" width="180">
          <template #default="{ row }">
            <el-tag v-for="tag in (row.tags || [])" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">{{ $t('standard.common.detail') }}</el-button>
            <el-button link type="success" @click="handleApprove(row)" v-if="row.status === 'draft'">{{ $t('standard.common.approve') }}</el-button>
            <el-button link type="warning" @click="handleDeprecate(row)" v-if="row.status === 'approved'">{{ $t('standard.common.deprecate') }}</el-button>
            <el-button link type="danger" @click="handleDelete(row)">{{ $t('standard.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="total > 0"
        class="pagination"
        :total="total"
        :page-size="filters.page_size"
        :current-page="filters.page"
        layout="total, prev, pager, next"
        @current-change="handlePageChange"
      />
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="editMode ? $t('standard.glossary.editTitle') : $t('standard.glossary.createTitle')" width="600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="$t('standard.glossary.nameLabel')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('standard.glossary.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.aliasLabel')">
          <el-select
            v-model="form.alias"
            multiple filterable allow-create default-first-option
            :placeholder="$t('standard.glossary.aliasPlaceholder')"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.domainLabel')">
          <el-select v-model="form.domain_id" :placeholder="$t('standard.common.domainOptional')" clearable style="width: 100%">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.definitionLabel')" prop="definition">
          <el-input v-model="form.definition" type="textarea" :rows="4" :placeholder="$t('standard.glossary.definitionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.exampleLabel')">
          <el-input v-model="form.example" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.noteLabel')">
          <el-input v-model="form.note" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.tags')">
          <el-select
            v-model="form.tags"
            multiple filterable allow-create default-first-option
            :placeholder="$t('standard.common.tagsPlaceholder')"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">{{ $t('standard.common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { domainAPI, glossaryAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()

const router = useRouter()
const route = useRoute()

const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const glossaries = ref([])
const domainList = ref([])
const total = ref(0)
const editingId = ref(null)
const formRef = ref(null)

const filters = reactive({
  keyword: '',
  domain_id: null,
  status: '',
  page: 1,
  page_size: 20
})

const validStatuses = new Set(['draft', 'approved', 'deprecated'])

const parsePositiveInteger = (value, fallback) => {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

const applyRouteFilters = (query) => {
  filters.keyword = typeof query.keyword === 'string' ? query.keyword : ''
  filters.domain_id = parsePositiveInteger(query.domain_id, null)
  filters.status = typeof query.status === 'string' && validStatuses.has(query.status) ? query.status : ''
  filters.page = parsePositiveInteger(query.page, 1)
  filters.page_size = parsePositiveInteger(query.page_size, 20)
}

const buildFilterQuery = () => {
  const query = {}
  if (filters.keyword) query.keyword = filters.keyword
  if (filters.domain_id) query.domain_id = String(filters.domain_id)
  if (filters.status) query.status = filters.status
  if (filters.page > 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  return query
}

const syncFilterRoute = () => navigateStandardRoute(router, { path: '/glossaries', query: buildFilterQuery() }, { history: 'replace' })

const form = ref({
  name: '',
  alias: [],
  domain_id: null,
  definition: '',
  example: '',
  note: '',
  tags: []
})

const rules = computed(() => ({
  name: [{ required: true, message: t('standard.glossary.nameRequired'), trigger: 'blur' }],
  definition: [{ required: true, message: t('standard.glossary.definitionRequired'), trigger: 'blur' }]
}))

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)

const flattenDomains = (nodes) => {
  const result = []
  const traverse = (list) => {
    for (const n of list) {
      result.push(n)
      if (n.children) traverse(n.children)
    }
  }
  traverse(nodes)
  return result
}

const getDomainName = (id) => {
  if (!id) return null
  return domainList.value.find(d => d.id === id)?.name || null
}

const loadDomains = async () => {
  const res = await domainAPI.list()
  domainList.value = flattenDomains(res || [])
}

const loadGlossaries = async () => {
  loading.value = true
  try {
    const params = { page: filters.page, page_size: filters.page_size }
    if (filters.keyword) params.keyword = filters.keyword
    if (filters.domain_id) params.domain_id = filters.domain_id
    if (filters.status) params.status = filters.status

    const res = await glossaryAPI.list(params)
    glossaries.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    ElMessage.error(t('standard.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handlePageChange = (page) => {
  filters.page = page
  syncFilterRoute()
  loadGlossaries()
}

const handleFilterChange = () => {
  filters.page = 1
  syncFilterRoute()
  loadGlossaries()
}

const openCreateDialog = () => {
  editMode.value = false
  editingId.value = null
  form.value = { name: '', alias: [], domain_id: filters.domain_id, definition: '', example: '', note: '', tags: [] }
  dialogVisible.value = true
}

const goToDetail = (row) => {
  navigateStandardRoute(router, { path: `/glossaries/${row.id}`, query: buildFilterQuery() })
}

const openEditDialog = (row) => {
  editMode.value = true
  editingId.value = row.id
  form.value = {
    name: row.name,
    alias: row.alias || [],
    domain_id: row.domain_id || null,
    definition: row.definition,
    example: row.example || '',
    note: row.note || '',
    tags: row.tags || []
  }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async valid => {
    if (!valid) return
    submitting.value = true
    try {
      if (editMode.value) {
        await glossaryAPI.update(editingId.value, form.value)
        ElMessage.success(t('standard.common.updateSuccess'))
      } else {
        await glossaryAPI.create(form.value)
        ElMessage.success(t('standard.common.createSuccess'))
      }
      dialogVisible.value = false
      await loadGlossaries()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
    } finally {
      submitting.value = false
    }
  })
}

const handleApprove = async (row) => {
  try {
    await glossaryAPI.approve(row.id)
    ElMessage.success(t('standard.common.approveSuccess'))
    await loadGlossaries()
  } catch (e) {
    ElMessage.error(t('standard.common.approveFailed'))
  }
}

const handleDeprecate = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.glossary.confirmDeprecate', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await glossaryAPI.deprecate(row.id)
    ElMessage.success(t('standard.common.deprecated'))
    await loadGlossaries()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.operationFailed'))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.glossary.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await glossaryAPI.delete(row.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    await loadGlossaries()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.deleteFailed'))
  }
}

watch(
  () => route.query,
  (query) => {
    const previous = JSON.stringify(buildFilterQuery())
    applyRouteFilters(query)
    if (JSON.stringify(buildFilterQuery()) !== previous) loadGlossaries()
  },
  { deep: true }
)

onMounted(async () => {
  applyRouteFilters(route.query)
  await loadDomains()
  await loadGlossaries()
})
</script>

<style scoped>
.glossary-list {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0;
  font-size: 18px;
  color: var(--el-text-color-primary);
}

.filter-card {
  margin-bottom: 16px;
}

.table-card :deep(.el-card__body) {
  padding: 0;
}

.term-name {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.alias-list {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.alias-tag {
  font-size: 11px;
}

.tag-item {
  margin-right: 4px;
  margin-bottom: 2px;
}

.pagination {
  padding: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
