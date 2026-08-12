<template>
  <div class="element-list">
    <div class="page-header">
      <h2>{{ $t('standard.element.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.element.create') }}</el-button>
    </div>

    <!-- 筛选栏 -->
    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="8">
          <el-input
            v-model="filters.keyword"
            :placeholder="$t('standard.element.searchPlaceholder')"
            clearable
            @change="handleFilterChange"
            :prefix-icon="Search"
          />
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.domain_id" :placeholder="$t('standard.common.selectDomain')" clearable @change="handleFilterChange">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
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
      <el-table :data="elements" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.element.nameLabel')" min-width="140">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.element.codeLabel')" prop="code" width="150" />
        <el-table-column :label="$t('standard.element.dataTypeLabel')" width="100">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ row.data_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.glossary.domainLabel')" width="120">
          <template #default="{ row }">{{ getDomainName(row.domain_id) || '-' }}</template>
        </el-table-column>
        <el-table-column :label="$t('standard.element.qualityRulesCount')" width="100" align="center">
          <template #default="{ row }">
            <el-badge
              :value="getRuleCount(row.quality_rules)"
              :max="99"
              v-if="getRuleCount(row.quality_rules) > 0"
              type="primary"
            >
              <el-icon class="primary-icon"><Checked /></el-icon>
            </el-badge>
            <span v-else class="no-rules">-</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.status')" width="90">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="210" fixed="right">
          <template #default="{ row }">
            <div class="table-actions">
            <el-button link type="primary" @click="goToDetail(row)">{{ $t('standard.common.detail') }}</el-button>
            <el-button link type="success" @click="handleApprove(row)" v-if="row.status === 'draft'">{{ $t('standard.common.approve') }}</el-button>
            <el-button link type="danger" @click="handleDelete(row)">{{ $t('standard.common.delete') }}</el-button>
            </div>
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

    <!-- 新建对话框 -->
    <el-dialog v-model="dialogVisible" :title="$t('standard.element.createTitle')" width="600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item :label="$t('standard.element.nameLabel')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('standard.element.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.element.codeLabel')" prop="code">
          <el-input v-model="form.code" :placeholder="$t('standard.element.codePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.element.dataTypeLabel')" prop="data_type">
          <el-select v-model="form.data_type" style="width: 100%">
            <el-option :label="$t('standard.element.dataTypeString')" value="string" />
            <el-option :label="$t('standard.element.dataTypeInt')" value="int" />
            <el-option :label="$t('standard.element.dataTypeFloat')" value="float" />
            <el-option :label="$t('standard.element.dataTypeDate')" value="date" />
            <el-option :label="$t('standard.element.dataTypeBool')" value="bool" />
            <el-option :label="$t('standard.element.dataTypeJson')" value="json" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.element.lengthLabel')" v-if="['string'].includes(form.data_type)">
          <el-input-number v-model="form.length" :min="1" />
        </el-form-item>
        <el-form-item :label="$t('standard.element.nullableLabel')">
          <el-switch v-model="form.nullable" />
        </el-form-item>
        <el-form-item :label="$t('standard.glossary.domainLabel')">
          <el-select v-model="form.domain_id" :placeholder="$t('standard.common.domainOptional')" clearable style="width: 100%">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.element.unitLabel')">
          <el-select v-model="form.unit_id" clearable filterable :placeholder="$t('standard.common.domainOptional')" style="width: 100%">
            <el-option-group v-for="cat in unitsByCategory" :key="cat.id" :label="cat.name">
              <el-option v-for="u in cat.units" :key="u.id" :label="`${u.name}（${u.symbol}）`" :value="u.id" />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.element.definitionLabel')">
          <el-input v-model="form.definition" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">{{ $t('standard.element.create') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Plus, Search, Checked } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, elementAPI, unitAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const elements = ref([])
const domainList = ref([])
const units = ref([])
const total = ref(0)
const formRef = ref(null)

