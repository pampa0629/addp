<template>
  <aside class="workspace-panel" :aria-label="t('agent.workspace.panel')">
    <div class="panel-heading"><strong>{{ t('agent.workspace.panel') }}</strong><el-button text @click="emit('close')">{{ t('agent.workspace.close') }}</el-button></div>
    <el-tabs v-model="tab">
      <el-tab-pane :label="t('agent.workspace.process')" name="process">
        <template v-if="canReadRuns">
          <div class="run-controls">
            <el-select v-model="selectedRun" :placeholder="t('agent.workspace.selectRun')" @change="loadDetail">
              <el-option v-for="run in runs" :key="run.id" :value="run.id" :label="`${run.skill_name || t('agent.workspace.conversation')} · ${t(`agent.workspace.status.${run.status}`)} · ${new Date(run.created_at).toLocaleString()}`" />
            </el-select>
            <el-button :icon="RefreshRight" :aria-label="t('agent.workspace.refresh')" :loading="loading" @click="reload" />
          </div>
          <el-alert v-if="failed" :title="t('agent.workspace.runsFailed')" type="error" :closable="false" />
          <DataPagination size="small" :background="false" :pager-count="5" layout="prev, pager, next" v-model:current-page="page" :page-size="20" :total="total" @change="reload" />
          <template v-if="detail">
            <div class="run-summary"><el-tag>{{ t(`agent.workspace.status.${detail.status}`) }}</el-tag><code>{{ detail.id }}</code></div>
            <div v-if="statusExplanation" class="run-explanation" role="status">
              <p>{{ t(statusExplanation) }}</p>
              <el-button v-if="detail.status === 'waiting' && pending.length" text type="primary" @click="tab = 'information'">{{ t('agent.workspace.runExplanation.viewPending') }}</el-button>
            </div>
            <details v-if="detail.error_code" class="step-summary" :open="expandDetails">
              <summary>{{ t('agent.workspace.technicalDetails') }}</summary>
              <p class="run-error">{{ detail.error_source }} · {{ detail.error_code }} · {{ detail.error_message }}</p>
            </details>
            <AgentPlan v-if="detail.plan?.length" :plan="detail.plan" />
            <el-empty v-if="detail.steps.length === 0" :description="t('agent.workspace.noSteps')" :image-size="48" />
            <ol class="run-steps">
              <li v-for="step in detail.steps" :key="step.id">
                <strong>{{ step.tool_name || step.step_type }}</strong>
                <div><el-tag size="small">{{ t(`agent.workspace.status.${step.status}`) }}</el-tag><span v-if="stepDuration(step) !== null">{{ stepDuration(step) }} s</span></div>
                <p v-if="step.error_code" class="run-error">{{ step.error_source }} · {{ step.error_code }} · {{ step.error_message }}</p>
                <details v-else-if="step.output_summary" class="step-summary" :open="expandDetails"><summary>{{ t('agent.workspace.technicalDetails') }}</summary><p>{{ step.output_summary }}</p></details>
              </li>
            </ol>
          </template>
          <el-empty v-else-if="!loading && !failed" :description="t('agent.workspace.noRuns')" :image-size="48" />
        </template>
        <p v-else>{{ t('agent.workspace.runReadRequired') }}</p>
      </el-tab-pane>
      <el-tab-pane :label="`${t('agent.workspace.results')} (${results.length})`" name="results">
        <p>{{ t('agent.history.indexScope') }}</p>
        <el-alert v-if="indexes.results.failed" :title="t('agent.history.loadFailed')" type="error" :closable="false" />
        <el-empty v-if="!results.length" :description="t('agent.workspace.noResults')" :image-size="48" />
        <el-select v-model="resultFilter" :aria-label="t('agent.workspace.resultFilter')">
          <el-option v-for="kind in ['all', 'drafts', 'reports', 'other']" :key="kind" :value="kind" :label="t(`agent.workspace.resultFilters.${kind}`)" />
        </el-select>
        <button v-for="result in filteredResults" :key="result.key" class="result-link" type="button" @click="emit('locate', result.source)">
          <strong>{{ t(`agent.workspace.resultKinds.${result.kind}`) }}</strong><span v-if="result.label">{{ result.label }}</span><small>{{ t('agent.workspace.locate') }}</small>
        </button>
        <el-button v-if="indexes.results.cursor" :loading="indexes.results.loading" @click="loadIndex('results', true)">{{ t('agent.history.moreIndex') }}</el-button>
      </el-tab-pane>
      <el-tab-pane :label="t('agent.workspace.information')" name="information">
        <h3>{{ t('agent.workspace.goal') }}</h3>
        <p class="session-goal">{{ goal || t('agent.workspace.noGoal') }}</p>
        <h3>{{ t('agent.workspace.pending') }}</h3>
        <el-empty v-if="!pendingCount" :description="t('agent.workspace.noPending')" :image-size="48" />
        <el-alert v-if="indexes.pending.failed" :title="t('agent.history.loadFailed')" type="error" :closable="false" />
        <button v-for="item in pending" :key="item.interaction_id" class="result-link" type="button" @click="emit('locate', item.source)">{{ t(item.kind === 'owner_approval' ? 'agent.workspace.pendingApproval' : 'agent.workspace.pendingClarification') }}</button>
        <el-button v-if="indexes.pending.cursor" :loading="indexes.pending.loading" @click="loadIndex('pending', true)">{{ t('agent.history.moreIndex') }}</el-button>
        <h3>{{ t('agent.workspace.resources') }}</h3>
        <p>{{ t('agent.workspace.resourcesNotice') }}</p>
        <p>{{ t('agent.history.indexScope') }}</p>
        <el-alert v-if="indexes.resources.failed" :title="t('agent.history.loadFailed')" type="error" :closable="false" />
        <el-empty v-if="!resources.length" :description="t('agent.workspace.noResources')" :image-size="48" />
        <button v-for="item in resources" :key="item.locator" class="result-link" type="button" @click="emit('locate', item.source)"><span>{{ item.locator }}</span><small>{{ t('agent.workspace.locate') }}</small></button>
        <el-button v-if="indexes.resources.cursor" :loading="indexes.resources.loading" @click="loadIndex('resources', true)">{{ t('agent.history.moreIndex') }}</el-button>
      </el-tab-pane>
    </el-tabs>
  </aside>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshRight } from '@element-plus/icons-vue'
