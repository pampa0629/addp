<template>
  <section class="table-relations" v-loading="loading">
    <el-alert :title="helpText" type="info" :closable="false" show-icon />
    <div class="relation-toolbar">
      <el-button v-if="editable && isFact" type="primary" :disabled="Boolean(loadError) || loading" @click="openEditor()">{{ t('model.table_relation.add') }}</el-button>
    </div>
    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon>
      <el-button link @click="loadRelations">{{ t('model.common.retry') }}</el-button>
    </el-alert>
    <el-alert v-else-if="relationId && !loading && !relations.some(item => item.id === relationId)" :title="t('model.table_relation.unavailable')" type="warning" :closable="false" />
    <el-empty v-if="!loading && !loadError && !relations.length" :description="t(isFact ? 'model.table_relation.empty' : 'model.table_relation.incoming_empty')" />
    <article v-for="relation in relations" :key="relation.id" :data-relation-id="relation.id" :class="['relation-card', { selected: relation.id === relationId }]" :aria-label="`${relation.source_table_name} → ${relation.target_table_name}`">
      <div class="relation-heading">
        <strong>{{ relation.source_table_name }} → {{ relation.target_table_name }}</strong>
        <el-tag size="small">{{ relation.relation_type.toUpperCase() }}</el-tag>
      </div>
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="t('model.table_relation.source')">{{ relation.source_table_name }}（{{ relation.source_table_code }}） · {{ relation.source_field_name }}（{{ relation.source_field_code }}）</el-descriptions-item>
        <el-descriptions-item :label="t('model.table_relation.target')">{{ relation.target_table_name }}（{{ relation.target_table_code }}） · {{ relation.target_field_name }}（{{ relation.target_field_code }}）</el-descriptions-item>
        <el-descriptions-item :label="t('model.table_relation.condition')"><code>{{ relation.source_table_code }}.{{ relation.source_field_code }} = {{ relation.target_table_code }}.{{ relation.target_field_code }}</code></el-descriptions-item>
      </el-descriptions>
      <div class="relation-actions">
        <template v-if="isFact">
          <el-button link type="primary" @click="openDimension(relation)">{{ t('model.table_relation.view_dimension') }}</el-button>
          <el-button v-if="editable" type="primary" :disabled="busy" @click="openEditor(relation)">{{ t('model.common.edit') }}</el-button>
          <el-button v-if="editable" type="danger" plain :disabled="busy" @click="removeRelation(relation)">{{ t('model.common.delete') }}</el-button>
        </template>
        <el-button v-else type="primary" plain @click="openSource(relation)">{{ t('model.table_relation.view_source_relation') }}</el-button>
      </div>
    </article>
    <el-dialog v-model="dialogVisible" class="addp-dialog" :title="t(editingId ? 'model.table_relation.edit' : 'model.table_relation.add')" width="min(660px, calc(100vw - 32px))" :close-on-click-modal="false" :before-close="closeDialog">
      <el-alert :title="t('model.table_relation.mapping_help')" type="info" :closable="false" show-icon />
      <el-alert v-if="editorError" :title="editorError" type="error" :closable="false" class="editor-error" />
      <el-form ref="formRef" :model="form" label-position="top" class="relation-form" v-loading="loadingOptions">
        <el-form-item :label="t('model.table_relation.source')" prop="source_field" :rules="[requiredRule]">
          <el-select v-model="form.source_field" filterable :disabled="busy" style="width:100%">
            <el-option v-for="field in fields" :key="field.id" :value="field.id" :label="fieldLabel(field)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.table_relation.dimension')" prop="target_table" :rules="[requiredRule]">
          <el-select v-model="form.target_table" filterable :disabled="busy" style="width:100%" @change="changeDimension">
            <el-option v-for="dimension in dimensions" :key="dimension.id" :value="dimension.id" :label="`${dimension.name}（${dimension.code}）`" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.table_relation.target_field')" prop="target_field" :rules="[requiredRule]">
          <el-select v-model="form.target_field" filterable :disabled="!form.target_table || loadingTarget || busy" :loading="loadingTarget" style="width:100%">
            <el-option v-for="field in targetFields" :key="field.id" :value="field.id" :label="fieldLabel(field)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.table_relation.type')">
          <el-radio-group v-model="form.relation_type" :disabled="busy">
            <el-radio value="fk">{{ t('model.table_relation.fk') }}</el-radio>
            <el-radio value="join">{{ t('model.table_relation.join') }}</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="busy" @click="closeDialog()">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" :loading="busy" :disabled="loadingOptions || loadingTarget || !optionsReady" @click="saveRelation">{{ t('model.common.save') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { logicalTableAPI } from '../api/model'