const unitsByCategory = computed(() => {
  const map = {}
  units.value.forEach(u => {
    const catId = u.category_id || 0
    const catName = u.category?.name || t('standard.element.other')
    if (!map[catId]) map[catId] = { id: catId, name: catName, units: [] }
    map[catId].units.push(u)
  })
  return Object.values(map)
})

const filters = reactive({
  keyword: typeof route.query.keyword === 'string' ? route.query.keyword : '',
  domain_id: route.query.domain_id ? Number(route.query.domain_id) : null,
  status: typeof route.query.status === 'string' ? route.query.status : '',
  page: Number(route.query.page) > 0 ? Number(route.query.page) : 1,
  page_size: Number(route.query.page_size) > 0 ? Number(route.query.page_size) : 20
})

const form = ref({
  name: '',
  code: '',
  data_type: 'string',
  length: null,
  nullable: true,
  domain_id: null,
  unit_id: null,
  definition: ''
})

const rules = computed(() => ({
  name: [{ required: true, message: t('standard.element.nameRequired'), trigger: 'blur' }],
  code: [{ required: true, message: t('standard.element.codeRequired'), trigger: 'blur' }],
  data_type: [{ required: true, message: t('standard.element.dataTypeRequired'), trigger: 'change' }]
}))

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)

const getRuleCount = (qr) => {
  if (!qr || qr.schema_version !== 'addp.quality.rules/v1' || !Array.isArray(qr.rules)) return 0
  return qr.rules.filter(r => r.enabled).length
}

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
  try {
    const res = await domainAPI.list()
    domainList.value = flattenDomains(res || [])
  } catch (e) {
    domainList.value = []
  }
}

const loadUnits = async () => {
  try {
    const res = await unitAPI.list({ page_size: 500 })
    units.value = res || []
  } catch (e) {
    units.value = []
  }
}

const loadElements = async () => {
  loading.value = true
  try {
    const params = { page: filters.page, page_size: filters.page_size }
    if (filters.keyword) params.keyword = filters.keyword
    if (filters.domain_id) params.domain_id = filters.domain_id
    if (filters.status) params.status = filters.status

    const res = await elementAPI.list(params)
    elements.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const syncQuery = () => {
  const query = {}
  if (filters.keyword) query.keyword = filters.keyword
  if (filters.domain_id) query.domain_id = String(filters.domain_id)
  if (filters.status) query.status = filters.status
  if (filters.page !== 1) query.page = String(filters.page)
  if (filters.page_size !== 20) query.page_size = String(filters.page_size)
  navigateStandardRoute(router, { path: '/elements', query }, { history: 'replace' })
}

const handleFilterChange = () => {
  filters.page = 1
  syncQuery()
  loadElements()
}

const handlePageChange = (page) => {
  filters.page = page
  syncQuery()
  loadElements()
}

const goToDetail = (row) => {
  navigateStandardRoute(router, {
    path: `/elements/${row.id}`,
    query: route.query
  })
}

const openCreateDialog = () => {
  form.value = { name: '', code: '', data_type: 'string', length: null, nullable: true, domain_id: filters.domain_id, unit_id: null, definition: '' }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch {
    return
  }
  submitting.value = true
  try {
    await elementAPI.create(form.value)
    ElMessage.success(t('standard.common.createSuccess'))
    dialogVisible.value = false
    await loadElements()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    submitting.value = false
  }
}

const handleApprove = async (row) => {
  try {
    await elementAPI.approve(row.id)
    ElMessage.success(t('standard.common.approveSuccess'))
    await loadElements()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.approveFailed'))
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.element.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await elementAPI.delete(row.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    await loadElements()
  } catch (e) {
    if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
  }
}

onMounted(async () => {
  await loadDomains()
  await loadElements()
  loadUnits()
})
</script>

<style scoped>
.element-list {
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

.primary-icon {
  color: var(--el-color-primary);
}

.no-rules {
  color: var(--el-text-color-placeholder);
}

.pagination {
  padding: 16px;
  display: flex;
  justify-content: flex-end;
}

.table-actions { display: flex; align-items: center; gap: 8px; white-space: nowrap; }
</style>
