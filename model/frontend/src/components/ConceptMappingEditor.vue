<template>
  <div class="concept-mapping-editor">
    <el-alert
      type="info"
      :title="t('model.concept_mapping.help')"
      :closable="false"
      show-icon
    />

    <el-skeleton v-if="loading" :rows="6" animated />
    <el-result
      v-else-if="loadError"
      icon="error"
      :title="t('model.concept_mapping.load_failed')"
      :sub-title="loadError"
    >
      <template #extra><el-button type="primary" @click="load">{{ t('model.common.retry') }}</el-button></template>
    </el-result>

    <template v-else>
      <div class="mapping-toolbar">
        <span class="text-muted">{{ t('model.concept_mapping.version', { version }) }}</span>
        <el-button v-if="editable" type="primary" :loading="saving" :disabled="!dirty || !valid" @click="save">
          {{ t('model.common.save') }}
        </el-button>
      </div>

      <el-card shadow="never">
        <template #header>
          <div class="card-header">
            <div>
              <div class="card-title">{{ t('model.concept_mapping.table_title') }}</div>
              <div class="card-help">{{ t('model.concept_mapping.table_help') }}</div>
            </div>
            <el-button v-if="editable" size="small" @click="addTableMapping">{{ t('model.common.add') }}</el-button>
          </div>
        </template>
        <el-empty v-if="tableMappings.length === 0" :description="t('model.concept_mapping.table_empty')" />
        <el-table v-else :data="tableMappings" stripe>
          <el-table-column :label="t('model.concept_mapping.entity')" min-width="240">
            <template #default="{ row }">
              <span v-if="!editable">{{ row.entity_name }} ({{ row.entity_code }})</span>
              <el-select
                v-else
                v-model="row.entity_id"
                filterable
                style="width:100%"
                @change="loadAttributes(row.entity_id)"
              >
                <el-option
                  v-for="entity in approvedEntities"
                  :key="entity.id"
                  :label="`${entity.name} (${entity.code})`"
                  :value="entity.id"
                  :disabled="entitySelectedElsewhere(entity.id, row)"
                />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.role')" width="190">
            <template #default="{ row }">
              <span v-if="!editable">{{ t(`model.concept_mapping.${row.mapping_role}`) }}</span>
              <el-select v-else v-model="row.mapping_role" style="width:100%">
                <el-option :label="t('model.concept_mapping.represents')" value="represents" />
                <el-option :label="t('model.concept_mapping.derives_from')" value="derives_from" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.sync')" width="120">
            <template #default="{ row }">
              <el-tag v-if="row.in_sync === false" type="danger" size="small">{{ t('model.concept_mapping.drifted') }}</el-tag>
              <el-tag v-else-if="row.id" type="success" size="small">{{ t('model.concept_mapping.synced') }}</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column v-if="editable" :label="t('model.logical_table.actions')" width="90" fixed="right">
            <template #default="{ $index }">
              <el-button link type="danger" @click="removeTableMapping($index)">{{ t('model.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-header">
            <div>
              <div class="card-title">{{ t('model.concept_mapping.field_title') }}</div>
              <div class="card-help">{{ t('model.concept_mapping.field_help') }}</div>
            </div>
            <el-button v-if="editable" size="small" :disabled="!attributeOptions.length" @click="addFieldMapping">{{ t('model.common.add') }}</el-button>
          </div>
        </template>
        <el-empty v-if="fieldMappings.length === 0" :description="t('model.concept_mapping.field_empty')" />
        <el-table v-else :data="fieldMappings" stripe>
          <el-table-column :label="t('model.concept_mapping.logical_field')" min-width="200">
            <template #default="{ row }">
              <span v-if="!editable">{{ row.field_name }} ({{ row.field_column_name }})</span>
              <el-select v-else v-model="row.field_id" filterable style="width:100%">
                <el-option v-for="field in fields" :key="field.id" :label="`${field.name} (${field.column_name})`" :value="field.id" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.entity_attribute')" min-width="260">
            <template #default="{ row }">
              <span v-if="!editable">{{ row.entity_name }}.{{ row.entity_attribute_name }} ({{ row.attribute_column_name }})</span>
              <el-select v-else v-model="row.entity_attribute_id" filterable style="width:100%">
                <el-option v-for="option in attributeOptions" :key="option.id" :label="option.label" :value="option.id" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.role')" width="150">
            <template #default="{ row }">
              <span v-if="!editable">{{ t(`model.concept_mapping.${row.mapping_role}`) }}</span>
              <el-select v-else v-model="row.mapping_role" style="width:100%">
                <el-option :label="t('model.concept_mapping.direct')" value="direct" />
                <el-option :label="t('model.concept_mapping.derived')" value="derived" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column v-if="editable" :label="t('model.logical_table.actions')" width="90" fixed="right">
            <template #default="{ $index }"><el-button link type="danger" @click="fieldMappings.splice($index, 1)">{{ t('model.common.delete') }}</el-button></template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card v-if="tableRelations.length || relationMappings.length" shadow="never">
        <template #header>
          <div class="card-header">
            <div>
              <div class="card-title">{{ t('model.concept_mapping.relation_title') }}</div>
              <div class="card-help">{{ t('model.concept_mapping.relation_help') }}</div>
            </div>
            <el-button v-if="editable" size="small" :disabled="!tableRelations.length" @click="addRelationMapping">{{ t('model.common.add') }}</el-button>
          </div>
        </template>
        <el-empty v-if="relationMappings.length === 0" :description="t('model.concept_mapping.relation_empty')" />
        <el-table v-else :data="relationMappings" stripe>
          <el-table-column :label="t('model.concept_mapping.table_relation')" min-width="250">
            <template #default="{ row }">
              <span v-if="!editable">{{ tableRelationLabel(tableRelationById.get(row.table_relation_id) || row) }}</span>
              <el-select v-else v-model="row.table_relation_id" style="width:100%">
                <el-option v-for="relation in tableRelations" :key="relation.id" :label="tableRelationLabel(relation)" :value="relation.id" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.entity_relation')" min-width="260">
            <template #default="{ row }">
              <span v-if="!editable">{{ row.source_entity_name }} → {{ row.target_entity_name }} ({{ row.entity_relation_name }})</span>
              <el-select v-else v-model="row.entity_relation_id" filterable style="width:100%">
                <el-option v-for="relation in entityRelations" :key="relation.id" :label="entityRelationLabel(relation)" :value="relation.id" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.orientation')" width="150">
            <template #default="{ row }">
              <span v-if="!editable">{{ t(`model.concept_mapping.${row.orientation}`) }}</span>
              <el-select v-else v-model="row.orientation" style="width:100%">
                <el-option :label="t('model.concept_mapping.same')" value="same" />
                <el-option :label="t('model.concept_mapping.inverse')" value="inverse" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.concept_mapping.sync')" width="120">
            <template #default="{ row }">
              <el-tag v-if="row.in_sync === false" type="danger" size="small">{{ t('model.concept_mapping.drifted') }}</el-tag>
              <el-tag v-else-if="row.id" type="success" size="small">{{ t('model.concept_mapping.synced') }}</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column v-if="editable" :label="t('model.logical_table.actions')" width="90" fixed="right">
            <template #default="{ $index }"><el-button link type="danger" @click="relationMappings.splice($index, 1)">{{ t('model.common.delete') }}</el-button></template>
          </el-table-column>
        </el-table>
      </el-card>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { entityAPI, entityRelationAPI, logicalTableAPI } from '../api/model'
import { getModelErrorMessage } from '../utils/apiError'

const props = defineProps({
  table: { type: Object, required: true },
  fields: { type: Array, default: () => [] },
  editable: { type: Boolean, default: false }
})
const emit = defineEmits(['update-version', 'dirty-change'])
const { t } = useI18n()

const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const version = ref(0)
const approvedEntities = ref([])
const entityRelations = ref([])
const tableRelations = ref([])
const attributesByEntity = ref({})
const tableMappings = ref([])
const fieldMappings = ref([])
const relationMappings = ref([])
const baseline = ref('')

const editableState = computed(() => ({
  table_mappings: tableMappings.value.map(({ entity_id, mapping_role }) => ({ entity_id, mapping_role })),
  field_mappings: fieldMappings.value.map(({ field_id, entity_attribute_id, mapping_role }) => ({ field_id, entity_attribute_id, mapping_role })),
  relation_mappings: relationMappings.value.map(({ table_relation_id, entity_relation_id, orientation }) => ({ table_relation_id, entity_relation_id, orientation }))
}))
const snapshot = computed(() => JSON.stringify(editableState.value))
const dirty = computed(() => baseline.value !== '' && snapshot.value !== baseline.value)
const valid = computed(() =>
  editableState.value.table_mappings.every(mapping => mapping.entity_id > 0 && mapping.mapping_role) &&
  editableState.value.field_mappings.every(mapping => mapping.field_id > 0 && mapping.entity_attribute_id > 0 && mapping.mapping_role) &&
  editableState.value.relation_mappings.every(mapping => mapping.table_relation_id > 0 && mapping.entity_relation_id > 0 && mapping.orientation)
)
const entityById = computed(() => new Map(approvedEntities.value.map(entity => [entity.id, entity])))
const tableRelationById = computed(() => new Map(tableRelations.value.map(relation => [relation.id, relation])))
const attributeOptions = computed(() => tableMappings.value.flatMap(mapping => {
  const entity = entityById.value.get(mapping.entity_id)
  return (attributesByEntity.value[mapping.entity_id] || []).map(attribute => ({
    id: attribute.id,
    entity_id: mapping.entity_id,
    label: `${entity?.name || mapping.entity_name || mapping.entity_id}.${attribute.name} (${attribute.column_name})`
  }))
}))

watch(dirty, value => emit('dirty-change', value), { immediate: true })

const loadAttributes = async entityId => {
  if (!entityId || attributesByEntity.value[entityId]) return
  const attributes = await entityAPI.getAttributes(entityId)
  attributesByEntity.value = { ...attributesByEntity.value, [entityId]: attributes || [] }
}

const load = async () => {
  loading.value = true
  loadError.value = ''
  try {
    const [mappingResult, entitiesResult, relationsResult, tableRelationsResult] = await Promise.all([
      logicalTableAPI.getConceptMappings(props.table.id),
      props.editable ? entityAPI.listAll({ status: 'approved' }) : Promise.resolve([]),
      props.editable ? entityRelationAPI.list() : Promise.resolve([]),
      props.table.table_type === 'fact' || props.table.table_type === 'dimension'
        ? logicalTableAPI.listDimensionRelations(props.table.id)
        : Promise.resolve([])
    ])
    version.value = mappingResult.version
    approvedEntities.value = entitiesResult || []
    entityRelations.value = relationsResult || []
    tableRelations.value = (tableRelationsResult || []).filter(relation => relation.source_table === props.table.id)
    tableMappings.value = (mappingResult.table_mappings || []).map(item => ({ ...item }))
    fieldMappings.value = (mappingResult.field_mappings || []).map(item => ({ ...item }))
    relationMappings.value = (mappingResult.relation_mappings || []).map(item => ({ ...item }))
    attributesByEntity.value = {}
    if (props.editable) await Promise.all(tableMappings.value.map(mapping => loadAttributes(mapping.entity_id)))
    await nextTick()
    baseline.value = snapshot.value
  } catch (error) {
    loadError.value = getModelErrorMessage(error, t, 'model.concept_mapping.load_failed')
  } finally {
    loading.value = false
  }
}

const entitySelectedElsewhere = (entityId, current) =>
  tableMappings.value.some(mapping => mapping !== current && mapping.entity_id === entityId)

const addTableMapping = () => tableMappings.value.push({ entity_id: null, mapping_role: 'represents' })
const removeTableMapping = index => {
  const [removed] = tableMappings.value.splice(index, 1)
  const removedAttributes = new Set((attributesByEntity.value[removed?.entity_id] || []).map(attribute => attribute.id))
  fieldMappings.value = fieldMappings.value.filter(mapping => !removedAttributes.has(mapping.entity_attribute_id))
}
const addFieldMapping = () => fieldMappings.value.push({
  field_id: props.fields[0]?.id || null,
  entity_attribute_id: attributeOptions.value[0]?.id || null,
  mapping_role: 'direct'
})
const addRelationMapping = () => relationMappings.value.push({
  table_relation_id: tableRelations.value[0]?.id || null,
  entity_relation_id: entityRelations.value[0]?.id || null,
  orientation: 'inverse'
})

const tableRelationLabel = relation =>
  `${relation.source_field_code || relation.source_field_name} → ${relation.target_table_name}.${relation.target_field_code || relation.target_field_name}`
const entityRelationLabel = relation => {
  const source = entityById.value.get(relation.source_entity)?.name || relation.source_entity
  const target = entityById.value.get(relation.target_entity)?.name || relation.target_entity
  return `${source} → ${target} (${relation.name || relation.relation_type})`
}

const save = async () => {
  if (!props.editable || saving.value) return
  saving.value = true
  try {
    const response = await logicalTableAPI.replaceConceptMappings(props.table.id, {
      version: version.value,
      ...editableState.value
    })
    version.value = response.version
    tableMappings.value = (response.table_mappings || []).map(item => ({ ...item }))
    fieldMappings.value = (response.field_mappings || []).map(item => ({ ...item }))
    relationMappings.value = (response.relation_mappings || []).map(item => ({ ...item }))
    await nextTick()
    baseline.value = snapshot.value
    emit('update-version', response.version)
    ElMessage.success(t('model.common.save_success'))
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t, 'model.common.save_failed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.concept-mapping-editor { display: grid; gap: 16px; }
.mapping-toolbar, .card-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.card-title { font-weight: 600; }
.card-help, .text-muted { color: var(--addp-text-tertiary); font-size: 13px; margin-top: 4px; }
@media (max-width: 767px) {
  .mapping-toolbar, .card-header { align-items: flex-start; flex-direction: column; }
}
</style>