import { createLatestRequestScope, DataPagination } from '@common-ui'
import { runAPI, sessionAPI } from '../api'
import AgentPlan from './AgentPlan.vue'
import { stepDuration } from '../utils/workspace'
import { conversationReferences } from '../utils/history'

const props = defineProps({ sessionId: { type: String, required: true }, messages: { type: Array, default: () => [] }, workspace: Object, canReadRuns: Boolean, revision: Number, activeRunId: String, expandDetails: Boolean })
const emit = defineEmits(['locate', 'close'])
const { t } = useI18n()
const tab = ref('process'), runs = ref([]), selectedRun = ref(null), detail = ref(null), page = ref(1), total = ref(0), loading = ref(false), failed = ref(false)
const scope = createLatestRequestScope()
const resultFilter = ref('all')
const indexScopes = Object.fromEntries(['results', 'resources', 'pending'].map(kind => [kind, createLatestRequestScope()]))
const indexes = ref(Object.fromEntries(['results', 'resources', 'pending'].map(kind => [kind, { rows: [], cursor: null, loading: false, failed: false }])))
const goal = computed(() => props.workspace?.goal || '')
const resources = computed(() => conversationReferences(indexes.value.resources.rows, props.messages, 'resources'))
const pendingCount = computed(() => (props.workspace?.pending_clarifications || 0) + (props.workspace?.pending_approvals || 0))
const pending = computed(() => pendingCount.value ? conversationReferences(indexes.value.pending.rows, props.messages, 'pending') : [])
const statusExplanation = computed(() => {
  const run = detail.value
  if (run?.status === 'waiting') {
    const lastStep = run.steps.at(-1)
    return lastStep?.status === 'completed' && lastStep.tool_name === 'request_clarification' &&
      lastStep.input?.reason === 'transfer_source_selection'
      ? 'agent.workspace.runExplanation.sourceSelection'
      : 'agent.workspace.runExplanation.waiting'
  }
  if (run?.status === 'failed') {
    const unavailable = run.error_source === 'runtime' && run.error_code === 'runtime_exception' &&
      run.error_message?.startsWith('inference_upstream_unavailable:')
    return unavailable ? 'agent.workspace.runExplanation.inferenceUnavailable' : 'agent.workspace.runExplanation.failed'
  }
  return null
})
const filteredResults = computed(() => results.value.filter(result => resultFilter.value === 'all' || (resultFilter.value === 'drafts' ? ['WorkflowDag', 'TransferDraftSnapshot'].includes(result.kind) : resultFilter.value === 'reports' ? result.kind === 'TransferConfigCheckReport' : !['WorkflowDag', 'TransferDraftSnapshot', 'TransferConfigCheckReport'].includes(result.kind))))
const results = computed(() => conversationReferences(indexes.value.results.rows, props.messages, 'results'))

