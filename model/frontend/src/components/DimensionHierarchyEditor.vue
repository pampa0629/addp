<template>
  <el-card shadow="never">
    <template #header>
      <div class="card-header-with-action">
        <div>
          <span class="card-title">{{ t('model.dimension_hierarchy.title') }}</span>
          <div class="section-help">{{ t('model.dimension_hierarchy.help') }}</div>
        </div>
        <el-button v-if="editable" type="primary" size="small" @click="openHierarchyDialog()">
          <el-icon><Plus /></el-icon>
          {{ t('model.dimension_hierarchy.add') }}
        </el-button>
      </div>
    </template>

    <el-skeleton v-if="loading" :rows="4" animated />
    <el-empty v-else-if="hierarchies.length === 0" :description="t('model.dimension_hierarchy.empty')" />
    <div v-else class="hierarchy-list">
      <section v-for="hierarchy in hierarchies" :key="hierarchy.id" class="hierarchy-item">
        <div class="hierarchy-header">
          <div>
            <div class="hierarchy-name">{{ hierarchy.name }}</div>
            <div v-if="hierarchy.description" class="section-help">{{ hierarchy.description }}</div>
          </div>
          <div v-if="editable" class="actions">
            <el-button link type="primary" @click="openHierarchyDialog(hierarchy)">{{ t('model.common.edit') }}</el-button>
            <el-popconfirm :title="t('model.dimension_hierarchy.delete_confirm', { name: hierarchy.name })" @confirm="deleteHierarchy(hierarchy.id)">
              <template #reference>
                <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
              </template>
            </el-popconfirm>
            <el-button link type="primary" @click="openLevelDialog(hierarchy)">{{ t('model.dimension_hierarchy.add_level') }}</el-button>
          </div>
        </div>
        <el-table :data="hierarchy.levels || []" size="small" stripe>
          <el-table-column prop="level_num" :label="t('model.dimension_hierarchy.level_num')" width="100" />
          <el-table-column prop="level_name" :label="t('model.dimension_hierarchy.level_name')" min-width="140" />
          <el-table-column :label="t('model.dimension_hierarchy.field')" min-width="180">
            <template #default="{ row }">{{ fieldLabel(row.field_id) }}</template>
          </el-table-column>
          <el-table-column v-if="editable" :label="t('model.field.actions')" width="130" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openLevelDialog(hierarchy, row)">{{ t('model.common.edit') }}</el-button>
              <el-popconfirm :title="t('model.dimension_hierarchy.delete_level_confirm', { name: row.level_name })" @confirm="deleteLevel(hierarchy.id, row.id)">
                <template #reference>
                  <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </div>

    <el-dialog v-model="hierarchyDialogVisible" class="addp-dialog" :title="editingHierarchy ? t('model.dimension_hierarchy.edit') : t('model.dimension_hierarchy.add')" width="min(520px, calc(100vw - 32px))">
      <el-form ref="hierarchyFormRef" :model="hierarchyForm" :rules="hierarchyRules" label-width="100px">
        <el-form-item :label="t('model.dimension_hierarchy.name')" prop="name">
          <el-input v-model="hierarchyForm.name" maxlength="200" />
        </el-form-item>
        <el-form-item :label="t('model.dimension_hierarchy.description')">
          <el-input v-model="hierarchyForm.description" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="hierarchyDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitHierarchy">{{ t('model.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="levelDialogVisible" class="addp-dialog" :title="editingLevel ? t('model.dimension_hierarchy.edit_level') : t('model.dimension_hierarchy.add_level')" width="min(520px, calc(100vw - 32px))">
      <el-form ref="levelFormRef" :model="levelForm" :rules="levelRules" label-width="100px">
        <el-form-item :label="t('model.dimension_hierarchy.level_num')" prop="level_num">
          <el-input-number v-model="levelForm.level_num" :min="1" style="width:100%" />
        </el-form-item>
        <el-form-item :label="t('model.dimension_hierarchy.level_name')" prop="level_name">
          <el-input v-model="levelForm.level_name" maxlength="100" />
        </el-form-item>
        <el-form-item :label="t('model.dimension_hierarchy.field')" prop="field_id">
          <el-select v-model="levelForm.field_id" filterable style="width:100%">
            <el-option v-for="field in fields" :key="field.id" :label="fieldLabel(field.id)" :value="field.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="levelDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitLevel">{{ t('model.common.save') }}</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { logicalTableAPI } from '../api/model'
import { getModelErrorMessage } from '../utils/apiError'

const props = defineProps({
  tableId: { type: Number, required: true },
  version: { type: Number, required: true },
  fields: { type: Array, default: () => [] },
  editable: { type: Boolean, default: false }
})
const emit = defineEmits(['update-version'])
const { t } = useI18n()

const loading = ref(false)
const submitting = ref(false)
const hierarchies = ref([])
const hierarchyDialogVisible = ref(false)
const levelDialogVisible = ref(false)
const editingHierarchy = ref(null)
const editingLevel = ref(null)
const activeHierarchy = ref(null)
const hierarchyFormRef = ref(null)
const levelFormRef = ref(null)
const hierarchyForm = reactive({ name: '', description: '' })
const levelForm = reactive({ level_num: 1, level_name: '', field_id: null })
const hierarchyRules = { name: [{ required: true, message: t('model.dimension_hierarchy.name_required'), trigger: 'blur' }] }
const levelRules = {
  level_num: [{ required: true, message: t('model.dimension_hierarchy.level_num_required'), trigger: 'change' }],
  level_name: [{ required: true, message: t('model.dimension_hierarchy.level_name_required'), trigger: 'blur' }],
  field_id: [{ required: true, message: t('model.dimension_hierarchy.field_required'), trigger: 'change' }]
}

const load = async () => {
  loading.value = true
  try {
    hierarchies.value = await logicalTableAPI.listDimensionHierarchies(props.tableId)
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.dimension_hierarchy.load_failed'))
  } finally {
    loading.value = false
  }
}

const fieldLabel = fieldId => {
  const field = props.fields.find(item => item.id === fieldId)
  return field ? `${field.name} (${field.column_name})` : `#${fieldId}`
}

const openHierarchyDialog = hierarchy => {
  editingHierarchy.value = hierarchy || null
  Object.assign(hierarchyForm, { name: hierarchy?.name || '', description: hierarchy?.description || '' })
  hierarchyDialogVisible.value = true
}

const submitHierarchy = async () => {
  if (!await hierarchyFormRef.value?.validate()) return
  submitting.value = true
  try {
    const body = { ...hierarchyForm, version: props.version }
    const result = editingHierarchy.value
      ? await logicalTableAPI.updateDimensionHierarchy(props.tableId, editingHierarchy.value.id, body)
      : await logicalTableAPI.createDimensionHierarchy(props.tableId, body)
    emit('update-version', result.version)
    hierarchyDialogVisible.value = false
    await load()
    ElMessage.success(t('model.common.save_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.save_failed'))
  } finally {
    submitting.value = false
  }
}

const deleteHierarchy = async hierarchyId => {
  try {
    const result = await logicalTableAPI.deleteDimensionHierarchy(props.tableId, hierarchyId, props.version)
    emit('update-version', result.version)
    await load()
    ElMessage.success(t('model.common.delete_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

const openLevelDialog = (hierarchy, level) => {
  activeHierarchy.value = hierarchy
  editingLevel.value = level || null
  Object.assign(levelForm, {
    level_num: level?.level_num || Math.max(1, ...(hierarchy.levels || []).map(item => item.level_num + 1)),
    level_name: level?.level_name || '',
    field_id: level?.field_id || null
  })
  levelDialogVisible.value = true
}

const submitLevel = async () => {
  if (!await levelFormRef.value?.validate()) return
  submitting.value = true
  try {
    const body = { ...levelForm, version: props.version }
    const result = editingLevel.value
      ? await logicalTableAPI.updateDimensionHierarchyLevel(props.tableId, activeHierarchy.value.id, editingLevel.value.id, body)
      : await logicalTableAPI.createDimensionHierarchyLevel(props.tableId, activeHierarchy.value.id, body)
    emit('update-version', result.version)
    levelDialogVisible.value = false
    await load()
    ElMessage.success(t('model.common.save_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.save_failed'))
  } finally {
    submitting.value = false
  }
}

const deleteLevel = async (hierarchyId, levelId) => {
  try {
    const result = await logicalTableAPI.deleteDimensionHierarchyLevel(props.tableId, hierarchyId, levelId, props.version)
    emit('update-version', result.version)
    await load()
    ElMessage.success(t('model.common.delete_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

watch(() => props.tableId, load)
onMounted(load)
</script>

<style scoped>
.card-header-with-action,
.hierarchy-header,
.actions {
  display: flex;
  align-items: center;
}

.card-header-with-action,
.hierarchy-header {
  justify-content: space-between;
  gap: 16px;
}

.card-title,
.hierarchy-name {
  color: var(--addp-text-primary);
  font-weight: 600;
}

.section-help {
  margin-top: 4px;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.hierarchy-list {
  display: grid;
  gap: 16px;
}

.hierarchy-item {
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
}

.hierarchy-header {
  margin-bottom: 12px;
}
</style>
