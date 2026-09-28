<template>
  <div class="professional-summary" data-testid="catalog-domain-professional-summary">
    <section class="owner-group" data-testid="catalog-domain-standard-summary">
      <div class="summary-header">
        <strong>{{ t('catalog.entries.domainStandard.title') }}</strong>
        <span>{{ t('catalog.entries.domainStandard.source') }}</span>
      </div>
      <p class="summary-note">{{ t('catalog.entries.domainStandard.scope') }}</p>
      <div class="summary-grid">
        <section
          v-for="section in standardSections"
          :key="section.key"
          class="definition-section"
          :data-testid="`catalog-domain-standard-${section.key}`"
          :data-state="section.status"
        >
          <div class="section-heading">
            <strong>{{ t(`catalog.entries.domainStandard.${section.key}`) }}</strong>
            <span v-if="section.status === 'ready'">{{ t('catalog.entries.domainStandard.count', { count: section.total }) }}</span>
          </div>
          <p v-if="section.status === 'forbidden'" class="section-message">{{ t('catalog.entries.domainStandard.forbidden') }}</p>
          <p v-else-if="section.status === 'unavailable'" class="section-message">
            {{ t('catalog.entries.domainStandard.unavailable') }}
            <el-button link type="primary" @click="loadStandard(section)">{{ t('catalog.entries.domainStandard.retry') }}</el-button>
          </p>
          <p v-else-if="section.status === 'loading'" class="section-message">{{ t('catalog.entries.domainStandard.loading') }}</p>
          <p v-else-if="section.status === 'ready' && section.items.length === 0" class="section-message">{{ t('catalog.entries.domainStandard.empty') }}</p>
          <template v-else-if="section.status === 'ready'">
            <ul class="definition-list">
              <li v-for="item in section.items" :key="item.id" class="definition-item">
                <div class="definition-name">
                  <el-link v-if="canOpenDetail(section, item)" type="primary" :underline="false" @click="openDetail(section, item)">{{ item.name }}</el-link>
                  <span v-else>{{ item.name }}</span>
                  <el-tag v-if="item.revisionStatus" size="small" effect="plain">{{ revisionStatusLabel(item.revisionStatus) }}</el-tag>
                </div>
                <span v-if="item.code && item.code !== item.name" class="definition-code">{{ item.code }}</span>
              </li>
            </ul>
            <el-pagination
              v-if="section.total > pageSize"
              class="section-pagination"
              small
              :current-page="section.page"
              :page-size="pageSize"
              :total="section.total"
              layout="prev, pager, next"
              @current-change="page => changePage(section, page)"
            />
          </template>
        </section>
      </div>
    </section>

    <section class="owner-group" data-testid="catalog-domain-model-links">
      <div class="summary-header">
        <strong>{{ t('catalog.entries.domainModel.title') }}</strong>
        <span>{{ t('catalog.entries.domainModel.source') }}</span>
      </div>
      <p class="summary-note">{{ t('catalog.entries.domainModel.scope') }}</p>
      <div class="owner-links">
        <el-button v-for="link in modelLinks" :key="link.key" v-show="canOpenOwnerList(link.route)" link type="primary" @click="openOwnerList(link.route, 'domain_id')">
          {{ t(`catalog.entries.domainModel.${link.key}`) }}
        </el-button>
        <span v-if="!modelLinks.some(link => canOpenOwnerList(link.route))" class="section-message">{{ t('catalog.entries.domainModel.forbidden') }}</span>
      </div>
    </section>

    <section class="owner-group" data-testid="catalog-domain-quality-summary">
      <div class="summary-header">
        <strong>{{ t('catalog.entries.domainQuality.title') }}</strong>
        <span>{{ t('catalog.entries.domainQuality.source') }}</span>
      </div>
      <p class="summary-note">{{ t('catalog.entries.domainQuality.scope') }}</p>
      <div class="summary-grid">
        <section
          v-for="section in qualitySections"
          :key="section.key"
          class="definition-section"
          :data-testid="`catalog-domain-quality-${section.key}`"
          :data-state="section.status"
        >
          <div class="section-heading">
            <strong>{{ t(`catalog.entries.domainQuality.${section.key}`) }}</strong>
            <span v-if="section.status === 'ready'">{{ t('catalog.entries.domainQuality.count', { count: section.total }) }}</span>
          </div>
          <p v-if="section.status === 'forbidden'" class="section-message">{{ t('catalog.entries.domainQuality.forbidden') }}</p>
          <p v-else-if="section.status === 'unavailable'" class="section-message">
            {{ t('catalog.entries.domainQuality.unavailable') }}
            <el-button link type="primary" @click="loadQuality(section)">{{ t('catalog.entries.domainQuality.retry') }}</el-button>
          </p>
          <p v-else-if="section.status === 'loading'" class="section-message">{{ t('catalog.entries.domainQuality.loading') }}</p>
          <p v-else-if="section.status === 'ready' && section.items.length === 0" class="section-message">{{ t('catalog.entries.domainQuality.empty') }}</p>
          <template v-else-if="section.status === 'ready'">
            <ul class="definition-list">
              <li v-for="item in section.items" :key="item.id" class="definition-item">
                <span class="definition-name">{{ item.name }}</span>
                <span v-if="item.code && item.code !== item.name" class="definition-code">{{ item.code }}</span>
              </li>
            </ul>
          </template>
          <el-button v-if="section.status === 'ready' && canOpenOwnerList(section.route)" link type="primary" class="owner-list-link" @click="openOwnerList(section.route, 'owner_domain_id', section.key === 'issues' ? 'open' : '')">
            {{ t('catalog.entries.domainQuality.viewAll') }}
          </el-button>
        </section>
      </div>
    </section>
  </div>