async function loadIndex(kind, append = false) {
  const scope = indexScopes[kind]
  const ticket = scope.invalidate()
  const state = indexes.value[kind]
  state.loading = true; state.failed = false
  try {
    const page = await scope.request(options => sessionAPI.messageIndex(props.sessionId, { kind, ...(append && state.cursor ? { cursor: state.cursor } : {}) }, options))
    if (!scope.current(ticket)) return
    state.rows = append ? [...state.rows, ...page.data] : page.data
    state.cursor = page.page.older_cursor
  } catch { if (scope.current(ticket)) state.failed = true }
  finally { if (scope.current(ticket)) state.loading = false }
}

function loadVisibleIndexes() {
  if (tab.value === 'results') loadIndex('results')
  if (tab.value === 'information') { loadIndex('pending'); loadIndex('resources') }
}
async function loadDetail() {
  const ticket = scope.invalidate(); detail.value = null; failed.value = false
  if (!selectedRun.value || !props.canReadRuns) { loading.value = false; return }
  loading.value = true
  try {
    const value = await scope.request(options => runAPI.get(selectedRun.value, options))
    if (scope.current(ticket)) detail.value = value
  } catch { if (scope.current(ticket)) failed.value = true }
  finally { if (scope.current(ticket)) loading.value = false }
}
async function reload() {
  const ticket = scope.invalidate(); failed.value = false
  if (!props.canReadRuns || !props.sessionId) { detail.value = null; runs.value = []; loading.value = false; return }
  loading.value = true
  try {
    const value = await scope.request(options => sessionAPI.runs(props.sessionId, { page: page.value, page_size: 20 }, options))
    if (!scope.current(ticket)) return
    runs.value = value.data; total.value = value.total
    selectedRun.value = runs.value.some(run => run.id === selectedRun.value) ? selectedRun.value : runs.value[0]?.id || null
    await loadDetail()
  } catch { if (scope.current(ticket)) { detail.value = null; failed.value = true } }
  finally { if (scope.current(ticket)) loading.value = false }
}
watch(() => props.sessionId, () => {
  for (const [kind, indexScope] of Object.entries(indexScopes)) {
    indexScope.invalidate()
    indexes.value[kind] = { rows: [], cursor: null, loading: false, failed: false }
  }
  page.value = 1; runs.value = []; detail.value = null; selectedRun.value = null; reload(); loadVisibleIndexes()
}, { immediate: true })
watch(tab, loadVisibleIndexes)
watch(() => props.revision, loadVisibleIndexes)
watch(() => props.canReadRuns, reload)
watch(() => props.revision, reload)
onBeforeUnmount(() => { scope.dispose(); Object.values(indexScopes).forEach(item => item.dispose()) })
</script>

<style scoped>
.workspace-panel { width: 340px; flex-shrink: 0; overflow: auto; padding: 8px 16px; border-left: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); color: var(--addp-text-primary); }
.run-controls { display: flex; align-items: center; gap: 8px; }
.run-controls .el-select { flex: 1; min-width: 0; }
.run-summary { display: flex; flex-direction: column; gap: 8px; margin: 16px 0; }
.run-summary code, .step-summary { overflow-wrap: anywhere; font-size: 12px; color: var(--addp-text-secondary); }
.run-explanation { padding: 8px 12px; background: var(--addp-bg-secondary); border-radius: 8px; margin-bottom: 8px; }
.run-explanation p { margin: 0; overflow-wrap: anywhere; }
.run-steps { padding-left: 20px; }
.run-steps li { padding: 8px 0; border-bottom: 1px solid var(--addp-border-color); overflow-wrap: anywhere; }
.run-steps li div { display: flex; gap: 8px; margin-top: 8px; }
.run-error { color: var(--el-color-danger); overflow-wrap: anywhere; }
.session-goal { white-space: pre-wrap; overflow-wrap: anywhere; }
.result-link { width: 100%; display: flex; flex-direction: column; gap: 8px; padding: 16px; margin-bottom: 8px; background: var(--addp-bg-secondary); border: 1px solid var(--addp-border-color); border-radius: 8px; color: inherit; text-align: left; cursor: pointer; overflow-wrap: anywhere; }
.result-link small { color: var(--el-color-primary); }
@media (max-width: 1050px) { .workspace-panel { width: 280px; } }
@media (max-width: 760px) { .workspace-panel { position: absolute; right: 0; top: 48px; bottom: 0; width: min(340px, 90%); z-index: 10; box-shadow: var(--addp-shadow-base); } }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
</style>
