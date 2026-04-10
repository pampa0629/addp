<template>
  <div class="domain-list">
    <div class="page-header">
      <h2>{{ $t('standard.domain.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.domain.create') }}</el-button>
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
              <el-button link type="primary" @click.stop="openCreateChildDialog(data)">{{ $t('standard.domain.createChild') }}</el-button>
              <el-button link type="primary" @click.stop="openEditDialog(data)">{{ $t('standard.common.edit') }}</el-button>
              <el-button link type="danger" @click.stop="handleDelete(data)">{{ $t('standard.common.delete') }}</el-button>
            </div>
          </div>
        </template>
      </el-tree>

      <el-empty v-if="!loading && domainTree.length === 0" :description="$t('standard.domain.empty')" />
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editMode ? $t('standard.domain.editTitle') : (parentDomain ? $t('standard.domain.createChildTitle', { name: parentDomain.name }) : $t('standard.domain.create'))"
      width="500px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="$t('standard.domain.nameLabel')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('standard.domain.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.domain.codeLabel')" prop="code" v-if="!editMode">
          <el-input v-model="form.code" :placeholder="$t('standard.domain.codePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.domain.iconLabel')">
          <el-input v-model="form.icon" :placeholder="$t('standard.domain.iconPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.domain.sortLabel')">
          <el-input-number v-model="form.sort_order" :min="0" />
        </el-form-item>
        <el-form-item :label="$t('standard.domain.descriptionLabel')">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">{{ $t('standard.common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { domainAPI } from '../api/standard'

const { t } = useI18n()

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

const rules = computed(() => ({
  name: [{ required: true, message: t('standard.domain.nameRequired'), trigger: 'blur' }],
  code: [{ required: true, message: t('standard.domain.codeRequired'), trigger: 'blur' }]
}))

const loadDomains = async () => {
  loading.value = true
  try {
    const res = await domainAPI.list()
    domainTree.value = res || []
  } catch (e) {
    ElMessage.error(t('standard.domain.loadFailed'))
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
        ElMessage.success(t('standard.common.updateSuccess'))
      } else {
        await domainAPI.create(form.value)
        ElMessage.success(t('standard.common.createSuccess'))
      }
      dialogVisible.value = false
      await loadDomains()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
    } finally {
      submitting.value = false
    }
  })
}

const handleDelete = async (domain) => {
  try {
    await ElMessageBox.confirm(t('standard.domain.confirmDelete', { name: domain.name }), t('standard.common.hint'), {
      type: 'warning'
    })
    await domainAPI.delete(domain.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    await loadDomains()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.deleteFailed'))
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
