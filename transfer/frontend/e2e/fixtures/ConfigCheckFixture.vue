<script setup>
import { ref } from 'vue'
import ExecutionDetail from '../../src/views/ExecutionDetail.vue'
const executionMode = new URLSearchParams(window.location.search).has('execution')
import { useI18n } from 'vue-i18n'
import Step5Review from '../../src/views/TaskWizard/Step5Review.vue'
import Step1SelectSource from '../../src/views/TaskWizard/Step1SelectSource.vue'
import { useTaskWizardState } from '../../src/views/TaskWizard/useTaskWizardState'
import { useAuthStore } from '../../src/store/auth'
import { taskConfigCheckPermissions } from '../../src/views/TaskWizard/useTaskConfigCheck'

const auth = useAuthStore()
auth.bindAuthSession()
const { locale } = useI18n()
const submits = ref(0)
const sourceVisible = ref(false)
function setContext(permissions = taskConfigCheckPermissions, tenant = '900001', principal = '900001') {
  auth.authContext = {
    principal: { type: 'user', id: principal },
    context: { type: 'tenant', tenant_id: tenant },
    authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: tenant }, permissions }] }
  }
}
setContext()
const wizard = useTaskWizardState()
wizard.loadFromTask({
  name: 'synthetic readonly precheck', description: '', task_type: 'sync', batch_size: 1000, enabled: false, schedule: '',
  config: {
    runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' },
    source: { locator: 'addp://engine/9000001/path/synthetic/source?type=table', data_type: 'table', representation: 'native' },
    target: { parent_locator: 'addp://engine/9000002/path/synthetic?type=schema', name: 'precheck_target', data_type: 'table', representation: 'native', policy: { apply_mode: 'replace' } },
    transforms: [{ type: 'field_mapping', fields: [{ source: 'payload', target: 'payload', target_type: 'json', nullable: true }] }]
  }
}, { source: { engine_type: 'postgresql' }, target: { engine_type: 'postgresql' } })
function queryDraft(language) {
  wizard.loadFromTask({
    name: 'synthetic query restore', task_type: 'sync', enabled: false,
    config: {
      runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' },
      source: { locator: `addp://engine/9000001/path/synthetic/source?type=${language === 'mql' ? 'collection' : 'table'}`,
        data_type: 'table', representation: 'native',
        query: { language, statement: language === 'mql'
          ? '{"aggregate":"source","pipeline":[{"$match":{"payload":{"$param":"needle"}}},{"$project":{"_id":"$_id","payload":"$payload"}}]}'
          : 'SELECT payload FROM synthetic.source WHERE payload = :needle', parameters: { needle: 'synthetic-value' } } },
      target: { parent_locator: 'addp://engine/9000002/path/synthetic?type=schema', name: 'precheck_target', data_type: 'table', representation: 'native', policy: { apply_mode: 'replace' } },
      transforms: [{ type: 'field_mapping', fields: [{ source: 'payload', target: 'payload', target_type: 'json', nullable: true }] }]
    }
  }, { source: { engine_type: language === 'mql' ? 'mongodb' : 'postgresql' }, target: { engine_type: 'postgresql' } })
}
</script>
<template>
  <ExecutionDetail v-if="executionMode" />
  <div v-else>
    <label>Name <input data-testid="fixture-name" v-model="wizard.taskName.value"></label>
    <button v-for="permission in taskConfigCheckPermissions" :key="permission" :data-testid="`revoke-${permission}`"
      @click="setContext(taskConfigCheckPermissions.filter(item => item !== permission))">Revoke {{ permission }}</button>
    <button data-testid="restore-permissions" @click="setContext()">Restore permissions</button>
    <button data-testid="change-context" @click="setContext(taskConfigCheckPermissions, '900002')">Change tenant</button>
    <button data-testid="change-principal" @click="setContext(taskConfigCheckPermissions, '900001', '900002')">Change user</button>
    <button data-testid="english" @click="locale = 'en'">English</button>
    <button data-testid="query-draft-mql" @click="queryDraft('mql')">MQL draft</button>
    <button data-testid="query-draft-sql" @click="queryDraft('sql')">SQL draft</button>
    <button data-testid="source-step" @click="sourceVisible = true">Return to source</button>
    <button data-testid="review-step" @click="sourceVisible = false">Return to review</button>
    <output data-testid="fixture-submits">{{ submits }}</output>
    <pre data-testid="fixture-request" hidden>{{ JSON.stringify(wizard.taskConfig.value) }}</pre>
    <Step1SelectSource v-if="sourceVisible" :wizard-state="wizard" />
    <Step5Review v-else :wizard-state="wizard" @submit="submits++" />
  </div>
</template>
