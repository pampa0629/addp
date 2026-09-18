<template>
  <main class="page" v-loading="loading || reauthorizing">
    <header class="toolbar">
      <el-button @click="go(`/ontologies/${route.params.ontology_id}`)">{{
        t('ontology.back')
      }}</el-button>
      <h2>{{ t('ontology.trial.title') }} · {{ route.params.ontology_id }}</h2>
      <el-button :disabled="loading || reauthorizing" @click="load">{{
        t('ontology.reload')
      }}</el-button>
    </header>
    <el-alert
      :title="t('ontology.trial.notice')"
      type="info"
      :closable="false"
    />
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert
      v-if="blocked"
      :title="t('ontology.trial.changed')"
      type="warning"
      :closable="false"
    />
    <template v-if="directory && allowed">
      <el-descriptions border :column="1">
        <el-descriptions-item :label="t('ontology.activeRevision')">{{
          directory.revision
        }}</el-descriptions-item>
        <el-descriptions-item :label="t('ontology.activeGeneration')">{{
          directory.generation
        }}</el-descriptions-item>
        <el-descriptions-item :label="t('ontology.trial.activationVersion')">{{
          directory.activation_version
        }}</el-descriptions-item>
        <el-descriptions-item :label="t('ontology.trial.digest')"
          ><span class="identifier">{{
            directory.digest
          }}</span></el-descriptions-item
        >
      </el-descriptions>
      <el-form label-position="top">
        <el-form-item :label="t('ontology.class')">
          <el-select
            :model-value="route.query.class_id || ''"
            :disabled="loading || blocked"
            @change="selectClass"
          >
            <el-option
              v-for="item in directory.classes"
              :key="item.id"
              :value="item.id"
              :label="`${item.name} (${item.id})`"
            />
          </el-select>
        </el-form-item>
        <el-form-item v-if="context" :label="t('ontology.trial.rule')">
          <el-select
            :model-value="route.query.rule_id || ''"
            :disabled="loading || blocked"
            @change="selectRule"
          >
            <el-option
              v-for="item in context.rules"
              :key="item.id"
              :value="item.id"
              :label="item.id"
            />
          </el-select>
        </el-form-item>
        <el-empty
          v-if="context && !context.rules.length"
          :description="t('ontology.trial.noRules')"
        />
        <template v-if="rule">
          <pre class="expression">{{ rule.expression }}</pre>
          <p class="basis">{{ rule.basis }}</p>
          <fieldset
            v-for="input in inputs"
            :key="input.variable"
            class="input-group"
            data-testid="trial-input"
          >
            <legend>{{ input.property.name }} · {{ input.variable }}</legend>
            <el-form-item :label="t('ontology.trial.inputState')">
              <el-select v-model="input.state" :disabled="blocked">
                <el-option
                  v-for="state in states"
                  :key="state"
                  :value="state"
                  :label="t(`ontology.trial.states.${state}`)"
                />
              </el-select>
            </el-form-item>
            <el-form-item
              v-if="input.state === 'known'"
              :label="t('ontology.trial.value')"
            >
              <ParameterValueInput
                v-model="input.value"
                :disabled="blocked"
                :control-type="
                  input.property.kind === 'bool' ? 'select' : 'text'
                "
                :options="trialOptions(input.property)"
              />
            </el-form-item>
            <span class="hint"
              >{{ t('ontology.onAbsent') }}：{{
                t(
                  input.on_absent === 'false'
                    ? 'ontology.false'
                    : 'ontology.unknown'
                )
              }}</span
            >
          </fieldset>
          <el-button
            type="primary"
            :loading="busy"
            :disabled="blocked || loading"
            @click="run"
            >{{ t('ontology.trial.run') }}</el-button
          >
        </template>
      </el-form>
    </template>
    <StatusAnnouncer
      :message="
        busy
          ? t('ontology.trial.running')
          : result
            ? t(`ontology.trial.outcomes.${result.decision.outcome}`)
            : ''
      "
    />
    <section v-if="result" class="result" data-testid="trial-result">
      <h3>{{ t('ontology.trial.result') }}</h3>
      <el-alert
        :title="t(`ontology.trial.outcomes.${result.decision.outcome}`)"
        :description="t(`ontology.trial.codes.${result.decision.code}`)"
        :type="outcomeType[result.decision.outcome]"
        :closable="false"
      />
      <p>{{ t('ontology.trial.hypothetical') }}</p>
      <p>{{ t('ontology.trial.rule') }}：{{ result.decision.rule_id }}</p>
      <ul>
        <li v-for="item in result.decision.inputs || []" :key="item.variable">
          {{ item.variable }} —
          {{ t(`ontology.trial.states.${item.state}`) }}；{{
            t(`ontology.trial.treatments.${item.treatment}`)
          }}
        </li>
      </ul>
      <p v-if="result.decision.unresolved?.length">
        {{ t('ontology.trial.unresolved') }}：{{
          result.decision.unresolved.join(', ')
        }}
      </p>
    </section>
  </main>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  navigateConsoleModuleRoute,
  ParameterValueInput,
  StatusAnnouncer,
  useConsolePageDescriptor
} from '@common-ui'
import { useAuthStore } from '../store/auth'
import client from '../api/client'
import { createOntologyAPI } from '../api/ontology.mjs'
import { validID } from '../utils/definition.mjs'
import {
  sameBinding,
  trialInputs,
  trialOwner,
  trialOptions,
  trialRequest
} from '../utils/trial.mjs'

