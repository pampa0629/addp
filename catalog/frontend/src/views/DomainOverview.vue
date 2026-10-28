<template>
 <main class="domain-overview" data-testid="catalog-domain-overview">
  <header><h2>{{ t('catalog.domainOverview.title') }}</h2><el-button @click="refresh">{{ t('catalog.common.refresh') }}</el-button></header>
  <el-card shadow="never">
   <div class="selectors">
    <BusinessDomainSelect :options="domainOptions" :model-value="state.domainId" filterable clearable :placeholder="t('catalog.entries.navigation.allDomains')" data-testid="catalog-overview-domain" @update:model-value="value => change({ domainId: value || '' })" />
    <el-radio-group v-if="auth.hasPermission('catalog.inventory.read')" :model-value="state.view" @update:model-value="value => change({ view: value })">
     <el-radio-button value="governance">{{ t('catalog.entries.view.governance') }}</el-radio-button><el-radio-button value="inventory">{{ t('catalog.entries.view.inventory') }}</el-radio-button>
    </el-radio-group>
   </div>
   <el-alert v-if="domainsError" type="warning" :closable="false" :title="t('catalog.entries.domainContext.unavailable')" />
   <section v-if="selectedDomain" class="domain-definition"><h3>{{ selectedDomain.name }}</h3><p>{{ selectedDomain.path.join(' / ') }} · {{ selectedDomain.code }}</p><p>{{ selectedDomain.description || t('catalog.entries.domainContext.noDefinition') }}</p></section>
  </el-card>
  <el-empty v-if="!state.domainId" :description="t('catalog.domainOverview.selectDomain')" />
  <el-alert v-else-if="invalidDomain" type="warning" :closable="false" :title="t('catalog.entries.domainContext.invalid')" />
  <template v-else>
   <el-card v-loading="loading" shadow="never" data-testid="catalog-domain-resource-summary">
    <template #header>{{ t('catalog.domainOverview.resources') }}</template>
    <el-alert v-if="overviewError" type="warning" :closable="false" :title="t('catalog.entries.loadFailed')" />
    <template v-else-if="overview">
     <p>{{ t('catalog.domainOverview.scope') }}</p>
     <strong>{{ t('catalog.entries.view.resultCount', { count: overview.total_entries }) }}</strong>
     <div class="resource-types"><el-button v-for="item in overview.entry_types" :key="item.entry_type" @click="browse({ entry_type: item.entry_type })">{{ catalogEntryTypeLabel(item.entry_type, t) }} · {{ item.count }}</el-button></div>
     <el-empty v-if="overview.total_entries === 0" :description="t('catalog.entries.domainContext.empty')" />
     <div class="statuses"><el-button v-for="item in overview.governance_statuses" :key="item.status" link @click="browse({ governance_status: item.status })">{{ t(`catalog.status.governance.${item.status}`) }} · {{ item.count }}</el-button></div>
     <section v-if="overview.coverage"><h3>{{ t('catalog.domainOverview.governance') }}</h3><div class="coverage-grid"><el-button v-for="dimension in overview.coverage.dimensions" :key="dimension.key" :disabled="!dimension.not_covered" @click="browse({ coverage_dimension: dimension.key, coverage_state: 'missing' })">{{ coverageDimensionLabel(t, dimension.key, 'name') }} · {{ dimension.covered }}/{{ dimension.applicable }}</el-button></div></section>
    </template>
   </el-card>
   <el-card shadow="never" data-testid="catalog-domain-reference-summary">
    <template #header>{{ t('catalog.domainOverview.referencedStandards') }}</template>
    <p>{{ t('catalog.domainOverview.referencesScope') }}</p>
    <section v-for="group in groups" :key="group.key" class="reference-group" :data-testid="`catalog-domain-references-${group.key}`" :data-state="group.status">
     <h3>{{ t(`catalog.domainOverview.groups.${group.key}`) }}</h3>
     <p v-if="group.status === 'forbidden'">{{ t('catalog.domainOverview.forbidden') }}</p>
     <p v-else-if="group.status === 'loading'">{{ t('catalog.domainOverview.loading') }}</p>
     <p v-else-if="group.status === 'unavailable'">{{ t('catalog.domainOverview.unavailable') }} <el-button link @click="loadGroup(group)">{{ t('catalog.domainOverview.retry') }}</el-button></p>
     <template v-else-if="group.status === 'ready'">
      <p v-if="!group.items.length">{{ t(group.total ? 'catalog.domainOverview.pageEmpty' : 'catalog.domainOverview.noReferences') }}</p>
      <article v-for="item in group.items" :key="`${item.source_type}:${item.source_identity}`">
       <el-link v-if="item.target?.entry" type="primary" @click="navigateConsoleModuleRoute(router, 'catalog', `/entries/${encodeURIComponent(item.target.entry.id)}`)">{{ item.target.entry.display_name }}</el-link>
       <span v-else>{{ t('catalog.domainOverview.targetUnavailable') }}</span>
       <el-tag size="small">{{ t(`catalog.domainOverview.ownership.${item.ownership}`) }}</el-tag>
       <ul><li v-for="(evidence, index) in item.evidence" :key="index"><span>{{ evidence.resource_name }} · {{ relationLabel(evidence) }}</span> <el-tag size="small">{{ t(`catalog.domainOverview.states.${evidence.state}`) }}</el-tag><el-button v-if="canOpenEvidence(item, evidence)" link type="primary" @click="openEvidence(item, evidence)">{{ t('catalog.domainOverview.openAdopted') }}</el-button></li></ul><p v-if="item.evidence_truncated">{{ t('catalog.domainOverview.moreEvidence') }}</p>
      </article>
      <el-pagination v-if="group.total > 5" :current-page="group.page" :page-size="5" :total="group.total" layout="prev, pager, next" @current-change="page => changePage(group, page)" />
     </template>
    </section>
   </el-card>
   <DomainQualitySummary :domain-id="state.domainId" :refresh-token="refreshToken" />
  </template>
 </main>
