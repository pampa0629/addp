<template>
  <div class="code-set-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="goBack">
          <el-icon><ArrowLeft /></el-icon>
          {{ $t('standard.common.back') }}
        </el-button>
        <span class="code-set-name">{{ codeSet.name || '...' }}</span>
        <el-tag :type="codeSet.type === 'system' ? 'success' : 'info'" size="small">
          {{ codeSet.type === 'system' ? $t('standard.codeSet.typeSystem') : $t('standard.codeSet.typeCustom') }}
        </el-tag>
        <el-tag v-if="isDirty" type="warning" size="small">{{ $t('standard.common.unsaved') }}</el-tag>
      </div>
      <div class="header-right">
        <el-button v-if="canModifyCodeSet" type="primary" @click="handleSave" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">{{ $t('standard.codeSet.basicInfo') }}</span></template>
          <el-form :model="form" label-width="90px" :disabled="!canModifyCodeSet">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item :label="$t('standard.codeSet.codeLabel')">
                  <el-input :value="codeSet.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.codeSet.nameLabel')">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.common.type')">
                  <el-select v-model="form.type" style="width:100%">
                    <el-option :label="$t('standard.codeSet.typeCustom')" value="custom" />
                    <el-option :label="$t('standard.codeSet.typeSystem')" value="system" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="$t('standard.common.description')">
                  <el-input v-model="form.description" type="textarea" :rows="2" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>

      <!-- 码值项列表 -->
      <el-col :span="24" class="items-column">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">{{ $t('standard.codeSet.items') }}</span>
              <el-button v-if="canModifyCodeSet" type="primary" size="small" @click="openItemDialog()">
                <el-icon><Plus /></el-icon>
                {{ $t('standard.codeSet.addItem') }}
              </el-button>
            </div>
          </template>

          <el-table :data="items" v-loading="itemLoading" stripe>
            <el-table-column label="#" type="index" width="60" />
            <el-table-column :label="$t('standard.codeSet.itemCode')" prop="code" width="150" />
            <el-table-column :label="$t('standard.codeSet.itemValue')" prop="value" min-width="140" />
            <el-table-column :label="$t('standard.common.sort')" prop="sort_order" width="80" />
            <el-table-column :label="$t('standard.codeSet.itemActive')" width="80">
              <template #default="{ row }">
                <el-tag :type="row.is_active ? 'success' : 'info'" size="small">
                  {{ row.is_active ? $t('standard.unit.yes') : $t('standard.unit.no') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.description')" prop="description" show-overflow-tooltip />
            <el-table-column v-if="canModifyCodeSet" :label="$t('standard.common.actions')" width="150" fixed="right">
              <template #default="{ row }">
                <div class="table-actions">
                  <el-button link type="primary" @click="openItemDialog(row)">{{ $t('standard.common.edit') }}</el-button>
                  <el-button link type="danger" :loading="isActionLocked(`code-item:${row.id}`)" @click="deleteItem(row)">{{ $t('standard.common.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <!-- 码值项对话框 -->
    <el-dialog
      v-model="itemDialogVisible"
      :title="editingItem ? $t('standard.common.edit') + $t('standard.codeSet.items') : $t('standard.codeSet.addItem')"
      width="480px"
    >
      <el-form ref="itemFormRef" :model="itemForm" :rules="itemRules" label-width="100px">
        <el-form-item :label="$t('standard.codeSet.itemCode')" prop="code">
          <el-input v-model="itemForm.code" :placeholder="$t('standard.codeSet.itemCodePlaceholder')" :disabled="!!editingItem" />
        </el-form-item>
        <el-form-item :label="$t('standard.codeSet.itemValue')" prop="value">
          <el-input v-model="itemForm.value" :placeholder="$t('standard.codeSet.itemValuePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.sort')">
          <el-input-number v-model="itemForm.sort_order" :min="0" style="width:100%" />
        </el-form-item>
        <el-form-item :label="$t('standard.codeSet.itemActive')">
          <el-switch v-model="itemForm.is_active" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="itemForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="itemDialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleItemSubmit" :loading="itemSubmitting">
          {{ editingItem ? $t('standard.common.save') : $t('standard.codeSet.addItem') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { codeSetAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useActionLock } from '../composables/useActionLock'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'

const route = useRoute()
const router = useRouter()
const goBack = () => navigateStandardRoute(router, { path: '/code-sets', query: route.query }, { history: 'replace' })
const { t } = useI18n()
const { canUpdate } = useStandardPermissions('code_set')
const { isLocked: isActionLocked, runLocked } = useActionLock()
const canModifyCodeSet = computed(() => canUpdate.value && codeSet.value.type !== 'system')
const codeSetId = computed(() => Number(route.params.id))

const saving = ref(false)
const itemLoading = ref(false)
const itemSubmitting = ref(false)
const itemDialogVisible = ref(false)
const editingItem = ref(null)
const itemFormRef = ref(null)

const codeSet = ref({})
useConsolePageDescriptor(router, 'standard', {
  title: computed(() => t('standard.codeSet.recentVisitTitle')),
  subject: computed(() => codeSet.value?.name || ''),
  ready: computed(() => Boolean(codeSet.value?.name))
})
const form = reactive({ name: '', type: 'custom', description: '' })
const items = ref([])
const editableState = computed(() => ({ ...form }))
const { isDirty, markSaved } = useUnsavedChanges({ state: editableState, t })

const itemForm = reactive({ code: '', value: '', description: '', sort_order: 0, is_active: true })
const itemRules = computed(() => ({
  code: [{ required: true, message: t('standard.codeSet.codeRequired'), trigger: 'blur' }],
  value: [{ required: true, message: t('standard.codeSet.nameRequired'), trigger: 'blur' }]
}))

const loadCodeSet = async () => {
  try {
    const res = await codeSetAPI.get(codeSetId.value)
    codeSet.value = res || {}
    Object.assign(form, {
      name: codeSet.value.name,
      type: codeSet.value.type || 'custom',
      description: codeSet.value.description || ''
    })
    markSaved()
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.loadFailed'))
    goBack()
  }
}

const loadItems = async () => {
  itemLoading.value = true
  try {
    const res = await codeSetAPI.getItems(codeSetId.value)
    items.value = res || []
  } catch (err) {
    items.value = []
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.loadFailed'))
  } finally {
    itemLoading.value = false
  }
}

const handleSave = async () => {
  if (saving.value) return
  saving.value = true
  try {
    await codeSetAPI.update(codeSetId.value, { ...form, version: codeSet.value.version })
    ElMessage.success(t('standard.common.saveSuccess'))
    await loadCodeSet()
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

const openItemDialog = (item = null) => {
  editingItem.value = item
  if (item) {
    Object.assign(itemForm, {
      code: item.code,
      value: item.value,
      description: item.description || '',
      sort_order: item.sort_order || 0,
      is_active: item.is_active
    })
  } else {
    Object.assign(itemForm, { code: '', value: '', description: '', sort_order: 0, is_active: true })
  }
  itemDialogVisible.value = true
}

const handleItemSubmit = async () => {
  if (itemSubmitting.value) return
  itemSubmitting.value = true
  try {
    const valid = await itemFormRef.value.validate().catch(() => false)
    if (!valid) return
    if (editingItem.value) {
      const res = await codeSetAPI.updateItem(codeSetId.value, editingItem.value.id, { ...itemForm, version: codeSet.value.version })
      codeSet.value.version = res.version
      ElMessage.success(t('standard.common.updateSuccess'))
    } else {
      const res = await codeSetAPI.createItem(codeSetId.value, { ...itemForm, version: codeSet.value.version })
      codeSet.value.version = res.version
      ElMessage.success(t('standard.common.createSuccess'))
    }
    itemDialogVisible.value = false
    loadItems()
  } catch (err) {
    ElMessage.error(getStandardErrorMessage(err, t))
  } finally {
    itemSubmitting.value = false
  }
}

const deleteItem = async (item) => {
  await runLocked(`code-item:${item.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.codeSet.confirmDeleteItem', { name: item.value }), t('standard.common.hint'), { type: 'warning' })
      const res = await codeSetAPI.deleteItem(codeSetId.value, item.id, codeSet.value.version)
      codeSet.value.version = res.version
      ElMessage.success(t('standard.common.deleteSuccess'))
      await loadItems()
    } catch (err) {
      if (!isCanceledInteraction(err)) ElMessage.error(getStandardErrorMessage(err, t, 'standard.common.deleteFailed'))
    }
  })
}

watch(() => route.params.id, () => {
  loadCodeSet()
  loadItems()
}, { immediate: true })
</script>

<style scoped>
.code-set-detail {
  min-height: 100%;
  padding: 20px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.code-set-name {
  font-size: 18px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.code-set-detail :deep(.el-card) { background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }

.info-card {
  margin-bottom: 0;
}

.card-title {
  font-weight: 600;
  color: var(--addp-text-primary);
}

.card-header-with-action {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.items-column { margin-top: 16px; }
.table-actions { display: inline-flex; align-items: center; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }

@media (max-width: 768px) {
  .code-set-detail { padding: 12px; }
  .detail-header { align-items: flex-start; flex-wrap: wrap; gap: 10px; }
  .header-left, .header-right { flex-wrap: wrap; }
  .code-set-detail :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .code-set-detail :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .items-column { margin-top: 12px; }
}
</style>
