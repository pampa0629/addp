<template>
  <div class="dim-hierarchy-list">
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="8">
          <el-input v-model="keyword" placeholder="搜索层级名称/编码" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            新建维度层级
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="filteredList" v-loading="loading" stripe>
        <el-table-column label="层级名称" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="$router.push(`/standard/dimension-hierarchies/${row.id}`)">
              {{ row.name }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column label="编码" prop="code" width="140" />
        <el-table-column label="层数" width="80">
          <template #default="{ row }">
            <el-tag type="info" size="small">{{ row.levels?.length ?? 0 }} 层</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="描述" prop="description" min-width="200" show-overflow-tooltip />
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="$router.push(`/standard/dimension-hierarchies/${row.id}`)">
              管理层级
            </el-button>
            <el-popconfirm title="确定删除该维度层级吗？" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createVisible" title="新建维度层级" width="480px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="90px">
        <el-form-item label="层级名称" prop="name">
          <el-input v-model="createForm.name" placeholder="如：时间层级、地区层级" />
        </el-form-item>
        <el-form-item label="编码" prop="code">
          <el-input v-model="createForm.code" placeholder="如：time_hierarchy" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">创建并编辑层级</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI } from '../api/standard'

const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const list = ref([])
const keyword = ref('')
const createVisible = ref(false)
const createFormRef = ref(null)

const createForm = reactive({ name: '', code: '', description: '' })
const createRules = {
  name: [{ required: true, message: '请填写层级名称', trigger: 'blur' }],
  code: [{ required: true, message: '请填写编码', trigger: 'blur' }]
}

const filteredList = computed(() => {
  if (!keyword.value) return list.value
  const kw = keyword.value.toLowerCase()
  return list.value.filter(h => h.name.toLowerCase().includes(kw) || h.code.toLowerCase().includes(kw))
})

async function loadList() {
  loading.value = true
  try {
    const res = await dimensionHierarchyAPI.list()
    list.value = res.data || []
  } catch {
    ElMessage.error('加载失败')
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
    router.push(`/standard/dimension-hierarchies/${res.data.id}`)
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

async function handleDelete(id) {
  try {
    await dimensionHierarchyAPI.delete(id)
    ElMessage.success('已删除')
    loadList()
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(loadList)
</script>

<style scoped>
.search-card { margin-bottom: 0; }
</style>
