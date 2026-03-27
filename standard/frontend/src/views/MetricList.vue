<template>
  <div class="metric-list">
    <div class="page-header">
      <div class="header-left">
        <h2>指标管理</h2>
        <el-radio-group v-model="filterType" size="small" @change="loadMetrics">
          <el-radio-button value="">全部</el-radio-button>
          <el-radio-button value="atomic">原子指标</el-radio-button>
          <el-radio-button value="derived">派生指标</el-radio-button>
          <el-radio-button value="composite">复合指标</el-radio-button>
        </el-radio-group>
      </div>
      <el-button type="primary" @click="showCreateDialog = true">新增指标</el-button>
    </div>

    <el-row :gutter="16">
      <!-- 左侧：指标目录 -->
      <el-col :span="5">
        <el-card class="category-card">
          <template #header>
            <div class="card-header">
              <span>指标目录</span>
              <el-button link size="small" @click="showCategoryDialog = true">管理</el-button>
            </div>
          </template>
          <div class="category-item" :class="{ active: !selectedCategoryID }" @click="selectCategory(null)">
            全部指标
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
            <el-input v-model="keyword" placeholder="搜索指标名称/编码/口径" clearable @change="loadMetrics" style="width:280px" />
            <el-select v-model="filterStatus" placeholder="状态" clearable @change="loadMetrics" style="width:120px">
              <el-option label="草稿" value="draft" />
              <el-option label="已审批" value="approved" />
              <el-option label="已废弃" value="deprecated" />
            </el-select>
          </div>

          <el-table :data="metrics" v-loading="loading" size="small" @row-click="openDetail">
            <el-table-column label="指标名称" prop="name" min-width="140" />
            <el-table-column label="编码" prop="code" width="140" />
            <el-table-column label="类型" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="typeTagType(row.type)">{{ typeLabel(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="业务口径" prop="definition" show-overflow-tooltip min-width="200" />
            <el-table-column label="状态" width="85">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="140" fixed="right">
              <template #default="{ row }">
                <el-button link size="small" @click.stop="openDetail(row)">详情</el-button>
                <el-button link size="small" type="success" v-if="row.status === 'draft'" @click.stop="approveMetric(row)">审批</el-button>
                <el-button link size="small" type="danger" @click.stop="deleteMetric(row)">删除</el-button>
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
    <el-dialog v-model="showCreateDialog" title="新增指标" width="600px">
      <el-form :model="form" label-width="100px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="指标名称" required>
              <el-input v-model="form.name" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="英文编码" required>
              <el-input v-model="form.code" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="指标类型" required>
              <el-select v-model="form.type" style="width:100%">
                <el-option label="原子指标" value="atomic" />
                <el-option label="派生指标" value="derived" />
                <el-option label="复合指标" value="composite" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="所属目录">
              <el-tree-select
                v-model="form.category_id"
                :data="categoryTree"
                :props="{ label: 'name', value: 'id', children: 'children' }"
                clearable style="width:100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="业务口径">
          <el-input v-model="form.definition" type="textarea" :rows="3" placeholder="描述指标的业务含义和计算口径" />
        </el-form-item>
        <el-form-item label="计算公式" v-if="form.type === 'composite'">
          <el-input v-model="form.formula" type="textarea" :rows="2" placeholder="如：metric_a / metric_b × 100%" />
        </el-form-item>
        <el-form-item label="基准指标" v-if="form.type === 'derived'">
          <el-select v-model="form.base_metric_id" filterable clearable style="width:100%" placeholder="选择基准原子指标">
            <el-option v-for="m in atomicMetrics" :key="m.id" :label="`${m.name}（${m.code}）`" :value="m.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="createMetric" :loading="saving">创建</el-button>
      </template>
    </el-dialog>

    <!-- 指标目录管理对话框 -->
    <el-dialog v-model="showCategoryDialog" title="指标目录管理" width="500px">
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
                <el-button link size="small" @click.stop="addSubCategory(data.id)">子级</el-button>
                <el-button link size="small" type="danger" @click.stop="deleteCategory(data)">删除</el-button>
              </div>
            </div>
          </template>
        </el-tree>
        <el-divider />
        <el-form :model="categoryForm" label-width="80px" size="small">
          <el-row :gutter="12">
            <el-col :span="12">
              <el-form-item label="名称">
                <el-input v-model="categoryForm.name" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="编码">
                <el-input v-model="categoryForm.code" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item label="上级目录">
            <el-tree-select
              v-model="categoryForm.parent_id"
              :data="categoryTree"
              :props="{ label: 'name', value: 'id', children: 'children' }"
              clearable placeholder="不选则为顶级" style="width:100%"
            />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="createCategory" :loading="saving">新增目录</el-button>
          </el-form-item>
        </el-form>
      </div>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { metricAPI, metricCategoryAPI } from '../api/standard'

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

const typeLabel = (t) => ({ atomic: '原子', derived: '派生', composite: '复合' }[t] || t)
const typeTagType = (t) => ({ atomic: 'primary', derived: 'warning', composite: 'success' }[t] || '')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)
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
    ElMessage.success('创建成功')
    showCreateDialog.value = false
    form.value = { name: '', code: '', type: 'atomic', definition: '', formula: '', category_id: null, base_metric_id: null }
    loadMetrics()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  } finally {
    saving.value = false
  }
}

const openDetail = (row) => {
  router.push(`/standard/metrics/${row.id}`)
}

const approveMetric = async (row) => {
  try {
    await ElMessageBox.confirm('确认审批通过？', '提示', { type: 'info' })
    await metricAPI.approve(row.id)
    ElMessage.success('审批成功')
    loadMetrics()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('审批失败')
  }
}

const deleteMetric = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除指标"${row.name}"？`, '提示', { type: 'warning' })
    await metricAPI.delete(row.id)
    ElMessage.success('删除成功')
    loadMetrics()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

const createCategory = async () => {
  saving.value = true
  try {
    await metricCategoryAPI.create(categoryForm.value)
    ElMessage.success('创建成功')
    categoryForm.value = { name: '', code: '', parent_id: null }
    await loadCategories()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  } finally {
    saving.value = false
  }
}

const addSubCategory = (parentId) => {
  categoryForm.value = { name: '', code: '', parent_id: parentId }
}

const deleteCategory = async (data) => {
  try {
    await ElMessageBox.confirm(`确认删除目录"${data.name}"？`, '提示', { type: 'warning' })
    await metricCategoryAPI.delete(data.id)
    ElMessage.success('删除成功')
    await loadCategories()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
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
.category-card { height: 100%; }
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
