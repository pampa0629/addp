<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Step5Review from '../../src/views/TaskWizard/Step5Review.vue'
import { useTaskWizardState } from '../../src/views/TaskWizard/useTaskWizardState'
import { useAuthStore } from '../../src/store/auth'
import { taskConfigCheckPermissions } from '../../src/views/TaskWizard/useTaskConfigCheck'

const auth = useAuthStore()
const { locale } = useI18n()
const submits = ref(0)
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
</script>
<template>
  <div>
    <label>Name <input data-testid="fixture-name" v-model="wizard.taskName.value"></label>
    <button v-for="permission in taskConfigCheckPermissions" :key="permission" :data-testid="`revoke-${permission}`"
      @click="setContext(taskConfigCheckPermissions.filter(item => item !== permission))">Revoke {{ permission }}</button>
    <button data-testid="restore-permissions" @click="setContext()">Restore permissions</button>
    <button data-testid="change-context" @click="setContext(taskConfigCheckPermissions, '900002')">Change tenant</button>
    <button data-testid="change-principal" @click="setContext(taskConfigCheckPermissions, '900001', '900002')">Change user</button>
    <button data-testid="english" @click="locale = 'en'">English</button>
    <output data-testid="fixture-submits">{{ submits }}</output>
    <pre data-testid="fixture-request" hidden>{{ JSON.stringify(wizard.taskConfig.value) }}</pre>
    <Step5Review :wizard-state="wizard" @submit="submits++" />
  </div>
</template>
