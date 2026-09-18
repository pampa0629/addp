<template>
  <main class="page">
    <header class="toolbar">
      <el-button
        :disabled="busy"
        @click="go(isNew ? '/ontologies' : `/ontologies/${id}`)"
      >
        {{ t('ontology.back') }}
      </el-button>
      <h2>
        {{
          isNew
            ? t('ontology.create')
            : t('ontology.revisionTitle', { id, revision: number })
        }}
      </h2>
      <el-tag v-if="record">
        {{ t(`ontology.statuses.${record.status}`) }}
      </el-tag>
      <el-tag v-if="dirty" type="warning">{{ t('ontology.unsaved') }}</el-tag>
    </header>
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
      show-icon
    />
    <el-alert
      v-if="locked"
      :title="t('ontology.reconcile')"
      type="warning"
      :closable="false"
      show-icon
    />
    <el-alert v-if="notice" :title="notice" type="info" :closable="false" />
    <StatusAnnouncer :message="busy ? t('ontology.saving') : notice" />
    <div v-if="canRead" class="toolbar">
      <el-button v-if="!isNew" :disabled="busy || loading" @click="refresh">
        {{ t('ontology.reload') }}
      </el-button>
      <el-button
        v-if="isNew && locked"
        :disabled="busy"
        @click="go(`/ontologies/${id}`)"
      >
        {{ t('ontology.inspectResult') }}
      </el-button>
      <el-button
        v-if="editable"
        type="primary"
        :loading="busy"
        :disabled="!ready || locked || loading || (!isNew && !dirty)"
        @click="save"
      >
        {{ isNew ? t('ontology.createDraft') : t('ontology.save') }}
      </el-button>
      <el-button
        v-for="action in actions"
        :key="action"
        :disabled="busy || loading || locked || dirty"
        :type="action === 'withdraw' ? 'danger' : 'default'"
        @click="transition(action)"
      >
        {{ t(`ontology.actions.${action}`) }}
      </el-button>
    </div>
    <template v-if="ready">
      <el-form v-if="isNew" label-position="top">
        <el-form-item :label="t('ontology.id')" required>
          <el-input
            v-model="id"
            :disabled="busy || locked"
            maxlength="64"
            :placeholder="t('ontology.idHint')"
          />
        </el-form-item>
      </el-form>
      <el-descriptions v-else border :column="1">
        <el-descriptions-item :label="t('ontology.version')">
          {{ record.version }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('ontology.activeRevision')">
          {{ head?.active_revision ?? t('ontology.inactive') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('ontology.projection')">
          <el-tag v-if="projection" :type="active ? 'success' : 'info'">
            {{
              active
                ? t('ontology.active')
                : t(`ontology.projectionStatuses.${projection.status}`)
            }}
          </el-tag>
          <span v-else>{{ t('ontology.noProjection') }}</span>
        </el-descriptions-item>
        <el-descriptions-item
          v-if="projection"
          :label="t('ontology.generation')"
        >
          <span class="identifier">{{ projection.generation }}</span>
        </el-descriptions-item>
      </el-descriptions>
      <DefinitionEditor
        v-model="definition"
        :disabled="!editable || busy || locked || loading"
        :tab="tab"
        @update:tab="setTab"
      />
    </template>
  </main>
</template>
<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  useRoute,
  useRouter,
  onBeforeRouteLeave,
  onBeforeRouteUpdate
} from 'vue-router'
import { ElMessageBox } from 'element-plus'
import {
  StatusAnnouncer,
  navigateConsoleModuleRoute,
  useUnsavedChangesGuard
} from '@common-ui'
import { useAuthStore } from '../store/auth'
import client from '../api/client'
import { createOntologyAPI } from '../api/ontology.mjs'
import DefinitionEditor from '../components/DefinitionEditor.vue'
import {
  sections,
  emptyDefinition,
  definitionInput,
  validID,
  positiveNumber,
  availableActions,
  isActive,
  requiresReconciliation
} from '../utils/definition.mjs'

const { t } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  auth = useAuthStore(),
  api = createOntologyAPI(client)
const isNew = computed(() => route.path === '/ontologies/new')
const number = computed(() => Number(route.params.revision))
const id = ref(''),
  record = ref(null),
  head = ref(null),
  projection = ref(null),
  definition = ref(emptyDefinition())
const baseline = ref(''),
  ready = ref(false),
  loading = ref(false),
  busy = ref(false),
  locked = ref(false),
  error = ref(''),
  notice = ref('')
const tab = computed(() =>
  sections.includes(route.query.tab) ? route.query.tab : 'classes'
)
const canRead = computed(
  () =>
    auth.contextType === 'tenant' &&
    auth.hasPermission('ontology.revision.read')
)
const allowed = computed(() =>
  availableActions(record.value, projection.value, auth.permissions)
)
const editable = computed(
  () =>
    canRead.value &&
    auth.hasPermission('ontology.revision.update') &&
    (isNew.value || allowed.value.save)
)
const actions = computed(() =>
  Object.keys(allowed.value).filter(
    (action) => action !== 'save' && allowed.value[action] && canRead.value
  )
)
const fingerprint = () =>
  JSON.stringify({ id: id.value, definition: definition.value })
const dirty = computed(() => ready.value && fingerprint() !== baseline.value)
const active = computed(() => isActive(head.value, projection.value))
let request = 0
const go = (path, query = {}, history = 'push') =>
  navigateConsoleModuleRoute(
    router,
    'ontology',
    { path, query },
    { history }
  ).catch((e) => {
    error.value = e.message
  })
const setTab = (value) =>
  go(route.path, value === 'classes' ? {} : { tab: value }, 'replace')
useUnsavedChangesGuard({
  router,
  isDirty: () => dirty.value || busy.value,
  shouldConfirmUpdate: (to, from) => to.path !== from.path
})
onBeforeRouteLeave(() => !busy.value)
onBeforeRouteUpdate((to, from) => !busy.value || to.path === from.path)
const confirm = (message, title, danger = false) =>
  ElMessageBox.confirm(message, title, {
    customClass: 'addp-message-box',
    confirmButtonText: t('ontology.confirm'),
    cancelButtonText: t('ontology.cancel'),
    ...(danger
      ? {
          type: 'warning',
          confirmButtonClass: 'el-button--danger',
          autofocus: false
        }
      : {})
  }).then(
    () => true,
    () => false
  )

async function load() {
  const current = ++request
  const ontologyID = route.params.ontology_id
  const revisionNumber = Number(route.params.revision)
  ready.value = false
  loading.value = true
  record.value = null
  head.value = null
  projection.value = null
  error.value = ''
  try {
    if (!canRead.value) {
      error.value = t('ontology.noAccess')
      return
    }
    if (isNew.value) {
      id.value = ''
      definition.value = emptyDefinition()
    } else {
      id.value = ontologyID
      if (!validID(id.value) || !positiveNumber(route.params.revision)) {
        error.value = t('ontology.invalidRoute')
        return
      }
      const [r, h] = await Promise.all([
        api.get(ontologyID, revisionNumber),
        api.head(ontologyID)
      ])
      if (current !== request) return
      const p = r.initial_generation
        ? await api.projection(ontologyID, revisionNumber)
        : null
      if (current !== request) return
      record.value = r
      head.value = h
      projection.value = p
      definition.value = definitionInput(r.snapshot.definition)
    }
    baseline.value = fingerprint()
    ready.value = true
    locked.value = false
  } catch (e) {
    if (current === request)
      error.value = e.response?.data?.error || t('ontology.failed')
  } finally {
    if (current === request) loading.value = false
  }
}
async function refresh() {
  if (busy.value || loading.value) return
  if (
    dirty.value &&
    !(await confirm(t('ontology.confirmReload'), t('ontology.reload'), true))
  )
    return
  await load()
}
function failed(e) {
  error.value = e.response?.data?.error || t('ontology.failed')
  locked.value = requiresReconciliation(e)
}
async function save() {
  if (
    !editable.value ||
    !ready.value ||
    busy.value ||
    locked.value ||
    loading.value
  )
    return
  if (!validID(id.value)) {
    error.value = t('ontology.idHint')
    return
  }
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = isNew.value
      ? await api.create(id.value, 1, definitionInput(definition.value))
      : await api.save(
          id.value,
          number.value,
          record.value.version,
          definitionInput(definition.value)
        )
    record.value = result
    definition.value = definitionInput(result.snapshot.definition)
    baseline.value = fingerprint()
    notice.value = t('ontology.saved')
    if (isNew.value) ready.value = false
    busy.value = false
    if (isNew.value)
      await go(
        `/ontologies/${result.ontology_id}/revisions/${result.revision}`,
        {},
        'replace'
      )
  } catch (e) {
    failed(e)
  } finally {
    busy.value = false
  }
}
async function transition(action) {
  if (
    !allowed.value[action] ||
    dirty.value ||
    busy.value ||
    locked.value ||
    loading.value
  )
    return
  busy.value = true
  try {
    if (
      !(await confirm(
        t(`ontology.confirmations.${action}`),
        t(`ontology.actions.${action}`),
        action === 'withdraw'
      ))
    )
      return
    error.value = ''
    notice.value = ''
    const body = { version: record.value.version }
    if (action === 'rebuild')
      Object.assign(body, {
        failed_generation: projection.value.generation,
        activation_version: head.value.activation_version
      })
    await api.transition(id.value, number.value, action, body)
    // Disable writes until the authoritative revision/head are read again, even if the read fails.
    locked.value = true
    notice.value = t(
      action === 'publish' || action === 'rebuild'
        ? 'ontology.accepted'
        : 'ontology.saved'
    )
    await load()
  } catch (e) {
    failed(e)
  } finally {
    busy.value = false
  }
}
watch(() => route.path, load, { immediate: true })
watch(
  () => route.fullPath,
  () => {
    const canonical = tab.value === 'classes' ? {} : { tab: tab.value }
    if (JSON.stringify(route.query) !== JSON.stringify(canonical))
      go(route.path, canonical, 'replace')
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  request++
})
</script>
<style scoped>
.page {
  padding: 24px;
  display: grid;
  gap: 16px;
}
.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
h2 {
  margin: 0 8px;
  font-size: 20px;
  color: var(--addp-text-primary);
  overflow-wrap: anywhere;
}
.identifier {
  overflow-wrap: anywhere;
}
.el-descriptions {
  min-width: 0;
}
@media (max-width: 768px) {
  .page {
    padding: 16px;
  }
}
</style>
