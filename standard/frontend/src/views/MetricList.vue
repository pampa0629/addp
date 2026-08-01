<template>
  <div class="metric-list">
    <div class="page-header">
      <div class="header-left">
        <h2>{{ $t('standard.metric.title') }}</h2>
        <el-radio-group v-model="filterType" size="small" @change="loadMetrics">
          <el-radio-button value="">{{ $t('standard.metric.all') }}</el-radio-button>
          <el-radio-button value="atomic">{{ $t('standard.metric.atomic') }}</el-radio-button>
          <el-radio-button value="derived">{{ $t('standard.metric.derived') }}</el-radio-button>
          <el-radio-button value="composite">{{ $t('standard.metric.composite') }}</el-radio-button>
        </el-radio-group>
      </div>
      <el-button type="primary" @click="showCreateDialog = true">{{ $t('standard.metric.create') }}</el-button>
    </div>

    <el-row :gutter="16">
      <!-- 左侧：指标目录 -->
      <el-col :span="5">
        <el-card class="category-card">
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.metric.categoryTitle') }}</span>
              <el-button link size="small" @click="showCategoryDialog = true">{{ $t('standard.metric.categoryManageBtn') }}</el-button>
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
            <el-input v-model="keyword" :placeholder="$t('standard.metric.searchPlaceholder')" clearable @change="loadMetrics" style="width:280px" />
            <el-select v-model="filterStatus" :placeholder="$t('standard.common.status')" clearable @change="loadMetrics" style="width:120px">
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
            <el-table-column :label="$t('standard.common.actions')" width="140" fixed="right">
              <template #default="{ row }">
                <el-button link size="small" @click.stop="openDetail(row)">{{ $t('standard.common.detail') }}</el-button>
                <el-button link size="small" type="success" v-if="row.status === 'draft'" @click.stop="approveMetric(row)">{{ $t('standard.common.approve') }}</el-button>
                <el-button link size="small" type="danger" @click.stop="deleteMetric(row)">{{ $t('standard.common.delete') }}</el-button>
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
              @change="loadMetrics"
            />
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增指标对话框 -->
    <el-dialog v-model="showCreateDialog" :title="$t('standard.metric.createTitle')" width="600px">
      <el-form :model="form" label-width="100px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.nameLabel')" required>
              <el-input v-model="form.name" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.codeLabel')" required>
              <el-input v-model="form.code" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item :label="$t('standard.metric.typeLabel')" required>
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
        <el-form-item :label="$t('standard.metric.definitionLabel')">
          <el-input v-model="form.definition" type="textarea" :rows="3" :placeholder="$t('standard.metric.definitionPlaceholder')" />
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
              <span>{{ data.name }}</span>
              <span class="tree-code">{{ data.code }}</span>
              <div class="tree-actions">
                <el-button link size="small" @click.stop="addSubCategory(data.id)">{{ $t('standard.metric.addSubCategory') }}</el-button>
                <el-button link size="small" type="danger" @click.stop="deleteCategory(data)">{{ $t('standard.common.delete') }}</el-button>
              </div>
            </div>
          </template>
        </el-tree>
        <el-divider />
        <el-form :model="categoryForm" label-width="80px" size="small">
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
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { metricAPI, metricCategoryAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const metrics = ref([])
const categories = ref([])
const atomicMetrics = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const keyword = ref('')
const filterType = ref('')
const filterStatus = ref('')
const selectedCategoryID = ref(null)

const showCreateDialog = ref(false)
const showCategoryDialog = ref(false)

const form = ref({ name: '', code: '', type: 'atomic', definition: '', formula: '', category_id: null, base_metric_id: null })
const categoryForm = ref({ name: '', code: '', parent_id: null })

const categoryTree = computed(() => buildTree(categories.value))
function buildTree(list, parentId = null) {
  return list.filter(i => (i.parent_id || null) === parentId).map(i => ({ ...i, children: buildTree(list, i.id) }))
}

const typeLabel = (type) => ({ atomic: t('standard.metric.atomicShort'), derived: t('standard.metric.derivedShort'), composite: t('standard.metric.compositeShort') }[type] || type)
const typeTagType = (type) => ({ atomic: 'primary', derived: 'warning', composite: 'success' }[type] || '')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)
const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || '')

