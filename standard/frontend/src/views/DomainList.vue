<template>
  <div class="domain-list">
    <div class="page-header">
      <h2>业务域管理</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新建业务域</el-button>
    </div>

    <el-card class="main-card">
      <el-tree
        :data="domainTree"
        :props="{ label: 'name', children: 'children' }"
        default-expand-all
        node-key="id"
        v-loading="loading"
      >
        <template #default="{ node, data }">
          <div class="tree-node">
            <div class="node-left">
              <el-icon v-if="data.icon" class="node-icon">
                <component :is="data.icon" />
              </el-icon>
              <span class="node-name">{{ data.name }}</span>
              <el-tag size="small" type="info" class="node-code">{{ data.code }}</el-tag>
            </div>
            <div class="node-actions">
              <el-button link type="primary" @click.stop="openCreateChildDialog(data)">添加子域</el-button>
              <el-button link type="primary" @click.stop="openEditDialog(data)">编辑</el-button>
              <el-button link type="danger" @click.stop="handleDelete(data)">删除</el-button>
            </div>
          </div>
        </template>
      </el-tree>

      <el-empty v-if="!loading && domainTree.length === 0" description="暂无业务域，请点击右上角创建" />
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editMode ? '编辑业务域' : (parentDomain ? `新建子域（${parentDomain.name}）` : '新建业务域')"
      width="500px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：客户域" />
        </el-form-item>
        <el-form-item label="英文编码" prop="code" v-if="!editMode">
          <el-input v-model="form.code" placeholder="如：customer（唯一标识）" />
        </el-form-item>
        <el-form-item label="图标">
          <el-input v-model="form.icon" placeholder="Element Plus 图标名（可选）" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="3" />
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
import { ref, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI } from '../api/standard'

const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const domainTree = ref([])
const parentDomain = ref(null)
const editingId = ref(null)
const formRef = ref(null)

const form = ref({
  name: '',
  code: '',
  description: '',
  icon: '',
  sort_order: 0,
  parent_id: null
})

const rules = {
  name: [{ required: true, message: '请输入业务域名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入英文编码', trigger: 'blur' }]
}

const loadDomains = async () => {
  loading.value = true
  try {
    const res = await domainAPI.list()
    domainTree.value = res || []
  } catch (e) {
    ElMessage.error('加载业务域失败')
  } finally {
    loading.value = false
  }
}

const openCreateDialog = () => {
  editMode.value = false
  parentDomain.value = null
  editingId.value = null
  form.value = { name: '', code: '', description: '', icon: '', sort_order: 0, parent_id: null }
  dialogVisible.value = true
}

const openCreateChildDialog = (parent) => {
  editMode.value = false
  parentDomain.value = parent
  editingId.value = null
  form.value = { name: '', code: '', description: '', icon: '', sort_order: 0, parent_id: parent.id }
  dialogVisible.value = true
}

const openEditDialog = (domain) => {
  editMode.value = true
  editingId.value = domain.id
  parentDomain.value = null
  form.value = {
    name: domain.name,
    code: domain.code,
    description: domain.description || '',
    icon: domain.icon || '',
    sort_order: domain.sort_order || 0,
    parent_id: domain.parent_id || null
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
        await domainAPI.update(editingId.value, form.value)
        ElMessage.success('更新成功')
      } else {
        await domainAPI.create(form.value)
        ElMessage.success('创建成功')
      }
      dialogVisible.value = false
      await loadDomains()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || '操作失败')
    } finally {
      submitting.value = false
    }
  })
}

const handleDelete = async (domain) => {
  try {
    await ElMessageBox.confirm(`确认删除业务域「${domain.name}」？`, '提示', {
      type: 'warning'
    })
    await domainAPI.delete(domain.id)
    ElMessage.success('删除成功')
    await loadDomains()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

onMounted(loadDomains)
</script>

<style scoped>
.domain-list {
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

.main-card {
  min-height: 400px;
}

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 4px 0;
}

.node-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.node-icon {
  color: var(--el-color-primary);
}

.node-name {
  font-size: 14px;
  color: var(--el-text-color-primary);
}

.node-code {
  font-size: 12px;
}

.node-actions {
  display: flex;
  gap: 4px;
}
</style>
