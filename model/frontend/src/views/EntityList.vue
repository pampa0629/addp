<template>
  <div class="entity-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="6">
          <el-input v-model="searchForm.keyword" placeholder="搜索实体名称/编码" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="5">
          <el-select v-model="searchForm.domain_id" placeholder="业务域" clearable @change="handleSearch" style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.status" placeholder="状态" clearable @change="handleSearch" style="width:100%">
            <el-option label="草稿" value="draft" />
            <el-option label="已审批" value="approved" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            新建实体
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 实体列表 -->
    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="entities" v-loading="loading" stripe>
        <el-table-column label="实体名称" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="英文编码" prop="code" width="160" />
        <el-table-column label="所属业务域" width="140">
          <template #default="{ row }">
            {{ getDomainName(row.domain_id) || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'approved' ? 'success' : 'info'" size="small">
              {{ row.status === 'approved' ? '已审批' : '草稿' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">设计</el-button>
            <el-popconfirm title="确定删除该实体吗？" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger">删除</el-button>
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
          @change="loadEntities"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" title="新建业务实体" width="480px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="90px">
        <el-form-item label="实体名称" prop="name">
          <el-input v-model="createForm.name" placeholder="如：客户、订单" />
        </el-form-item>
        <el-form-item label="英文编码" prop="code">
          <el-input v-model="createForm.code" placeholder="如：customer、order" />
        </el-form-item>
        <el-form-item label="业务域">
          <el-select v-model="createForm.domain_id" placeholder="请选择" clearable style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">创建并进入详情</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { entityAPI, domainAPI } from '../api/model'

const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const entities = ref([])
const domains = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', domain_id: null, status: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })

const createForm = reactive({ name: '', code: '', domain_id: null, description: '' })
const createRules = {
  name: [{ required: true, message: '请输入实体名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入英文编码', trigger: 'blur' }]
}

const getDomainName = (id) => domains.value.find(d => d.id === id)?.name

const loadEntities = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      domain_id: searchForm.domain_id || undefined,
      status: searchForm.status || undefined
    }
    const res = await entityAPI.list(params)
    entities.value = res.data || []
    pagination.total = res.total || 0
  } finally {
    loading.value = false
  }
}

const loadDomains = async () => {
  const res = await domainAPI.list()
  domains.value = res.data || []
}

const handleSearch = () => {
  pagination.page = 1
  loadEntities()
}

const openCreateDialog = () => {
  Object.assign(createForm, { name: '', code: '', domain_id: null, description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await entityAPI.create(createForm)
    ElMessage.success('创建成功')
    createDialogVisible.value = false
    router.push(`/modeling/entities/${res.id}`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await entityAPI.delete(id)
    ElMessage.success('删除成功')
    loadEntities()
  } catch {
    ElMessage.error('删除失败')
  }
}

const goToDetail = (row) => {
  router.push(`/modeling/entities/${row.id}`)
}

onMounted(() => {
  loadDomains()
  loadEntities()
})
</script>

<style scoped>
.entity-list {
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