import { getModelErrorMessage } from '../utils/apiError'
import { snapshotUnsavedState } from '../utils/modelDetailState'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { buildTableRelationRoute } from '../utils/routeState'

const props = defineProps({
  table: { type: Object, required: true },
  fields: { type: Array, required: true },
  editable: Boolean,
  relationId: { type: Number, default: null },
  confirmDiscard: { type: Function, required: true }
})
const emit = defineEmits(['update-version', 'dirty-change', 'relation-removed'])
const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const isFact = computed(() => props.table.table_type === 'fact')
const helpText = computed(() => t(!isFact.value ? 'model.table_relation.incoming_help' : props.table.status === 'approved' ? 'model.table_relation.approved_help' : 'model.table_relation.draft_help'))
const relations = ref([])
const loading = ref(false)
const loadError = ref('')
const busy = ref(false)
const dialogVisible = ref(false)
const editingId = ref(null)
const editVersion = ref(null)
const editorError = ref('')
const loadingOptions = ref(false)
const loadingTarget = ref(false)
const optionsReady = ref(false)
const dimensions = ref([])
const targetFields = ref([])
const formRef = ref(null)
const form = reactive({ source_field: null, target_table: null, target_field: null, relation_type: 'fk' })
const baseline = ref('')
const dirty = computed(() => dialogVisible.value && snapshotUnsavedState(form) !== baseline.value)
const requiredRule = computed(() => ({ required: true, message: t('model.table_relation.required') }))
const fieldLabel = field => `${field.name}（${field.column_name}）${field.is_pk ? ' · PK' : ''}`
let loadGeneration = 0
let targetGeneration = 0
let editorGeneration = 0