const route = useRoute(),
  router = useRouter(),
  auth = useAuthStore(),
  { t } = useI18n(),
  api = createOntologyAPI(client)
const directory = ref(null),
  context = ref(null),
  inputs = ref([]),
  result = ref(null)
const loading = ref(false),
  busy = ref(false),
  blocked = ref(false),
  error = ref('')
const states = ['known', 'absent', 'unknown', 'invalid']
const outcomeType = {
  matched: 'success',
  not_matched: 'info',
  unknown: 'warning',
  error: 'error'
}
const owner = computed(() => trialOwner(auth.authContext))
const reauthorizing = computed(() =>
  Boolean(auth.token && !auth.authContext && auth.authContextLoadPromise)
)
const allowed = computed(
  () => Boolean(owner.value) && auth.hasPermission('ontology.semantic.read')
)
const rule = computed(() =>
  context.value?.rules.find((item) => item.id === route.query.rule_id)
)
let loadTicket = 0,
  trialTicket = 0,
  controller,
  loadedOwner = '',
  loadedRoute = ''
function clearResult() {
  trialTicket++
  controller?.abort()
  busy.value = false
  result.value = null
}
const go = (path, query = {}) =>
  navigateConsoleModuleRoute(router, 'ontology', { path, query }).catch(() => {
    error.value = t('ontology.failed')
  })
const selectClass = (classID) => go(route.path, { class_id: classID })
const selectRule = (ruleID) =>
  go(route.path, { class_id: route.query.class_id, rule_id: ruleID })
