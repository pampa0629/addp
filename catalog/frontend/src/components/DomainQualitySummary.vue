<template>
  <el-card shadow="never" data-testid="catalog-domain-quality-summary">
    <template #header>{{ t('catalog.entries.domainQuality.title') }}</template>
    <p>{{ t('catalog.entries.domainQuality.scope') }}</p>
    <div class="quality-grid">
      <section v-for="section in sections" :key="section.key" :data-state="section.status">
        <strong>{{ t(`catalog.entries.domainQuality.${section.key}`) }}</strong>
        <span v-if="section.status === 'ready'"> · {{ t('catalog.entries.domainQuality.count', { count: section.total }) }}</span>
        <p v-if="section.status !== 'ready'">{{ t(`catalog.entries.domainQuality.${section.status === 'forbidden' ? 'forbidden' : section.status === 'unavailable' ? 'unavailable' : 'loading'}`) }}</p>
        <p v-else-if="section.items.length === 0">{{ t('catalog.entries.domainQuality.empty') }}</p>
        <ul v-else><li v-for="item in section.items" :key="item.id">{{ item.name || t('catalog.entries.unnamed') }}</li></ul>
        <el-button v-if="section.status === 'unavailable'" link @click="load(section)">{{ t('catalog.entries.domainQuality.retry') }}</el-button>
        <el-button v-if="section.status === 'ready' && allowsConsoleRoute(section.route, auth.contextType, auth.permissions)" link type="primary" @click="open(section)">{{ t('catalog.entries.domainQuality.viewAll') }}</el-button>
      </section>
    </div>
  </el-card>
</template>
<script setup>
import { reactive, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { allowsConsoleRoute, openConsoleRoute } from '@common-ui'
import { useAuthStore } from '../store/auth'
import { DOMAIN_QUALITY_SECTIONS, loadDomainQualitySection } from '../utils/domainQualitySummary'
import { listDomainQualityIssues, listDomainQualityPlans, listDomainQualityRules } from '../api/quality'
const props = defineProps({ domainId: { type: String, required: true }, refreshToken: { type: Number, default: 0 } })
const auth = useAuthStore()
const { t } = useI18n()
const readers = { rules: listDomainQualityRules, plans: listDomainQualityPlans, issues: listDomainQualityIssues }
const sections = reactive(DOMAIN_QUALITY_SECTIONS.map(section => ({ ...section, status: 'idle', items: [], total: 0, requestVersion: 0 })))
const load = section => loadDomainQualitySection(section, { domainId: props.domainId, hasPermission: key => auth.hasPermission(key), list: readers[section.key] })
function open(section) {
 const query = new URLSearchParams({ owner_domain_id: props.domainId, ...(section.key === 'issues' ? { status: 'open' } : {}) })
 openConsoleRoute(`${section.route}?${query}`)
}
watch([() => props.domainId, () => props.refreshToken, () => auth.permissions.join(','), () => auth.authContext?.context?.tenant_id, () => auth.contextType], () => sections.forEach(load), { immediate: true })
onBeforeUnmount(() => sections.forEach(section => { section.requestVersion++ }))
</script>
<style scoped>
p,li { color:var(--el-text-color-secondary); }
.quality-grid {display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px;}
section {min-width:0;overflow-wrap:anywhere;border:1px solid var(--el-border-color);padding:16px;border-radius:var(--el-border-radius-base);}
ul {padding-inline-start:20px;line-height:1.6;}
section :deep(.el-button){max-width:100%;white-space:normal;text-align:start;}
@media(max-width:900px){.quality-grid{grid-template-columns:1fr;}}
</style>