</template>

<script setup>
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { allowsConsoleRoute, openConsoleRoute } from '@common-ui'
import { listDomainElements, listDomainGlossaries, listDomainMetrics } from '../api/standard'
import { listDomainQualityIssues, listDomainQualityPlans, listDomainQualityRules } from '../api/quality'
import { useAuthStore } from '../store/auth'
import { DOMAIN_STANDARD_PAGE_SIZE, DOMAIN_STANDARD_SECTIONS, loadDomainStandardSection } from '../utils/domainStandardSummary'
import { DOMAIN_QUALITY_SECTIONS, loadDomainQualitySection } from '../utils/domainQualitySummary'

const props = defineProps({
  domainId: { type: String, required: true },
  refreshToken: { type: Number, default: 0 }
})

const { t } = useI18n()
const authStore = useAuthStore()
const pageSize = DOMAIN_STANDARD_PAGE_SIZE
const listByType = { glossaries: listDomainGlossaries, elements: listDomainElements, metrics: listDomainMetrics }
const qualityLists = { rules: listDomainQualityRules, plans: listDomainQualityPlans, issues: listDomainQualityIssues }
const modelLinks = [
  { key: 'entities', route: '/modeling/entities' },
  { key: 'logicalModels', route: '/modeling/logical-tables' }
]
const standardSections = reactive(DOMAIN_STANDARD_SECTIONS.map(definition => ({
  ...definition,
  page: 1,
  total: 0,
  items: [],
  status: 'idle',
  requestVersion: 0
})))
const qualitySections = reactive(DOMAIN_QUALITY_SECTIONS.map(definition => ({
  ...definition,
  total: 0,
  items: [],
  status: 'idle',
  requestVersion: 0
})))

async function loadStandard(section) {
  return loadDomainStandardSection(section, {
    domainId: props.domainId,
    hasPermission: permission => authStore.hasPermission(permission),
    list: listByType[section.key]
  })
}

async function loadQuality(section) {
  return loadDomainQualitySection(section, {
    domainId: props.domainId,
    hasPermission: permission => authStore.hasPermission(permission),
    list: qualityLists[section.key]
  })
}

function changePage(section, page) {
  section.page = page
  loadStandard(section)
}

function canOpenDetail(section, item) {
  return item.id && allowsConsoleRoute(`${section.route}/${encodeURIComponent(item.id)}`, authStore.contextType, authStore.permissions)
}

function openDetail(section, item) {
  if (!canOpenDetail(section, item)) return
  openConsoleRoute(`${section.route}/${encodeURIComponent(item.id)}`)
}

function canOpenOwnerList(route) {
  return allowsConsoleRoute(route, authStore.contextType, authStore.permissions)
}

function openOwnerList(route, domainKey, status = '') {
  if (!canOpenOwnerList(route)) return
  const params = new URLSearchParams({ [domainKey]: props.domainId })
  if (status) params.set('status', status)
  openConsoleRoute(`${route}?${params}`)
}

function revisionStatusLabel(status) {
  const known = ['draft', 'in_review', 'published', 'withdrawn']
  return known.includes(status) ? t(`catalog.entries.domainStandard.revisionStatus.${status}`) : status
}

watch(
  [() => props.domainId, () => props.refreshToken, () => authStore.permissions.join(','), () => authStore.contextType],
  ([domainId], [previousDomainId] = []) => {
    if (domainId !== previousDomainId) standardSections.forEach(section => { section.page = 1 })
    standardSections.forEach(loadStandard)
    qualitySections.forEach(loadQuality)
  },
  { immediate: true }
)
</script>

<style scoped>
.summary-header,
.section-heading,
.definition-name {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.professional-summary { border-top: 1px solid var(--el-border-color-lighter); margin-top: 16px; padding-top: 16px; }
.owner-group + .owner-group { border-top: 1px solid var(--el-border-color-lighter); margin-top: 18px; padding-top: 18px; }
.owner-links { display: flex; flex-wrap: wrap; gap: 8px 16px; }
.owner-list-link { margin-top: 8px; }

.summary-header span,
.summary-note,
.section-heading span,
.section-message,
.definition-code {
  color: var(--el-text-color-secondary);
}

.summary-note { margin: 0 0 1rem; }
.summary-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1rem; }
.definition-section { min-width: 0; padding: 1rem; border: 1px solid var(--el-border-color); border-radius: var(--el-border-radius-base); }
.section-message { margin: 1rem 0 0; }
.definition-list { list-style: none; margin: 0.75rem 0 0; padding: 0; }
.definition-item { padding: 0.5rem 0; border-top: 1px solid var(--el-border-color-lighter); }
.definition-name { justify-content: flex-start; min-width: 0; }
.definition-name > span:first-child,
.definition-name > .el-link { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.definition-code { display: block; margin-top: 0.25rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.85rem; }
.section-pagination { margin-top: 0.75rem; justify-content: center; }

@media (max-width: 1100px) {
  .summary-grid { grid-template-columns: 1fr; }
}
</style>
