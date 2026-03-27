<template>
  <div class="element-list">
    <div class="page-header">
      <h2>数据元管理</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新建数据元</el-button>
    </div>

    <!-- 筛选栏 -->
    <el-card class="filter-card">
      <el-row :gutter="12">
        <el-col :span="8">
          <el-input
            v-model="filters.keyword"
            placeholder="搜索名称、编码或定义"
            clearable
            @change="loadElements"
            :prefix-icon="Search"
          />
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.domain_id" placeholder="选择业务域" clearable @change="loadElements">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-col>
        <el-col :span="6">
          <el-select v-model="filters.status" placeholder="选择状态" clearable @change="loadElements">
            <el-option label="草稿" value="draft" />
            <el-option label="已审批" value="approved" />
            <el-option label="已废弃" value="deprecated" />
          </el-select>
        </el-col>
      </el-row>
    </el-card>

    <!-- 列表 -->
    <el-card class="table-card">
      <el-table :data="elements" v-loading="loading" stripe>
        <el-table-column label="名称" min-width="140">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="英文编码" prop="code" width="150" />
        <el-table-column label="数据类型" width="100">
          <template #default="{ row }">
            <el-tag size="small" type="info">{{ row.data_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="所属业务域" width="120">
          <template #default="{ row }">{{ getDomainName(row.domain_id) || '-' }}</template>
        </el-table-column>
        <el-table-column label="质量规则" width="100" align="center">
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
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">详情</el-button>
            <el-button link type="success" @click="handleApprove(row)" v-if="row.status === 'draft'">审批</el-button>
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

    <!-- 新建对话框 -->
    <el-dialog v-model="dialogVisible" title="新建数据元" width="600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item label="中文名称" prop="name">
          <el-input v-model="form.name" placeholder="如：手机号码" />
        </el-form-item>
        <el-form-item label="英文编码" prop="code">
          <el-input v-model="form.code" placeholder="如：mobile_phone（唯一）" />
        </el-form-item>
        <el-form-item label="数据类型" prop="data_type">
          <el-select v-model="form.data_type" style="width: 100%">
            <el-option label="字符串 (string)" value="string" />
            <el-option label="整数 (int)" value="int" />
            <el-option label="浮点数 (float)" value="float" />
            <el-option label="日期时间 (date)" value="date" />
            <el-option label="布尔 (bool)" value="bool" />
            <el-option label="JSON (json)" value="json" />
          </el-select>
        </el-form-item>
        <el-form-item label="长度" v-if="['string'].includes(form.data_type)">
          <el-input-number v-model="form.length" :min="1" />
        </el-form-item>
        <el-form-item label="允许为空">
          <el-switch v-model="form.nullable" />
        </el-form-item>
        <el-form-item label="所属业务域">
          <el-select v-model="form.domain_id" placeholder="选择业务域（可选）" clearable style="width: 100%">
            <el-option v-for="d in domainList" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="计量单位">
          <el-select v-model="form.unit_id" clearable filterable placeholder="选择单位（可选）" style="width: 100%">
            <el-option-group v-for="cat in unitsByCategory" :key="cat.id" :label="cat.name">
              <el-option v-for="u in cat.units" :key="u.id" :label="`${u.name}（${u.symbol}）`" :value="u.id" />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item label="业务含义">
          <el-input v-model="form.definition" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search, Checked } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, elementAPI, unitAPI } from '../api/standard'

const router = useRouter()
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
    const catName = u.category?.name || '其他'
    if (!map[catId]) map[catId] = { id: catId, name: catName, units: [] }
    map[catId].units.push(u)
  })
  return Object.values(map)
})

const filters = reactive({
  keyword: '',
  domain_id: null,
  status: '',
  page: 1,
  page_size: 20
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

const rules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入英文编码', trigger: 'blur' }],
  data_type: [{ required: true, message: '请选择数据类型', trigger: 'change' }]
}

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)

const getRuleCount = (qr) => {
  if (!qr || !qr.rules) return 0
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
  const res = await domainAPI.list()
  domainList.value = flattenDomains(res || [])
}

const loadUnits = async () => {
  try {
    const res = await unitAPI.list({ page_size: 500 })
    units.value = res || []
  } catch (e) {
    console.error('加载单位失败:', e)
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
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handlePageChange = (page) => {
  filters.page = page
  loadElements()
}

const goToDetail = (row) => {
  router.push(`/standard/elements/${row.id}`)
}

const openCreateDialog = () => {
  form.value = { name: '', code: '', data_type: 'string', length: null, nullable: true, domain_id: null, unit_id: null, definition: '' }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async valid => {
    if (!valid) return
    submitting.value = true
    try {
      await elementAPI.create(form.value)
      ElMessage.success('创建成功')
      dialogVisible.value = false
      await loadElements()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || '创建失败')
    } finally {
      submitting.value = false
    }
  })
}

const handleApprove = async (row) => {
  try {
    await elementAPI.approve(row.id)
    ElMessage.success('审批成功')
    await loadElements()
  } catch (e) {
    ElMessage.error('审批失败')
  }
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除数据元「${row.name}」？`, '提示', { type: 'warning' })
    await elementAPI.delete(row.id)
    ElMessage.success('删除成功')
    await loadElements()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
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
</style>
