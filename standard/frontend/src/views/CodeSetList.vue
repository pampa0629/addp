<template>
  <div class="code-set-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="6">
          <el-input v-model="searchForm.keyword" placeholder="搜索编码/名称" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.type" placeholder="类型" clearable @change="handleSearch" style="width:100%">
            <el-option label="系统内置" value="system" />
            <el-option label="自定义" value="custom" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            新建码值集
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 码值集列表 -->
    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="codeSets" v-loading="loading" stripe>
        <el-table-column label="编码" prop="code" width="180" />
        <el-table-column label="名称" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="row.type === 'system' ? 'success' : 'info'" size="small">
              {{ row.type === 'system' ? '系统' : '自定义' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="描述" prop="description" show-overflow-tooltip />
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">编辑</el-button>
            <el-popconfirm
              title="确定删除该码值集吗？"
              @confirm="handleDelete(row.id)"
              :disabled="row.type === 'system'"
            >
              <template #reference>
                <el-button link type="danger" :disabled="row.type === 'system'">删除</el-button>
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
          @change="loadCodeSets"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" title="新建码值集" width="540px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="100px">
        <el-form-item label="编码" prop="code">
          <el-input v-model="createForm.code" placeholder="如：gender, status" />
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="createForm.name" placeholder="如：性别、状态" />
        </el-form-item>
        <el-form-item label="类型" prop="type">
          <el-select v-model="createForm.type" style="width:100%">
            <el-option label="自定义" value="custom" />
            <el-option label="系统内置" value="system" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">创建并编辑</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { codeSetAPI } from '../api/standard'

const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const codeSets = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', type: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const createForm = reactive({ code: '', name: '', type: 'custom', description: '' })
const createRules = {
  code: [{ required: true, message: '请输入编码', trigger: 'blur' }],
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择类型', trigger: 'change' }]
}

const loadCodeSets = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      type: searchForm.type || undefined
    }
    const res = await codeSetAPI.list(params)
    codeSets.value = res.data || []
    pagination.total = res.total || 0
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
  loadCodeSets()
}

const openCreateDialog = () => {
  Object.assign(createForm, { code: '', name: '', type: 'custom', description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await codeSetAPI.create(createForm)
    ElMessage.success('创建成功')
    createDialogVisible.value = false
    router.push(`/standard/code-sets/${res.id}`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await codeSetAPI.delete(id)
    ElMessage.success('删除成功')
    loadCodeSets()
  } catch {
    ElMessage.error('删除失败')
  }
}

const goToDetail = (row) => {
  router.push(`/standard/code-sets/${row.id}`)
}

onMounted(() => {
  loadCodeSets()
})
</script>

<style scoped>
.code-set-list {
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
