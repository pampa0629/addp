<template>
  <div class="domain-list">
    <div class="page-header">
      <h2>{{ $t('standard.domain.title') }}</h2>
      <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreateDialog">{{ $t('standard.domain.create') }}</el-button>
    </div>

    <el-card class="main-card">
      <el-tree
        v-if="loading || domainTree.length > 0"
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
              <el-button v-if="canCreate" link type="primary" @click.stop="openCreateChildDialog(data)">{{ $t('standard.domain.createChild') }}</el-button>
              <el-button v-if="canUpdate" link type="primary" @click.stop="openEditDialog(data)">{{ $t('standard.common.edit') }}</el-button>
              <el-button v-if="canDelete" link type="danger" :loading="isActionLocked(`domain:${data.id}`)" @click.stop="handleDelete(data)">{{ $t('standard.common.delete') }}</el-button>
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
      @opened="focusNameInput"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="$t('standard.domain.nameLabel')" prop="name">
          <el-input ref="nameInputRef" v-model="form.name" :placeholder="$t('standard.domain.namePlaceholder')" />
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
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useActionLock } from '../composables/useActionLock'

const { t } = useI18n()
const { canCreate, canUpdate, canDelete } = useStandardPermissions('domain')
const { isLocked: isActionLocked, runLocked } = useActionLock()

const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const domainTree = ref([])
const parentDomain = ref(null)
const editingId = ref(null)
const formRef = ref(null)
const nameInputRef = ref(null)

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
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.domain.loadFailed'))
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
    parent_id: domain.parent_id || null,
    version: domain.version
  }
  dialogVisible.value = true
}

const focusNameInput = () => {
  nameInputRef.value?.focus()
}

const handleSubmit = async () => {
  if (submitting.value || !formRef.value) return
  submitting.value = true
  try {
    const valid = await formRef.value.validate().catch(() => false)
    if (!valid) return
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
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    submitting.value = false
  }
}

const handleDelete = async (domain) => {
  await runLocked(`domain:${domain.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.domain.confirmDelete', { name: domain.name }), t('standard.common.hint'), {
        type: 'warning'
      })
      await domainAPI.delete(domain.id, domain.version)
      ElMessage.success(t('standard.common.deleteSuccess'))
      await loadDomains()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
    }
  })
}

onMounted(loadDomains)
</script>

<style scoped>
.domain-list {
  min-height: 100%;
  padding: 20px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
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
  color: var(--addp-text-primary);
}

.main-card {
  min-height: 400px;
  background: var(--addp-bg-primary);
  border-color: var(--addp-border-color);
  box-shadow: var(--addp-shadow-card);
}

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  width: 100%;
  padding: 4px 0;
}

.node-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  overflow: hidden;
}

.node-icon {
  color: var(--el-color-primary);
}

.node-name {
  font-size: 14px;
  color: var(--addp-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-code {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  font-size: 12px;
  text-overflow: ellipsis;
}

.node-actions {
  display: flex;
  gap: 4px;
  flex: 0 0 auto;
  white-space: nowrap;
}

</style>