useConsolePageDescriptor(router, 'ontology', {
  title: computed(() => t('ontology.trial.title')),
  subject: computed(() =>
    allowed.value ? directory.value?.ontology_id || '' : ''
  ),
  ready: computed(() => allowed.value && Boolean(directory.value))
})
async function load() {
  const ticket = ++loadTicket
  const requestOwner = owner.value,
    requestRoute = route.fullPath
  clearResult()
  directory.value = null
  context.value = null
  inputs.value = []
  blocked.value = false
  error.value = ''
  loading.value = true
  try {
    const id = route.params.ontology_id,
      { class_id: classID, rule_id: ruleID } = route.query
    if (!allowed.value) {
      error.value = t('ontology.trial.noAccess')
      return
    }
    if (
      !validID(id) ||
      Object.keys(route.query).some(
        (key) => !['class_id', 'rule_id'].includes(key)
      ) ||
      (classID !== undefined && !validID(classID)) ||
      (ruleID !== undefined && (!classID || !validID(ruleID)))
    ) {
      error.value = t('ontology.invalidRoute')
      return
    }
    const binding = await api.classes(id)
    let detail = null,
      nextInputs = []
    if (classID) {
      if (!binding.classes.some((item) => item.id === classID))
        throw new Error('unknown_class')
      detail = await api.classContext(id, classID, binding)
      if (!sameBinding(binding, detail)) throw new Error('binding_mismatch')
      if (ruleID) {
        const selected = detail.rules.find((item) => item.id === ruleID)
        if (!selected) throw new Error('unknown_rule')
        nextInputs = trialInputs(selected, detail.properties)
      }
    }
    if (ticket !== loadTicket) return
    directory.value = binding
    context.value = detail
    inputs.value = nextInputs
    loadedOwner = requestOwner
    loadedRoute = requestRoute
  } catch (e) {
    if (ticket === loadTicket)
      error.value = e.response?.data?.error || t('ontology.failed')
  } finally {
    if (ticket === loadTicket) loading.value = false
  }
}
async function run() {
  if (
    busy.value ||
    loading.value ||
    blocked.value ||
    !rule.value ||
    !allowed.value
  )
    return
  clearResult()
  const ticket = trialTicket,
    binding = directory.value,
    ruleID = rule.value.id
  controller = new AbortController()
  busy.value = true
  error.value = ''
  try {
    const value = await api.trial(
      binding.ontology_id,
      ruleID,
      trialRequest(binding, inputs.value),
      controller.signal
    )
    if (ticket !== trialTicket) return
    if (
      !sameBinding(binding, value) ||
      value.decision.mode !== 'hypothetical' ||
      value.decision.rule_id !== ruleID ||
      value.decision.digest !== binding.digest
    )
      throw new Error('binding_mismatch')
    result.value = value
  } catch (e) {
    if (ticket !== trialTicket) return
    error.value = e.response?.data?.error || t('ontology.failed')
    blocked.value =
      [401, 403, 404, 409].includes(e.response?.status) ||
      e.message === 'binding_mismatch'
  } finally {
    if (ticket === trialTicket) busy.value = false
  }
}
watch(inputs, clearResult, { deep: true, flush: 'sync' })
watch(
  [
    () => route.fullPath,
    owner,
    allowed,
    reauthorizing,
    () => Boolean(auth.token)
  ],
  () => {
    // Token rotation briefly removes AuthContext. Suspend consumption without
    // treating that gap as a new definition selection or retaining a conclusion.
    loadTicket++
    clearResult()
    loading.value = false
    if (reauthorizing.value) return
    if (!allowed.value) {
      directory.value = null
      context.value = null
      inputs.value = []
      loadedOwner = ''
      loadedRoute = ''
      blocked.value = false
      error.value = t('ontology.trial.noAccess')
      return
    }
    if (
      !directory.value ||
      loadedOwner !== owner.value ||
      loadedRoute !== route.fullPath
    )
      load()
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  loadTicket++
  clearResult()
})
</script>

<style scoped>
.page {
  padding: 24px;
  display: grid;
  gap: 16px;
  min-width: 0;
}
.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px;
}
h2,
h3 {
  margin: 0;
  color: var(--addp-text-primary);
  overflow-wrap: anywhere;
}
.identifier,
.basis,
.result {
  overflow-wrap: anywhere;
}
.expression {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  padding: 16px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
}
.input-group {
  min-width: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  margin: 16px 0;
  padding: 16px;
}
.hint {
  color: var(--addp-text-secondary);
  font-size: 12px;
}
.result {
  display: grid;
  gap: 8px;
}
</style>
