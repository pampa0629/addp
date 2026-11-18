<template>
  <section data-testid="infra-configuration" v-loading="loading">
    <div class="infra-toolbar">
      <el-select v-model="selectedID" :aria-label="label('component')" :disabled="saving" data-testid="infra-component">
        <el-option v-for="item in components" :key="item.id" :label="componentName(item)" :value="item.id" />
      </el-select>
      <el-button :disabled="saving" @click="reload">{{ label('refresh') }}</el-button>
      <span v-if="selected?.observed_at" class="observation-time">
        {{ label('observedAt', { time: new Date(selected.observed_at).toLocaleString(locale) }) }}
      </span>
    </div>
    <el-alert v-if="lastOutcome" data-testid="infra-outcome" :type="outcomeType"
      :title="label('result', { name: lastOutcome.name })" :closable="false" show-icon>
      <p>{{ label(`application.${lastOutcome.application}`) }} · {{ label(`persistence.${lastOutcome.persistence}`) }}</p>
      <p v-if="lastOutcome.current">{{ label('verifiedValue', { value: lastOutcome.current.value, unit: lastOutcome.current.unit }) }}</p>
      <p v-if="lastOutcome.failure_stage">{{ label(`stage.${lastOutcome.failure_stage}`) }}</p>
      <p v-if="!lastOutcome.audit_recorded">{{ label('completionAuditFailed') }}</p>
    </el-alert>
    <el-alert v-if="loadFailed" type="error" :title="label('loadFailed')" :closable="false" />
    <el-alert v-else-if="selected && selected.status !== 'available'" type="info"
      :title="label(`status.${selected.status}`)" :closable="false" />
    <template v-else-if="selected">
      <el-table :data="selected.parameters" row-key="name">
        <el-table-column :label="label('parameter')" min-width="250">
          <template #default="{ row }">
            <div>{{ row.name }}</div>
            <small>{{ label(`description.${row.name}`) }}</small>
          </template>
        </el-table-column>
        <el-table-column :label="label('value')" min-width="120">
          <template #default="{ row }">{{ row.value === '' ? label('empty') : row.value }} {{ row.unit }}</template>
        </el-table-column>
        <el-table-column :label="label('source')" min-width="125">
          <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
        </el-table-column>
        <el-table-column :label="label('activation')" min-width="120">
          <template #default="{ row }">{{ label(row.pending_restart ? 'pendingRestart' : `activationMode.${row.activation}`) }}</template>
        </el-table-column>
        <el-table-column :label="label('actions')" width="100">
          <template #default="{ row }">
            <el-button v-if="row.editable && canUpdate" text type="primary" :disabled="saving"
              :data-testid="`edit-${row.name}`" @click="edit(row)">{{ label('edit') }}</el-button>
            <span v-else>{{ label('readOnly') }}</span>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <el-dialog v-model="editing" :title="label('editTitle', { name: draft?.name || '' })" width="min(480px, 90vw)"
      :close-on-click-modal="!saving" :close-on-press-escape="!saving" :show-close="!saving">
      <el-form v-if="draft" label-position="top" @submit.prevent="apply">
        <el-alert v-if="saveError" type="error" :title="saveError" :closable="false" />
        <el-form-item :label="draft.unit ? label('newValueWithUnit', { unit: draft.unit }) : label('newValue')">
          <el-select v-if="draft.name === 'log_lock_waits'" v-model="draft.value" :disabled="saving">
            <el-option :label="label('on')" value="on" />
            <el-option :label="label('off')" value="off" />
          </el-select>
          <el-input v-else v-model="draft.value" inputmode="numeric" :maxlength="32"
            :disabled="saving" data-testid="infra-parameter-value" />
        </el-form-item>
        <el-alert v-if="diagnosticHint" type="info" :title="diagnosticHint" :closable="false" />
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="editing = false">{{ label('cancel') }}</el-button>
        <el-button type="primary" :disabled="!canUpdate || !draft?.value.trim()"
          :loading="saving" data-testid="infra-apply" @click="apply">{{ label('apply') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import client, { explicitWriteClient } from '../../api/client'
import { useAuthStore } from '../../store/auth'

const endpoint = '/system/platform/infra_configuration'
const { t, te, locale } = useI18n()
const auth = useAuthStore()
const components = ref([])
const selectedID = ref('')
const loading = ref(false)
const saving = ref(false)
const loadFailed = ref(false)
const editing = ref(false)
const draft = ref(null)
const saveError = ref('')
const lastOutcome = ref(null)
const selected = computed(() => components.value.find(item => item.id === selectedID.value))
const canUpdate = computed(() => auth.hasPermission('system.infra_configuration.update'))
const outcomeType = computed(() => lastOutcome.value?.application === 'confirmed' &&
  lastOutcome.value?.persistence === 'confirmed' && lastOutcome.value?.audit_recorded ? 'success' : 'warning')
const diagnosticHint = computed(() => {
  const hints = {
    log_min_duration_statement: 'slowSQL',
    'slowlog-log-slower-than': 'slowCommand',
    'slowlog-max-len': 'slowLogLength',
    work_mem: 'workMem',
    maintenance_work_mem: 'maintenanceMem'
  }
  const hint = hints[draft.value?.name]
  return hint ? label(hint) : ''
})
let readSequence = 0
let contextSequence = 0

function label(key, values = {}) {
  return t(`system.infraConfiguration.${key}`, values)
}
function componentName(component) {
  if (component.provider === 'postgres') return 'PostgreSQL'
  if (component.provider === 'redis') return 'Redis'
  return component.provider
}
function sourceLabel(source) {
  const key = `system.infraConfiguration.sources.${source.replaceAll(' ', '_')}`
  return te(key) ? t(key) : label('sources.other')
}
async function reload() {
  const sequence = ++readSequence
  loading.value = true
  loadFailed.value = false
  try {
    const values = await client.get(endpoint)
    if (sequence !== readSequence) return
    components.value = values
    if (!values.some(item => item.id === selectedID.value)) selectedID.value = values[0]?.id || ''
  } catch {
    if (sequence !== readSequence) return
    components.value = []
    loadFailed.value = true
  } finally {
    if (sequence === readSequence) loading.value = false
  }
}
function edit(row) {
  draft.value = { ...row }
  saveError.value = ''
  editing.value = true
}
async function apply() {
  if (saving.value || !canUpdate.value || !draft.value?.value.trim()) return
  const context = contextSequence
  const { name, value } = draft.value
  saving.value = true
  saveError.value = ''
  try {
    const result = await explicitWriteClient.post(
      `${endpoint}/${encodeURIComponent(selectedID.value)}/parameters/${encodeURIComponent(name)}/apply`,
      { value: value.trim() }
    )
    if (context !== contextSequence) return
    lastOutcome.value = { ...result, name }
    editing.value = false
    await reload()
  } catch (error) {
    if (context === contextSequence) saveError.value = error.response?.data?.error || label('saveFailed')
  } finally {
    if (context === contextSequence) saving.value = false
  }
}
watch(selectedID, () => { lastOutcome.value = null })
watch(() => JSON.stringify([auth.authContext?.principal?.id, auth.authContext?.context]), () => {
  contextSequence++
  components.value = []
  selectedID.value = ''
  saving.value = false
  editing.value = false
  lastOutcome.value = null
  reload()
})
onMounted(reload)
onBeforeUnmount(() => { readSequence++; contextSequence++ })
</script>

<style scoped>
.infra-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.infra-toolbar .el-select { width: min(240px, 100%); }
.observation-time, small { color: var(--addp-text-secondary); }
.el-table, .el-form-item { margin-top: 12px; }
.el-alert p { margin: 0; }
</style>
