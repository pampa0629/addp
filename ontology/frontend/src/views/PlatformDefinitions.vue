<template>
  <main class="page">
    <header class="toolbar">
      <h2>{{ t('ontology.platform.title') }}</h2>
      <el-button v-if="route.params.capability" @click="go('')">{{ t('ontology.back') }}</el-button>
      <el-button :disabled="!allowed || loading" @click="load">{{ t('ontology.reload') }}</el-button>
    </header>
    <section v-loading="loading || reauthorizing" class="content">
    <el-alert :title="t('ontology.platform.notice')" type="info" :closable="false" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="allowed && !reauthorizing">
      <el-table v-if="catalog" :data="catalog.capabilities" data-testid="platform-catalog">
        <el-table-column :label="t('ontology.platform.capability')">
          <template #default="{ row }"><el-button link type="primary" @click="go(row.capability)">{{ row.capability }}</el-button></template>
        </el-table-column>
        <el-table-column prop="operation.owner" :label="t('ontology.platform.owner')" />
        <el-table-column prop="revision" :label="t('ontology.revision')" />
      </el-table>
      <template v-if="release">
        <el-descriptions border :column="1" data-testid="platform-release">
          <el-descriptions-item :label="t('ontology.platform.capability')">{{ definition.capability }}</el-descriptions-item>
          <el-descriptions-item :label="t('ontology.revision')">{{ definition.revision }}</el-descriptions-item>
          <el-descriptions-item :label="t('ontology.trial.digest')"><code>{{ definition.digest }}</code></el-descriptions-item>
          <el-descriptions-item :label="t('ontology.platform.owner')">{{ definition.operation.owner }}</el-descriptions-item>
          <el-descriptions-item label="Skill">{{ definition.operation.skill }}</el-descriptions-item>
          <el-descriptions-item label="Tool">{{ definition.operation.tool }}</el-descriptions-item>
        </el-descriptions>
        <h3>{{ t('ontology.platform.graph') }}</h3>
        <div ref="graphContainer" class="graph-frame" data-testid="platform-graph">
          <OntologyView :entity-types="graph.entityTypes" :relation-types="graph.relationTypes" readonly @node-click="selectConcept" />
        </div>
        <h3>{{ t('ontology.platform.concepts') }}</h3>
        <el-table :data="definition.concepts">
          <el-table-column prop="id" :label="t('ontology.platform.identifier')" />
          <el-table-column :label="t('ontology.name')"><template #default="{ row }">{{ row.name[locale] }}</template></el-table-column>
        </el-table>
        <h3>{{ t('ontology.platform.conditions') }}</h3>
        <ul><li v-for="id in definition.operation.inputs_required" :key="id">{{ conceptName(id) }} <code>{{ id }}</code></li></ul>
        <h3>{{ t('ontology.platform.effects') }}</h3>
        <ul><li v-for="id in definition.operation.effects" :key="id"><code>{{ id }}</code></li></ul>
        <h3>{{ t('ontology.platform.excluded') }}</h3>
        <ul><li v-for="id in definition.operation.excluded_effects" :key="id"><code>{{ id }}</code></li></ul>
        <h3>{{ t('ontology.platform.requirements') }}</h3>
        <el-table :data="definition.requirements">
          <el-table-column prop="id" :label="t('ontology.platform.identifier')" />
          <el-table-column label="Tools"><template #default="{ row }">{{ row.tools.join(', ') }}</template></el-table-column>
        </el-table>
        <h3>{{ t('ontology.platform.coverage') }}</h3>
        <el-alert :title="t('ontology.platform.coverageNotice')" type="warning" :closable="false" />
        <el-table :data="release.review.coverage">
          <el-table-column prop="id" :label="t('ontology.platform.capability')" />
          <el-table-column :label="t('ontology.status')"><template #default="{ row }">{{ t(`ontology.platform.${row.state}`) }}</template></el-table-column>
          <el-table-column :label="t('ontology.platform.summary')"><template #default="{ row }">{{ row.summary[locale] }}</template></el-table-column>
          <el-table-column :label="t('ontology.platform.sources')"><template #default="{ row }">{{ row.sources.join(', ') }}</template></el-table-column>
        </el-table>
        <h3>{{ t('ontology.platform.bindings') }}</h3>
        <div v-if="selectedSubject" class="toolbar">
          <code>{{ selectedSubject }}</code>
          <el-button @click="selectedSubject = ''">{{ t('ontology.platform.allMembers') }}</el-button>
        </div>
        <el-table :data="bindings" data-testid="platform-bindings">
          <el-table-column prop="subject" :label="t('ontology.platform.member')" />
          <el-table-column :label="t('ontology.platform.sources')">
            <template #default="{ row }"><el-button v-for="id in row.sources" :key="id" link type="primary" @click="showSource(id)">{{ id }}</el-button></template>
          </el-table-column>
        </el-table>
        <h3>{{ t('ontology.platform.sources') }}</h3>
        <article v-for="source in release.review.sources" :id="`source-${source.id}`" :key="source.id" class="source" data-testid="platform-source">
          <h4>{{ source.id }} · {{ source.owner }} · {{ t(`ontology.platform.kinds.${source.kind}`) }}</h4>
          <p>{{ source.use[locale] }}</p>
          <el-descriptions border :column="1">
            <el-descriptions-item :label="t('ontology.platform.path')"><code>{{ source.path }}</code></el-descriptions-item>
            <el-descriptions-item :label="t('ontology.platform.anchor')"><code>{{ source.anchor }}</code></el-descriptions-item>
            <el-descriptions-item :label="t('ontology.platform.commit')"><code>{{ source.commit }}</code></el-descriptions-item>
            <el-descriptions-item label="SHA-256"><code>{{ source.sha256 }}</code></el-descriptions-item>
          </el-descriptions>
        </article>
      </template>
    </template>
    </section>
  </main>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { navigateConsoleModuleRoute, useConsolePageDescriptor } from '@common-ui'