</template>
<script setup>
import { computed, ref, reactive, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BusinessDomainSelect, buildBusinessDomainOptions, allowsConsoleRoute, openConsoleRoute, navigateConsoleModuleRoute } from '@common-ui'
import client from '../api/client'
import { listDomainOverviews, resolveSourceEntries } from '../api/catalog'
import { useAuthStore } from '../store/auth'
import { catalogEntryTypeLabel } from '../utils/catalogResourceTypes'
import { standardResourcePresentation } from '../utils/standardResourcePresentation'
import { buildEntryListQuery } from '../utils/entryRouteState'
import { DOMAIN_REFERENCE_GROUPS, domainOverviewState, domainOverviewQuery, loadDomainReferenceGroup } from '../utils/domainOverview'
import { coverageDimensionLabel } from '../utils/governanceCoverageView'
import DomainQualitySummary from '../components/DomainQualitySummary.vue'
const auth = useAuthStore(), router = useRouter(), route = useRoute()
const { t } = useI18n()
const state = computed(() => domainOverviewState(route.query, auth.hasPermission('catalog.inventory.read')))
const domainTree = ref(null), domainsError = ref(false), loading = ref(false), overviewError = ref(false), overview = ref(null), refreshToken = ref(0)
const groups = reactive(DOMAIN_REFERENCE_GROUPS.map(group => ({ ...group, page: 1, total: 0, status: 'idle', items: [], version: 0 })))
const domainOptions = computed(() => domainTree.value ? buildBusinessDomainOptions(domainTree.value) : [])
const selectedDomain = computed(() => domainOptions.value.find(item => String(item.id) === state.value.domainId))
const invalidDomain = computed(() => Array.isArray(domainTree.value) && state.value.domainId && !selectedDomain.value)
let version = 0, domainVersion = 0
async function loadDomains() {
 const current = ++domainVersion
 try { const tree = await listDomainOverviews(); if (current === domainVersion) { domainTree.value = tree; domainsError.value = false } }
 catch { if (current === domainVersion) { domainTree.value = null; domainsError.value = true } }
}
function change(patch) { return navigateConsoleModuleRoute(router, 'catalog', { path: '/domains', query: domainOverviewQuery({ ...state.value, ...patch }) }) }
function browse(filters) { return navigateConsoleModuleRoute(router, 'catalog', { path: '/entries', query: buildEntryListQuery({ ...filters, primary_domain_id: state.value.domainId, view: state.value.view }) }) }
async function loadOverview() {
 const current = ++version
 overview.value = null; overviewError.value = false
 if (!state.value.domainId || invalidDomain.value) { loading.value = false; return }
 loading.value = true
 try { const result = await client.get(`/catalog/domains/${encodeURIComponent(state.value.domainId)}/overview`, { params: { view: state.value.view } }); if (current === version) overview.value = result }
 catch { if (current === version) overviewError.value = true }
 finally { if (current === version) loading.value = false }
}
function loadGroup(group) {
 return loadDomainReferenceGroup(group, { ...state.value, hasPermission: key => auth.hasPermission(key), read: (path, params) => client.get(path, { params }), resolve: resolveSourceEntries })
}
function changePage(group, page) { group.page = page; loadGroup(group) }
function relationLabel(evidence) {
 const keys = { 'catalog.entry.glossary': 'glossary', 'catalog.component.standard_mapping': 'mapping', 'model.entity.element': 'entityElement', 'model.logical_table.element': 'tableElement', 'model.metric_implementation.definition': 'metricDefinition', 'standard.glossary.element': 'glossaryElement', 'standard.element.code_set': 'elementCodeSet', 'standard.metric.dependency': 'metricDependency' }
 const label = t(`catalog.domainOverview.relations.${keys[evidence.relation_kind]}`)
 return ['model.entity.element', 'model.logical_table.element', 'catalog.component.standard_mapping'].includes(evidence.relation_kind) ? `${label} · ${evidence.position}` : label
}
function adoptedPath(item, evidence) {
 return standardResourcePresentation(item.source_type, item.source_identity, evidence.revision_id)?.detailPath || ''
}
function canOpenEvidence(item, evidence) { return allowsConsoleRoute(adoptedPath(item, evidence), auth.contextType, auth.permissions) }
function openEvidence(item, evidence) { if (canOpenEvidence(item, evidence)) openConsoleRoute(adoptedPath(item, evidence)) }
function refresh() { refreshToken.value++; loadDomains() }
watch([() => state.value.domainId, () => state.value.view, () => auth.permissions.join(','), () => auth.authContext?.context?.tenant_id, () => auth.contextType, () => refreshToken.value, () => invalidDomain.value], () => {
 const canonical = domainOverviewQuery(state.value)
 if (JSON.stringify(Object.entries(route.query).sort()) !== JSON.stringify(Object.entries(canonical).sort())) { navigateConsoleModuleRoute(router, 'catalog', { path: '/domains', query: canonical }, { history: 'replace' }) }
 loadOverview(); groups.forEach(group => { group.page = 1; if (invalidDomain.value) { group.version++; group.items = []; group.status = 'idle' } else loadGroup(group) })
}, { immediate: true })
onMounted(loadDomains)
onBeforeUnmount(() => { version++; domainVersion++; groups.forEach(group => { group.version++ }) })
</script>
<style scoped>
.domain-overview{padding:20px;display:grid;grid-template-columns:minmax(0,1fr);gap:16px;min-width:0;}
header,.selectors,.resource-types,.statuses,.coverage-grid{display:flex;align-items:center;gap:12px;flex-wrap:wrap;}
header{justify-content:space-between;} h2,h3{margin:0 0 12px;} p,li{color:var(--el-text-color-secondary);}
.selectors>.el-select{width:min(100%,440px);} .resource-types,.statuses,.coverage-grid{margin:16px 0;}
.domain-definition{display:grid;gap:12px;margin-top:24px;overflow-wrap:anywhere;}
.domain-definition h3,.domain-definition p{margin:0;}
.resource-types :deep(.el-button),.statuses :deep(.el-button),.coverage-grid :deep(.el-button){margin:0;}
.reference-group{border-top:1px solid var(--el-border-color);padding:16px 0;}article{padding:12px 0;} article>.el-tag{margin-left:8px;}
li{overflow-wrap:anywhere;} .el-link{max-width:100%;overflow-wrap:anywhere;} .el-pagination{margin-top:12px;}
</style>
