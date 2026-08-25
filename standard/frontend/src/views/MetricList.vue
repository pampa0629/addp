<template>
  <div class="metric-list">
    <div class="page-header">
      <div class="header-left">
        <h2>{{ $t('standard.metric.title') }}</h2>
        <el-radio-group v-model="filterType" size="small" @change="handleFilterChange">
          <el-radio-button value="">{{ $t('standard.metric.all') }}</el-radio-button>
          <el-radio-button value="atomic">{{ $t('standard.metric.atomic') }}</el-radio-button>
          <el-radio-button value="derived">{{ $t('standard.metric.derived') }}</el-radio-button>
          <el-radio-button value="composite">{{ $t('standard.metric.composite') }}</el-radio-button>
        </el-radio-group>
      </div>
      <el-button v-if="canCreate" type="primary" @click="openCreateDialog">{{ $t('standard.metric.create') }}</el-button>
    </div>

    <el-row :gutter="16">
      <!-- 左侧：指标目录 -->
      <el-col :span="5">
        <el-card class="category-card">
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.metric.categoryTitle') }}</span>
              <el-button v-if="canManageCategories" link size="small" @click="showCategoryDialog = true">{{ $t('standard.metric.categoryManageBtn') }}</el-button>
            </div>
          </template>
          <div class="category-item" :class="{ active: !selectedCategoryID }" @click="selectCategory(null)">
            {{ $t('standard.metric.allMetrics') }}
          </div>
          <el-tree
            :data="categoryTree"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            :highlight-current="false"
            @node-click="(data) => selectCategory(data.id)"
          >
            <template #default="{ data }">
              <div :class="['cat-node', { active: selectedCategoryID === data.id }]">
                {{ data.name }}
              </div>
            </template>
          </el-tree>
        </el-card>
      </el-col>

      <!-- 右侧：指标列表 -->
      <el-col :span="19">
        <el-card>
          <div class="toolbar">
            <el-input v-model="keyword" :placeholder="$t('standard.metric.searchPlaceholder')" clearable @change="handleFilterChange" style="width:280px" />
            <el-select v-model="filterStatus" :placeholder="$t('standard.common.status')" clearable @change="handleFilterChange" style="width:120px">
              <el-option :label="$t('standard.common.draft')" value="draft" />
              <el-option :label="$t('standard.common.approved')" value="approved" />
              <el-option :label="$t('standard.common.deprecated')" value="deprecated" />
            </el-select>
          </div>

          <el-table :data="metrics" v-loading="loading" size="small" @row-click="openDetail">
            <el-table-column :label="$t('standard.metric.nameLabel')" prop="name" min-width="140" />
            <el-table-column :label="$t('standard.common.code')" prop="code" width="140" />
            <el-table-column :label="$t('standard.common.type')" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="typeTagType(row.type)">{{ typeLabel(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.metric.definitionLabel')" prop="definition" show-overflow-tooltip min-width="200" />
            <el-table-column :label="$t('standard.common.status')" width="85">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.actions')" width="200" fixed="right">
              <template #default="{ row }">
                <div class="table-actions">
                <el-button link size="small" @click.stop="openDetail(row)">{{ $t('standard.common.detail') }}</el-button>
                <el-button v-if="canApprove && row.status === 'draft'" link size="small" type="success" :loading="isActionLocked(`metric:${row.id}`)" @click.stop="approveMetric(row)">{{ $t('standard.common.approve') }}</el-button>
                <el-button v-if="canDelete" link size="small" type="danger" :disabled="isActionLocked(`metric:${row.id}`)" @click.stop="deleteMetric(row)">{{ $t('standard.common.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>

          <div class="pagination">
            <el-pagination
              v-model:current-page="page"
              v-model:page-size="pageSize"
              :total="total"
              :page-sizes="[20, 50]"
              layout="total, sizes, prev, pager, next"
              @change="handlePageChange"
            />
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增指标对话框 -->
    <el-dialog v-model="showCreateDialog" :title="$t('standard.metric.createTitle')" width="600px">
      <el-form ref="formRef" :model="form" :rules="metricRules" label-width="100px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.nameLabel')" prop="name">
              <el-input v-model="form.name" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.codeLabel')" prop="code">
              <el-input v-model="form.code" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.typeLabel')" prop="type">
              <el-select v-model="form.type" style="width:100%">
                <el-option :label="$t('standard.metric.atomic')" value="atomic" />
                <el-option :label="$t('standard.metric.derived')" value="derived" />
                <el-option :label="$t('standard.metric.composite')" value="composite" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.categoryLabel')">
              <el-tree-select
                v-model="form.category_id"
                :data="categoryTree"
                :props="{ label: 'name', value: 'id', children: 'children' }"
                clearable style="width:100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="$t('standard.glossary.domainLabel')">
          <el-select v-model="form.domain_id" filterable :placeholder="$t('standard.common.domainOptional')" style="width: 100%">
            <el-option v-for="domain in domainList" :key="domain.id" :label="domain.name" :value="domain.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.metric.definitionLabel')">
          <el-input v-model="form.definition" type="textarea" :rows="3" :placeholder="$t('standard.metric.definitionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.metric.derivationConfigLabel')">
          <el-input
            v-model="derivationConfigText"
            class="derivation-config-input"
            type="textarea"
            :rows="10"
            resize="vertical"
            :placeholder="$t('standard.metric.derivationConfigPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="$t('standard.metric.formulaLabel')" v-if="form.type === 'composite'">
          <el-input v-model="form.formula" type="textarea" :rows="2" :placeholder="$t('standard.metric.formulaPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.metric.baseMetricLabel')" v-if="form.type === 'derived'">
          <el-select v-model="form.base_metric_id" filterable clearable style="width:100%" :placeholder="$t('standard.metric.baseMetricPlaceholder')">
            <el-option v-for="m in atomicMetrics" :key="m.id" :label="`${m.name}（${m.code}）`" :value="m.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="createMetric" :loading="saving">{{ $t('standard.metric.create') }}</el-button>
      </template>
    </el-dialog>

    <!-- 指标目录管理对话框 -->
    <el-dialog v-model="showCategoryDialog" :title="$t('standard.metric.categoryManage')" width="500px">
      <div class="category-manage">
        <el-tree
          :data="categoryTree"
          :props="{ label: 'name', children: 'children' }"
          node-key="id"
          default-expand-all
        >
          <template #default="{ data }">
            <div class="tree-node">
              <span class="tree-name">{{ data.name }}</span>
              <span class="tree-code">{{ data.code }}</span>
              <div class="tree-actions">
                <el-button v-if="canCreate" link size="small" @click.stop="addSubCategory(data.id)">{{ $t('standard.metric.addSubCategory') }}</el-button>
                <el-button v-if="canDelete" link size="small" type="danger" :loading="isActionLocked(`metric-category:${data.id}`)" @click.stop="deleteCategory(data)">{{ $t('standard.common.delete') }}</el-button>
              </div>
            </div>
          </template>
        </el-tree>
        <el-divider />
        <el-form v-if="canCreate" :model="categoryForm" label-width="80px" size="small">
          <el-row :gutter="12">
            <el-col :span="12">
              <el-form-item :label="$t('standard.common.name')">
                <el-input v-model="categoryForm.name" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item :label="$t('standard.common.code')">
                <el-input v-model="categoryForm.code" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item :label="$t('standard.metric.categoryLabel')">
            <el-tree-select
              v-model="categoryForm.parent_id"
              :data="categoryTree"
              :props="{ label: 'name', value: 'id', children: 'children' }"
              clearable :placeholder="$t('standard.metric.categoryParentPlaceholder')" style="width:100%"
            />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="createCategory" :loading="saving">{{ $t('standard.metric.addCategory') }}</el-button>
          </el-form-item>
        </el-form>
      </div>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, metricAPI, metricCategoryAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useActionLock } from '../composables/useActionLock'
import { createLatestRequestCoordinator } from '@common-ui'
import { parseMetricDerivationConfig } from '../utils/metricDerivationConfig'

const { t } = useI18n()
const { canCreate, canDelete, canApprove } = useStandardPermissions('metric')
const { isLocked: isActionLocked, runLocked } = useActionLock()
const canManageCategories = computed(() => canCreate.value || canDelete.value)
const router = useRouter()
const route = useRoute()
const metrics = ref([])
const categories = ref([])
const domainList = ref([])
const atomicMetrics = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(Number(route.query.page) > 0 ? Number(route.query.page) : 1)
const pageSize = ref(Number(route.query.page_size) > 0 ? Number(route.query.page_size) : 20)
const total = ref(0)
const keyword = ref(typeof route.query.keyword === 'string' ? route.query.keyword : '')
const filterType = ref(typeof route.query.type === 'string' ? route.query.type : '')
const filterStatus = ref(typeof route.query.status === 'string' ? route.query.status : '')
const selectedCategoryID = ref(route.query.category_id ? Number(route.query.category_id) : null)

const showCreateDialog = ref(false)
const showCategoryDialog = ref(false)
const formRef = ref(null)
const listRequests = createLatestRequestCoordinator()

const form = ref({ name: '', code: '', type: 'atomic', definition: '', formula: '', category_id: null, domain_id: null, base_metric_id: null })
const derivationConfigText = ref('')
const categoryForm = ref({ name: '', code: '', parent_id: null })
const metricRules = computed(() => ({
  name: [{ required: true, message: t('standard.metric.nameRequired'), trigger: 'blur' }],
  code: [{ required: true, message: t('standard.metric.codeRequired'), trigger: 'blur' }],
  type: [{ required: true, message: t('standard.metric.typeRequired'), trigger: 'change' }]
}))

const categoryTree = computed(() => buildTree(categories.value))
function buildTree(list, parentId = null) {
  return list.filter(i => (i.parent_id || null) === parentId).map(i => ({ ...i, children: buildTree(list, i.id) }))
}

const flattenDomains = (nodes) => {
  const result = []
  const traverse = (list) => {
    for (const node of list) {
      result.push(node)
      if (node.children) traverse(node.children)
    }
  }
  traverse(nodes)
  return result
}

const typeLabel = (type) => ({ atomic: t('standard.metric.atomicShort'), derived: t('standard.metric.derivedShort'), composite: t('standard.metric.compositeShort') }[type] || type)
const typeTagType = (type) => ({ atomic: 'primary', derived: 'warning', composite: 'success' }[type] || '')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)
const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || '')

const loadMetrics = async () => {
  const params = { page: page.value, page_size: pageSize.value, keyword: keyword.value, type: filterType.value, status: filterStatus.value }
  if (selectedCategoryID.value) params.category_id = selectedCategoryID.value
  const request = listRequests.begin(JSON.stringify(params))
  loading.value = true
  try {
    const res = await metricAPI.list(params)
    if (!listRequests.isCurrent(request, JSON.stringify(params))) return
    metrics.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    if (!listRequests.isCurrent(request, JSON.stringify(params))) return
    metrics.value = []
    total.value = 0
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  } finally {
    if (listRequests.isCurrent(request, JSON.stringify(params))) loading.value = false
  }
}

const syncQuery = () => {
  const query = {}
  if (keyword.value) query.keyword = keyword.value
  if (filterType.value) query.type = filterType.value
  if (filterStatus.value) query.status = filterStatus.value
  if (selectedCategoryID.value) query.category_id = String(selectedCategoryID.value)
  if (page.value !== 1) query.page = String(page.value)
  if (pageSize.value !== 20) query.page_size = String(pageSize.value)
  navigateStandardRoute(router, { path: '/metrics', query }, { history: 'replace' })
}

const handleFilterChange = () => {
  page.value = 1
  syncQuery()
  loadMetrics()
}

const handlePageChange = () => {
  syncQuery()
  loadMetrics()
}

const loadCategories = async () => {
  try {
    const res = await metricCategoryAPI.list()
    categories.value = res || []
  } catch (e) {
    categories.value = []
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  }
}

const loadDomains = async () => {
  try {
    const res = await domainAPI.list()
    domainList.value = flattenDomains(res || [])
  } catch (e) {
    domainList.value = []
  }
}

const loadAtomicMetrics = async () => {
  try {
    const res = await metricAPI.list({ type: 'atomic', page_size: 500 })
    atomicMetrics.value = res.data || []
  } catch (e) {
    atomicMetrics.value = []
  }
}

const selectCategory = (id) => {
  selectedCategoryID.value = id
  page.value = 1
  syncQuery()
  loadMetrics()
}

const createMetric = async () => {
  if (saving.value || !formRef.value) return
  saving.value = true
  try {
    await formRef.value.validate()
  } catch {
    saving.value = false
    return
  }
  try {
    const payload = { ...form.value, derivation_config: parseMetricDerivationConfig(derivationConfigText.value) }
    await metricAPI.create(payload)
    ElMessage.success(t('standard.common.createSuccess'))
    showCreateDialog.value = false
    form.value = { name: '', code: '', type: 'atomic', definition: '', formula: '', category_id: null, domain_id: null, base_metric_id: null }
    derivationConfigText.value = ''
    loadMetrics()
  } catch (e) {
    if (e instanceof SyntaxError || e?.message === 'metric derivation config must be a JSON object') {
      ElMessage.error(t('standard.metric.derivationConfigInvalid'))
      return
    }
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    saving.value = false
  }
}

const openCreateDialog = () => {
  form.value = { name: '', code: '', type: filterType.value || 'atomic', definition: '', formula: '', category_id: selectedCategoryID.value, domain_id: null, base_metric_id: null }
  derivationConfigText.value = ''
  showCreateDialog.value = true
}

const openDetail = (row) => {
  navigateStandardRoute(router, { path: `/metrics/${row.id}`, query: route.query })
}

const approveMetric = async (row) => {
  await runLocked(`metric:${row.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.metric.confirmApprove'), t('standard.common.hint'), { type: 'info' })
      await metricAPI.approve(row.id, row.version)
      ElMessage.success(t('standard.common.approveSuccess'))
      await loadMetrics()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.approveFailed'))
    }
  })
}

const deleteMetric = async (row) => {
  await runLocked(`metric:${row.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.metric.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
      await metricAPI.delete(row.id)
      ElMessage.success(t('standard.common.deleteSuccess'))
      await loadMetrics()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
    }
  })
}

const createCategory = async () => {
  if (saving.value) return
  saving.value = true
  try {
    await metricCategoryAPI.create(categoryForm.value)
    ElMessage.success(t('standard.common.createSuccess'))
    categoryForm.value = { name: '', code: '', parent_id: null }
    await loadCategories()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    saving.value = false
  }
}

const addSubCategory = (parentId) => {
  categoryForm.value = { name: '', code: '', parent_id: parentId }
}

const deleteCategory = async (data) => {
  await runLocked(`metric-category:${data.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.metric.confirmDeleteCategory', { name: data.name }), t('standard.common.hint'), { type: 'warning' })
      await metricCategoryAPI.delete(data.id)
      ElMessage.success(t('standard.common.deleteSuccess'))
      await loadCategories()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
    }
  })
}

onMounted(async () => {
  await loadCategories()
  await loadDomains()
  if (selectedCategoryID.value && !categories.value.some(category => category.id === selectedCategoryID.value)) {
    selectedCategoryID.value = null
    syncQuery()
  }
  await Promise.all([loadMetrics(), loadAtomicMetrics()])
})
</script>

<style scoped>
.metric-list { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }

.derivation-config-input :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  line-height: 1.5;
}
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.header-left { display: flex; align-items: center; gap: 16px; }
.header-left h2 { margin: 0; font-size: 18px; color: var(--addp-text-primary); }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.metric-list :deep(.el-card) { background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }
.category-item { padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 13px; color: var(--addp-text-primary); }
.category-item.active, .cat-node.active { color: var(--el-color-primary); font-weight: 500; }
.category-item:hover { background: var(--addp-bg-secondary); }
.cat-node { padding: 2px 0; font-size: 13px; }
.toolbar { display: flex; gap: 10px; margin-bottom: 16px; }
.pagination { margin-top: 16px; display: flex; justify-content: flex-end; }
.table-actions { display: inline-flex; align-items: center; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }
.tree-node { display: flex; align-items: center; gap: 8px; min-width: 0; width: 100%; }
.tree-name, .tree-code { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-name { flex: 1 1 auto; }
.tree-code { flex: 0 1 auto; font-size: 12px; color: var(--addp-text-secondary); }
.tree-actions { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; min-width: max-content; white-space: nowrap; }
.category-manage { max-height: 400px; overflow-y: auto; }

@media (max-width: 768px) {
  .metric-list { padding: 12px; }
  .page-header, .header-left { align-items: flex-start; flex-wrap: wrap; }
  .metric-list :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .metric-list :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .metric-list :deep(.el-col + .el-col) { margin-top: 12px; }
  .toolbar { flex-wrap: wrap; }
  .toolbar :deep(.el-input) { width: 100% !important; }
}
</style>