import OntologyView from '../../../../common-frontend/graph/src/OntologyView.vue'
import { useAuthStore } from '../store/auth'
import { createOntologyAPI } from '../api/ontology.mjs'
import client from '../api/client'
import { platformGraph, platformInspectionOwner, validCapability } from '../utils/platformDefinition.mjs'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore()
const api = createOntologyAPI(client)
const catalog = ref(null), release = ref(null), loading = ref(false), error = ref(''), selectedSubject = ref('')
const graphContainer = ref(null), colors = ref({ node: '', edge: '' })
const owner = computed(() => platformInspectionOwner(auth.authContext))
const allowed = computed(() => Boolean(owner.value) && auth.hasPermission('ontology.platform_definition.read'))
const reauthorizing = computed(() => Boolean(auth.token && !auth.authContext && auth.authContextLoadPromise))
const definition = computed(() => release.value?.context)
const graph = computed(() => definition.value ? platformGraph(definition.value, locale.value, colors.value) : { entityTypes: [], relationTypes: [] })
const bindings = computed(() => release.value?.review.bindings.filter(item => !selectedSubject.value || item.subject === selectedSubject.value) || [])
const conceptName = id => definition.value.concepts.find(item => item.id === id)?.name[locale.value] || id
const selectConcept = item => { selectedSubject.value = `concept/${item.id}` }
const showSource = id => document.getElementById(`source-${id}`)?.scrollIntoView({ block: 'start' })
const go = capability => navigateConsoleModuleRoute(router, 'ontology', `/platform/definitions${capability ? `/${encodeURIComponent(capability)}` : ''}`)
useConsolePageDescriptor(router, 'ontology', {
  title: computed(() => t('ontology.platform.title')),
  subject: computed(() => allowed.value ? definition.value?.capability || '' : ''),
  ready: computed(() => allowed.value && Boolean(release.value))
})
let ticket = 0, controller
function clear() {
  ticket++
  controller?.abort()
  loading.value = false
  catalog.value = release.value = null
  selectedSubject.value = ''
  error.value = ''
}
async function load() {
  clear()
  if (!allowed.value) return
  const capability = route.params.capability
  if (capability && !validCapability(capability)) { error.value = t('ontology.invalidRoute'); return }
  const current = ticket, currentOwner = owner.value
  controller = new AbortController()
  loading.value = true
  try {
    const result = capability ? await api.platformDefinition(capability, controller.signal) : await api.platformDefinitions(controller.signal)
    if (current !== ticket || owner.value !== currentOwner || !allowed.value) return
    if (capability) {
      release.value = result
      await nextTick()
      if (current !== ticket || !graphContainer.value) return
      const style = getComputedStyle(graphContainer.value)
      colors.value = { node: style.getPropertyValue('--el-color-primary').trim(), edge: style.getPropertyValue('--addp-text-secondary').trim() }
    } else catalog.value = result
  } catch (err) {
    if (current === ticket) error.value = err.response?.data?.error || t('ontology.failed')
  } finally {
    if (current === ticket) loading.value = false
  }
}
watch([() => route.params.capability, owner, allowed, reauthorizing, () => Boolean(auth.token)], () => {
  clear()
  if (reauthorizing.value) return
  if (!allowed.value) { error.value = t('ontology.platform.noAccess'); return }
  load()
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(clear)
</script>

<style scoped>
.page { padding: 24px; color: var(--addp-text-primary); }
.toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.toolbar h2 { margin-right: auto; }
.graph-frame { height: 520px; min-width: 0; }
.content { min-height: 120px; }
.source { margin-top: 16px; scroll-margin-top: 12px; }
code { overflow-wrap: anywhere; }
li { margin: 8px 0; }
@media (max-width: 768px) { .page { padding: 12px; } .graph-frame { height: 420px; } }
</style>
