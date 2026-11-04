<template>
  <section v-if="allowed" class="engine-table-structure" data-testid="engine-table-structure">
    <ResourceTreePicker :key="session" :adapter="adapter" :engine-id="engine.id" :show-engine-selector="false"
      :show-search="false" :show-selection-summary="false" mode="any" :title="engine.name" tree-height="420px"
      :selectable-filter="selectable" @node-click="clearFields" @select="selectTable" />
    <div class="structure-result" v-loading="loading">
      <div class="structure-heading">
        <strong data-testid="structure-target">{{ targetLabel }}</strong>
        <el-button v-if="target" :disabled="loading" @click="loadFields">{{ t('system.engine.actions.refresh') }}</el-button>
      </div>
      <el-alert v-if="error" :title="error" type="error" :closable="false" data-testid="structure-error" />
      <template v-else-if="fields !== null">
        <p>{{ t('system.engine.tableStructure.fieldCount', { count: fields.length }) }}</p>
        <el-table :data="fields" height="420" border data-testid="structure-fields">
          <el-table-column prop="name" :label="t('system.engine.tableStructure.name')" min-width="160" show-overflow-tooltip />
          <el-table-column prop="native_type" :label="t('system.engine.tableStructure.nativeType')" min-width="140" show-overflow-tooltip />
          <el-table-column prop="type" :label="t('system.engine.tableStructure.type')" min-width="110" />
          <el-table-column :label="t('system.engine.tableStructure.nullable')" width="90">
            <template #default="{ row }">{{ t(row.nullable ? 'common.yes' : 'common.no') }}</template>
          </el-table-column>
          <el-table-column :label="t('system.engine.tableStructure.primaryKey')" width="90">
            <template #default="{ row }">{{ t(row.primary_key ? 'common.yes' : 'common.no') }}</template>
          </el-table-column>
        </el-table>
      </template>
      <el-empty v-else-if="!loading" :description="t('system.engine.tableStructure.selectTable')" />
    </div>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTreePicker, createEngineCatalogTreeAdapter, serializeEngineCatalogTarget, parseEngineCapabilities } from '@common-ui'
import { useAuthStore } from '../../store/auth'
import { enginesAPI } from '../../api/engines'
import { supportsEngineTableStructure, isStructureTableEntry, tableStructureFields } from '../../utils/engineTableStructure'
import { useEngineAuthorizationSession } from '../../utils/engineAuthorizationSession'

const props = defineProps({ engine: { type: Object, required: true } })
const { t } = useI18n(), auth = useAuthStore()
const { authorizationKey, authorizationReady } = useEngineAuthorizationSession(auth, computed(() => props.engine))
const allowed = computed(() => authorizationReady.value && auth.hasPermission('system.engine_catalog.read') && supportsEngineTableStructure(props.engine))
const fields = ref(null), target = ref(null), targetLabel = ref(''), error = ref(''), loading = ref(false), session = ref(0)
const adapter = shallowRef(null)
let request = 0, disposed = false
function clearFields() { request++; fields.value = null; target.value = null; targetLabel.value = ''; error.value = ''; loading.value = false }
const selectable = node => isStructureTableEntry(props.engine, node?.metadata?.catalog_entry)
const scopeKey = computed(() => {
  const caps = parseEngineCapabilities(props.engine), storage = caps?.storage
  return JSON.stringify([authorizationKey.value, props.engine.version, props.engine.engine_type,
    caps?.engine_family, storage?.catalog_model, storage?.catalog?.supported, storage?.catalog?.real_time,
    storage?.facts?.supported, storage?.facts?.field_info])
})
watch([scopeKey, allowed], () => {
  const generation = ++session.value
  clearFields()
  const { id, name, engine_type, lifecycle_state, connection_status, capabilities } = props.engine
  adapter.value = allowed.value ? createEngineCatalogTreeAdapter({ id, name, engine_type, lifecycle_state, connection_status, capabilities }, async (engineID, path) => {
    if (!allowed.value || disposed || generation !== session.value) throw new Error('catalogNotAvailable')
    const response = await enginesAPI.listCatalogChildren(engineID, path)
    if (!allowed.value || disposed || generation !== session.value) throw new Error('catalogNotAvailable')
    return response.nodes
  }) : null
}, { immediate: true, flush: 'sync' })
async function selectTable(selection) {
  clearFields()
  const entry = selection?.raw?.node?.metadata?.catalog_entry
  if (!allowed.value || !isStructureTableEntry(props.engine, entry)) return
  target.value = { term: entry.term, role: entry.role, kind: entry.kind, path: JSON.parse(serializeEngineCatalogTarget({ ...entry.path, engine_id: String(entry.path.engine_id) })) }
  targetLabel.value = entry.path.segments.filter(s => s.name).map(s => s.name).join(' / ')
  await loadFields()
}
async function loadFields() {
  const entry = target.value, generation = ++request
  fields.value = null; error.value = ''
  if (!entry || !allowed.value || disposed) return
  loading.value = true
  try {
    const facts = await enginesAPI.describeCatalogFacts(props.engine.id, entry.path)
    if (generation !== request || !allowed.value || disposed) return
    fields.value = tableStructureFields(props.engine, entry, facts)
  } catch {
    if (generation === request && allowed.value && !disposed) error.value = t('system.engine.tableStructure.failed')
  } finally { if (generation === request) loading.value = false }
}
onBeforeUnmount(() => { disposed = true; session.value++; clearFields() })
</script>

<style scoped>
.engine-table-structure { display: grid; grid-template-columns: minmax(200px, 30%) minmax(0, 1fr); gap: 16px; }
.structure-result { min-width: 0; min-height: 300px; }
.structure-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 36px; }
.structure-heading strong { overflow-wrap: anywhere; }
@media (max-width: 700px) { .engine-table-structure { grid-template-columns: minmax(0, 1fr); } }
</style>
