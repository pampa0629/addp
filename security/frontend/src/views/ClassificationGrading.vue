<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h2>{{ t('security.resources.classificationGrading') }}</h2>
        <p>{{ t('security.descriptions.classificationGrading') }}</p>
      </div>
    </header>

    <el-card class="foundation-workspace">
      <div v-if="profile" class="profile-banner">
        <div>
          <strong>{{ t(profile.name_i18n_key) }}</strong>
          <p>{{ t(profile.description_i18n_key, { classifications: profile.classification_count, grades: profile.grade_count }) }}</p>
        </div>
        <el-button v-if="canApplyProfile" :loading="applyingProfile" @click="applyProfile">
          {{ t('security.definitionProfile.apply') }}
        </el-button>
      </div>

      <el-tabs v-model="activeTab" class="foundation-tabs" @tab-change="handleTabChange">
        <el-tab-pane
          v-for="tab in foundationTabs"
          :key="tab.name"
          :name="tab.name"
          :label="t(tab.label)"
        />
      </el-tabs>
      <FoundationList :key="`${activeResource}-${workspaceVersion}`" :resource-key="activeResource" embedded />
    </el-card>
  </section>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { navigateConsoleModuleRoute, resolveCanonicalTabRouteState } from '@common-ui'
import FoundationList from './FoundationList.vue'
import { definitionProfileAPI } from '../api/security'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const profile = ref(null)
const applyingProfile = ref(false)
const workspaceVersion = ref(0)
const foundationTabs = [
  { name: 'classifications', resource: 'classification', label: 'security.definitionTabs.classification' },
  { name: 'grades', resource: 'grade', label: 'security.definitionTabs.grade' }
]
const resolveRouteState = routeQuery => resolveCanonicalTabRouteState({
  allowedTabs: foundationTabs.map(tab => tab.name),
  defaultTab: 'classifications',
  routeQuery
})
const activeTab = ref(resolveRouteState(route.query).tab)
const activeResource = computed(() => foundationTabs.find(tab => tab.name === activeTab.value)?.resource || 'classification')
const canReadProfile = computed(() => auth.hasPermission('security.classification.read') && auth.hasPermission('security.grade.read'))
const canApplyProfile = computed(() => auth.hasPermission('security.classification.create') && auth.hasPermission('security.grade.create'))

async function loadProfile() {
  if (!canReadProfile.value) return
  try {
    const profiles = await definitionProfileAPI.list()
    profile.value = Array.isArray(profiles) ? profiles[0] || null : null
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  }
}

async function applyProfile() {
  if (!profile.value) return
  applyingProfile.value = true
  try {
    const result = await definitionProfileAPI.apply(profile.value.key)
    workspaceVersion.value += 1
    ElMessage.success(t('security.definitionProfile.applied', {
      classifications: result.created_classifications,
      grades: result.created_grades
    }))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    applyingProfile.value = false
  }
}

async function navigateToTab(tab, history = 'replace') {
  const routeState = resolveRouteState({ tab })
  const location = { path: '/classification-grading', query: routeState.query }
  if (router.resolve(location).fullPath === route.fullPath) return
  await navigateConsoleModuleRoute(router, 'security', location, { history })
}

async function handleTabChange(tab) {
  await navigateToTab(tab)
}

watch(() => route.query, async routeQuery => {
  const routeState = resolveRouteState(routeQuery)
  activeTab.value = routeState.tab
  if (routeState.changed) await navigateToTab(routeState.tab)
}, { immediate: true })

onMounted(loadProfile)
</script>

<style scoped>
.page { min-height: 100%; padding: 20px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.page-header { margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.foundation-workspace { background: var(--addp-bg-primary); border-color: var(--addp-border-color); }
.profile-banner { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 14px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.profile-banner p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.foundation-tabs { margin: -4px 0 4px; }
:deep(.foundation-tabs .el-tabs__header) { margin-bottom: 8px; }
:deep(.foundation-workspace > .el-card__body) { padding-top: 12px; }
@media (max-width: 720px) { .profile-banner { align-items: flex-start; flex-direction: column; } }
</style>