const loadMetrics = async () => {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value, keyword: keyword.value, type: filterType.value, status: filterStatus.value }
    if (selectedCategoryID.value) params.category_id = selectedCategoryID.value
    const res = await metricAPI.list(params)
    metrics.value = res.data || []
    total.value = res.total || 0
  } finally {
    loading.value = false
  }
}

const loadCategories = async () => {
  const res = await metricCategoryAPI.list()
  categories.value = res || []
}

const loadAtomicMetrics = async () => {
  const res = await metricAPI.list({ type: 'atomic', page_size: 500 })
  atomicMetrics.value = res.data || []
}

const selectCategory = (id) => {
  selectedCategoryID.value = id
  page.value = 1
  loadMetrics()
}

const createMetric = async () => {
  saving.value = true
  try {
    await metricAPI.create(form.value)
    ElMessage.success(t('standard.common.createSuccess'))
    showCreateDialog.value = false
    form.value = { name: '', code: '', type: 'atomic', definition: '', formula: '', category_id: null, base_metric_id: null }
    loadMetrics()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    saving.value = false
  }
}

const openDetail = (row) => {
  navigateStandardRoute(router, `/metrics/${row.id}`)
}

const approveMetric = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.metric.confirmApprove'), t('standard.common.hint'), { type: 'info' })
    await metricAPI.approve(row.id)
    ElMessage.success(t('standard.common.approveSuccess'))
    loadMetrics()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.approveFailed'))
  }
}

const deleteMetric = async (row) => {
  try {
    await ElMessageBox.confirm(t('standard.metric.confirmDelete', { name: row.name }), t('standard.common.hint'), { type: 'warning' })
    await metricAPI.delete(row.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    loadMetrics()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.deleteFailed'))
  }
}

const createCategory = async () => {
  saving.value = true
  try {
    await metricCategoryAPI.create(categoryForm.value)
    ElMessage.success(t('standard.common.createSuccess'))
    categoryForm.value = { name: '', code: '', parent_id: null }
    await loadCategories()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    saving.value = false
  }
}

const addSubCategory = (parentId) => {
  categoryForm.value = { name: '', code: '', parent_id: parentId }
}

const deleteCategory = async (data) => {
  try {
    await ElMessageBox.confirm(t('standard.metric.confirmDeleteCategory', { name: data.name }), t('standard.common.hint'), { type: 'warning' })
    await metricCategoryAPI.delete(data.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    await loadCategories()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.deleteFailed'))
  }
}

onMounted(() => {
  loadCategories()
  loadMetrics()
  loadAtomicMetrics()
})
</script>

<style scoped>
.metric-list { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.header-left { display: flex; align-items: center; gap: 16px; }
.header-left h2 { margin: 0; font-size: 18px; color: var(--el-text-color-primary); }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.category-item { padding: 8px 12px; cursor: pointer; border-radius: 4px; font-size: 13px; color: var(--el-text-color-primary); }
.category-item.active, .cat-node.active { color: var(--el-color-primary); font-weight: 500; }
.category-item:hover { background: var(--el-fill-color-light); }
.cat-node { padding: 2px 0; font-size: 13px; }
.toolbar { display: flex; gap: 10px; margin-bottom: 16px; }
.pagination { margin-top: 16px; display: flex; justify-content: flex-end; }
.tree-node { display: flex; align-items: center; gap: 8px; width: 100%; }
.tree-code { font-size: 12px; color: var(--el-text-color-secondary); }
.tree-actions { margin-left: auto; }
.category-manage { max-height: 400px; overflow-y: auto; }
</style>
