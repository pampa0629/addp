<template>
  <section class="sensitive-type-list">
    <div class="section-header">
      <p>{{ t('security.descriptions.sensitiveDataType') }}</p>
      <el-button v-if="can('create')" type="primary" @click="openCreate">
        {{ t('security.common.createResource', { name: t('security.resources.sensitiveDataType') }) }}
      </el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.sensitiveType.type')" min-width="190">
          <template #default="{ row }">
            <div class="primary-text">{{ row.name }}</div>
            <div class="secondary-text">{{ row.code }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.fields.security_classification_id')" min-width="170">
          <template #default="{ row }">{{ referenceLabel(classifications, row.security_classification_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.fields.default_security_grade_id')" min-width="190">
          <template #default="{ row }">{{ referenceLabel(grades, row.default_security_grade_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.sensitiveType.recognition')" min-width="180">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetectors(row)">
              {{ t('security.sensitiveType.recognitionCount', recognitionCount(row.id)) }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.sensitiveType.defaultProtection')" min-width="170">
          <template #default="{ row }">
            {{ t('security.sensitiveType.baselineCount', { count: baselineCount(row.id) }) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('security.sensitiveType.empty')" />
    </div>

    <el-dialog
      v-model="dialog"
      class="addp-dialog"
      :title="editing ? t('security.common.editResource', { name: t('security.resources.sensitiveDataType') }) : t('security.common.createResource', { name: t('security.resources.sensitiveDataType') })"
      width="min(620px, calc(100vw - 24px))"
    >
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item :label="t('security.fields.code')" required>
            <el-input v-model="form.code" :disabled="Boolean(editing)" />
          </el-form-item>
          <el-form-item :label="t('security.fields.name')" required>
            <el-input v-model="form.name" />
          </el-form-item>
        </div>
        <el-form-item :label="t('security.fields.description')">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item :label="t('security.fields.security_classification_id')" required>
          <el-select v-model="form.security_classification_id" class="wide" filterable>
            <el-option v-for="item in classifications" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
          </el-select>
          <div class="field-help">{{ t('security.sensitiveType.classificationHelp') }}</div>
        </el-form-item>
        <el-form-item :label="t('security.fields.default_security_grade_id')" required>
          <el-select v-model="form.default_security_grade_id" class="wide" filterable>
            <el-option v-for="item in orderedGrades" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
          </el-select>
          <div class="field-help">{{ t('security.sensitiveType.initialGradeHelp') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('security.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="detectorDrawer" :title="t('security.sensitiveType.manageRecognition')" size="min(820px, 92vw)">
      <div v-if="selectedType" class="drawer-context">
        <div class="primary-text">{{ selectedType.name }}</div>
        <div class="secondary-text">{{ selectedType.code }}</div>
      </div>
      <DetectorBindings
        v-if="selectedType"
        :key="`${selectedType.id}:${detectorDrawerRevision}`"
        :sensitive-type-id="Number(selectedType.id)"
        :sensitive-type-name="selectedType.name"
        @changed="load"
      />
    </el-drawer>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { classificationAPI, detectorAPI, gradeAPI, protectionBaselineAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import DetectorBindings from './DetectorBindings.vue'

const { t } = useI18n()
const auth = useAuthStore()
const rows = ref([])
const classifications = ref([])
const grades = ref([])
const detectors = ref([])
const baselines = ref([])
const loading = ref(false)
const saving = ref(false)
const dialog = ref(false)
const detectorDrawer = ref(false)
const detectorDrawerRevision = ref(0)
const editing = ref(null)
const selectedType = ref(null)
const form = reactive({ code: '', name: '', description: '', security_classification_id: null, default_security_grade_id: null, version: 0 })
const orderedGrades = computed(() => [...grades.value].sort((left, right) => Number(left.risk_order) - Number(right.risk_order)))

function can(action) { return auth.hasPermission(`security.sensitive_data_type.${action}`) }
function definitionLabel(item) {
  return t('security.common.referenceOption', { name: item.name, code: item.code })
}
function referenceLabel(items, id) {
  const item = items.find(candidate => String(candidate.id) === String(id))
  return item ? definitionLabel(item) : t('security.common.unknownReference', { id })
}
function recognitionCount(id) {
  const all = detectors.value.filter(item => String(item.sensitive_data_type_id) === String(id))
  return { enabled: all.filter(item => item.enabled).length, total: all.length }
}
function baselineCount(id) {
  return baselines.value.filter(item => String(item.sensitive_data_type_id) === String(id) && item.enabled).length
}

async function load() {
  loading.value = true
  try {
    const result = await Promise.all([
      sensitiveDataTypeAPI.list(), classificationAPI.list(), gradeAPI.list(), detectorAPI.list(), protectionBaselineAPI.list()
    ])
    rows.value = Array.isArray(result[0]) ? result[0] : []
    classifications.value = Array.isArray(result[1]) ? result[1] : []
    grades.value = Array.isArray(result[2]) ? result[2] : []
    detectors.value = Array.isArray(result[3]) ? result[3] : []
    baselines.value = Array.isArray(result[4]) ? result[4] : []
    if (selectedType.value) selectedType.value = rows.value.find(item => String(item.id) === String(selectedType.value.id)) || null
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    loading.value = false
  }
}

function reset(row = {}) {
  form.code = row.code || ''
  form.name = row.name || ''
  form.description = row.description || ''
  form.security_classification_id = row.security_classification_id ? Number(row.security_classification_id) : (classifications.value[0] ? Number(classifications.value[0].id) : null)
  form.default_security_grade_id = row.default_security_grade_id ? Number(row.default_security_grade_id) : (orderedGrades.value[0] ? Number(orderedGrades.value[0].id) : null)
  form.version = Number(row.version || 0)
}
function openCreate() { editing.value = null; reset(); dialog.value = true }
function openEdit(row) { editing.value = row; reset(row); dialog.value = true }
function openDetectors(row) {
  selectedType.value = row
  detectorDrawerRevision.value += 1
  detectorDrawer.value = true
}

async function save() {
  if (!form.code.trim() || !form.name.trim() || !form.security_classification_id || !form.default_security_grade_id) {
    ElMessage.warning(t('security.sensitiveType.required'))
    return
  }
  saving.value = true
  try {
    const payload = {
      code: form.code.trim(), name: form.name.trim(), description: form.description.trim(),
      security_classification_id: Number(form.security_classification_id),
      default_security_grade_id: Number(form.default_security_grade_id)
    }
    if (editing.value) {
      payload.version = form.version
      await sensitiveDataTypeAPI.update(editing.value.id, payload)
    } else {
      await sensitiveDataTypeAPI.create(payload)
    }
    dialog.value = false
    await load()
    ElMessage.success(t('security.common.saved'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(t('security.common.confirmDelete', { name: row.name }), t('security.common.hint'), { type: 'warning' })
    await sensitiveDataTypeAPI.delete(row.id)
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || t('security.common.failed'))
  }
}

onMounted(load)
</script>

<style scoped>
.sensitive-type-list { min-height: 0; }
.section-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.section-header p { margin: 4px 0 0; color: var(--addp-text-secondary); }
.table-panel { overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.primary-text { color: var(--addp-text-primary); font-weight: 600; }
.secondary-text { margin-top: 4px; color: var(--addp-text-tertiary); font-size: 12px; }
.wide { width: 100%; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field-help { margin-top: 6px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.drawer-context { margin: -4px 0 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
@media (max-width: 720px) { .form-grid { grid-template-columns: 1fr; gap: 0; } }
</style>