watch(dirty, value => emit('dirty-change', value), { flush: 'sync' })
const locateRelation = async () => {
  await nextTick()
  if (props.relationId) document.querySelector(`[data-relation-id="${props.relationId}"]`)?.scrollIntoView({ block: 'nearest' })
}
const loadRelations = async () => {
  const generation = ++loadGeneration
  loading.value = true
  loadError.value = ''
  try {
    const result = await logicalTableAPI.listDimensionRelations(props.table.id)
    if (generation !== loadGeneration) return
    relations.value = result
    await locateRelation()
  } catch (error) {
    if (generation === loadGeneration) loadError.value = getModelErrorMessage(error, t, 'model.common.load_failed')
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}
watch(() => props.table.id, loadRelations, { immediate: true })
watch(() => props.relationId, locateRelation)

const openSource = relation => navigateModelRoute(router, buildTableRelationRoute(relation.source_table, relation.id, route.query))
const openDimension = relation => navigateModelRoute(router, `/logical-tables/${relation.target_table}`)
const loadTargetFields = async id => {
  const generation = ++targetGeneration
  loadingTarget.value = true
  targetFields.value = []
  try {
    const result = await logicalTableAPI.getFields(id)
    if (generation === targetGeneration) targetFields.value = result
  } catch (error) {
    if (generation === targetGeneration) editorError.value = getModelErrorMessage(error, t, 'model.common.load_failed')
  } finally {
    if (generation === targetGeneration) loadingTarget.value = false
  }
}
const changeDimension = async id => {
  form.target_field = null
  editorError.value = ''
  await loadTargetFields(id)
}
const openEditor = async relation => {
  if (!props.editable || busy.value) return
  const generation = ++editorGeneration
  editingId.value = relation?.id || null
  editVersion.value = props.table.version
  Object.assign(form, { source_field: relation?.source_field || null, target_table: relation?.target_table || null, target_field: relation?.target_field || null, relation_type: relation?.relation_type || 'fk' })
  baseline.value = snapshotUnsavedState(form)
  editorError.value = ''
  targetFields.value = []
  optionsReady.value = false
  loadingOptions.value = true
  dialogVisible.value = true
  await nextTick()
  formRef.value?.clearValidate()
  const results = await Promise.allSettled([
    logicalTableAPI.listAll({ table_type: 'dimension' }),
    form.target_table ? loadTargetFields(form.target_table) : Promise.resolve()
  ])
  if (generation !== editorGeneration) return
  if (results[0].status === 'fulfilled') {
    dimensions.value = results[0].value
    optionsReady.value = true
  } else {
    editorError.value = getModelErrorMessage(results[0].reason, t, 'model.common.load_failed')
  }
  loadingOptions.value = false
}
const closeDialog = async done => {
  if (busy.value || !(await props.confirmDiscard(dirty.value))) return
  editorGeneration += 1
  targetGeneration += 1
  dialogVisible.value = false
  if (typeof done === 'function') done()
}
const saveRelation = async () => {
  if (!props.editable || busy.value || !optionsReady.value || loadingTarget.value) return
  if (!(await formRef.value.validate().catch(() => false))) return
  const source = props.fields.find(field => field.id === form.source_field)
  const target = targetFields.value.find(field => field.id === form.target_field)
  if (!dimensions.value.some(table => table.id === form.target_table) || !source || !target || source.data_type !== target.data_type || (form.relation_type === 'fk' && !target.is_pk)) {
    editorError.value = t('model.table_relation.invalid_mapping')
    return
  }
  busy.value = true
  editorError.value = ''
  try {
    const request = { version: editVersion.value, ...form }
    const response = editingId.value
      ? await logicalTableAPI.updateDimensionRelation(props.table.id, editingId.value, request)
      : await logicalTableAPI.addDimensionRelation(props.table.id, request)
    emit('update-version', response.version)
    dialogVisible.value = false
    ElMessage.success(t('model.common.save_success'))
    await loadRelations()
  } catch (error) {
    editorError.value = getModelErrorMessage(error, t, 'model.common.save_failed')
  } finally {
    busy.value = false
  }
}
const removeRelation = async relation => {
  if (!props.editable || busy.value) return
  const version = props.table.version
  try {
    await ElMessageBox.confirm(t('model.table_relation.delete_confirm', { source: relation.source_table_name, target: relation.target_table_name }), t('model.common.delete'), {
      type: 'warning', customClass: 'addp-message-box', confirmButtonText: t('model.common.delete'), cancelButtonText: t('model.common.cancel')
    })
  } catch { return }
  busy.value = true
  try {
    const response = await logicalTableAPI.removeDimensionRelation(props.table.id, relation.id, version)
    emit('update-version', response.version)
    emit('relation-removed', relation.id)
    await loadRelations()
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.delete_failed'))
  } finally {
    busy.value = false
  }
}
onBeforeUnmount(() => {
  loadGeneration += 1
  targetGeneration += 1
  editorGeneration += 1
  emit('dirty-change', false)
})
</script>

<style scoped>
.table-relations { min-width: 0; }
.relation-toolbar, .relation-form, .editor-error { margin-top: 16px; }
.relation-card { margin-top: 16px; padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 6px; }
.relation-card.selected { border-color: var(--el-color-primary); }
.relation-heading, .relation-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.relation-heading { margin-bottom: 16px; }
.relation-actions { margin-top: 16px; justify-content: flex-end; }
code { overflow-wrap: anywhere; }
</style>
