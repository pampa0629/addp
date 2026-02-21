<template>
  <div class="glossary-list">
    <div class="page-header">
      <h2>业务术语词典</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新建术语</el-button>
    </div>

    <!-- 筛选栏 -->
    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="8">
          <el-input
            v-model="filters.keyword"
            placeholder="搜索术语名称或定义"
            clearable
            @change="loadGlossaries"
            :prefix-icon="Search"
          />
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.domain_id" placeholder="选择业务域" clearable @change="loadGlossaries">
            <el-option
              v-for="domain in domainList"
              :key="domain.id"
              :label="domain.name"
              :value="domain.id"
            />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.status" placeholder="选择状态" clearable @change="loadGlossaries">
            <el-option label="草稿" value="draft" />
            <el-option label="已审批" value="approved" />
            <el-option label="已废弃" value="deprecated" />
          </el-select>
        </el-col>
      </el-row>
    </el-card>

    <!-- 列表 -->
    <el-card class="table-card">
      <el-table :data="glossaries" v-loading="loading" stripe>
        <el-table-column label="术语名称" min-width="150">
          <template #default="{ row }">
            <span class="term-name">{{ row.name }}</span>
            <div v-if="row.alias && row.alias.length > 0" class="alias-list">
              <el-tag v-for="a in row.alias" :key="a" size="small" type="info" class="alias-tag">{{ a }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="所属业务域" width="120">
          <template #default="{ row }">
            <span>{{ getDomainName(row.domain_id) || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="业务定义" prop="definition" show-overflow-tooltip />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="标签" width="180">
          <template #default="{ row }">
            <el-tag v-for="tag in (row.tags || [])" :key="tag" size="small" class="tag-item">{{ tag }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">详情</el-button>
            <el-button link type="success" @click="handleApprove(row)" v-if="row.status === 'draft'">审批</el-button>
            <el-button link type="warning" @click="handleDeprecate(row)" v-if="row.status === 'approved'">废弃</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
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
    <el-dialog v-model="dialogVisible" :title="editMode ? '编辑业务术语' : '新建业务术语'" width="600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="术语名称" prop="name">
          <el-input v-model="form.name" placeholder="如：客户" />
        </el-form-item>
        <el-form-item label="别名">
          <el-select
            v-model="form.alias"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="输入后回车添加别名"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="所属业务域">
          <el-select v-model="form.domain_id" placeholder="选择业务域（可选）" clearable style="width: 100%">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="业务定义" prop="definition">
          <el-input v-model="form.definition" type="textarea" :rows="4" placeholder="请输入业务定义" />
        </el-form-item>
        <el-form-item label="使用示例">
          <el-input v-model="form.example" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.note" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="form.tags"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="输入后回车添加标签"
            style="width: 100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, glossaryAPI } from '../api/standard'

const router = useRouter()

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

const form = ref({
  name: '',
  alias: [],
  domain_id: null,
  definition: '',
  example: '',
  note: '',
  tags: []
})

const rules = {
  name: [{ required: true, message: '请输入术语名称', trigger: 'blur' }],
  definition: [{ required: true, message: '请输入业务定义', trigger: 'blur' }]
}

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)

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
  domainList.value = flattenDomains(res.data || [])
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
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handlePageChange = (page) => {
  filters.page = page
  loadGlossaries()
}

const openCreateDialog = () => {
  editMode.value = false
  editingId.value = null
  form.value = { name: '', alias: [], domain_id: null, definition: '', example: '', note: '', tags: [] }
  dialogVisible.value = true
}

const goToDetail = (row) => {
  router.push(`/standard/glossaries/${row.id}`)
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
        ElMessage.success('更新成功')
      } else {
        await glossaryAPI.create(form.value)
        ElMessage.success('创建成功')
      }
      dialogVisible.value = false
      await loadGlossaries()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || '操作失败')
    } finally {
      submitting.value = false
    }
  })
}

const handleApprove = async (row) => {
  try {
    await glossaryAPI.approve(row.id)
    ElMessage.success('审批成功')
    await loadGlossaries()
  } catch (e) {
    ElMessage.error('审批失败')
  }
}

const handleDeprecate = async (row) => {
  try {
    await ElMessageBox.confirm(`确认废弃术语「${row.name}」？`, '提示', { type: 'warning' })
    await glossaryAPI.deprecate(row.id)
    ElMessage.success('已废弃')
    await loadGlossaries()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除术语「${row.name}」？`, '提示', { type: 'warning' })
    await glossaryAPI.delete(row.id)
    ElMessage.success('删除成功')
    await loadGlossaries()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

onMounted(async () => {
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
