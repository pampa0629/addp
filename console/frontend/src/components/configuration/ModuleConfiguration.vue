<template>
  <section class="module-configuration" :class="{ 'module-configuration--embedded': showManagerInference }">
    <el-tabs :model-value="activeTab" :before-leave="selectTab" class="configuration-tabs">
      <el-tab-pane
        v-for="tab in tabs"
        :key="tab.name"
        :label="t(tab.label)"
        :name="tab.name"
        :class="{ 'embedded-pane': tab.route }"
      >
        <InferenceBindingsConfiguration
          v-if="tab.kind === 'inference' && activeTab === tab.name"
          :owner="owner"
        />
        <PolicyConfiguration
          v-else-if="tab.kind === 'policy' && activeTab === tab.name"
          :owner="owner"
        />
        <SMTPRelayConfiguration
          v-else-if="tab.kind === 'smtp' && activeTab === tab.name"
        />
        <BaseMapConfiguration
          v-else-if="tab.kind === 'map' && activeTab === tab.name"
        />
        <slot v-else-if="tab.route && showManagerInference && activeTab === tab.name" />
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { allowsConsoleRoute } from '@common-ui'
import { useAuthStore } from '../../store/auth'
import InferenceBindingsConfiguration from './InferenceBindingsConfiguration.vue'
import PolicyConfiguration from './PolicyConfiguration.vue'
import SMTPRelayConfiguration from './SMTPRelayConfiguration.vue'
import BaseMapConfiguration from './BaseMapConfiguration.vue'
import { MANAGER_INFERENCE_CONFIGURATION_ROUTE, MANAGER_SEGMENTATION_CONFIGURATION_ROUTE } from '../../utils/consoleNavigation'

const props = defineProps({
  owner: { type: String, required: true },
  showManagerInference: Boolean
})
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const tabsByOwner = {
  agent: [{ name: 'inference', kind: 'inference', label: 'console.configuration.tabs.inference' }],
  copilot: [{ name: 'inference', kind: 'inference', label: 'console.configuration.tabs.inference' }],
  develop: [{ name: 'execution-policy', kind: 'policy', label: 'console.configuration.tabs.executionPolicy' }],
  manager: [
    { name: 'quick-view-policy', kind: 'policy', label: 'console.configuration.tabs.quickViewPolicy', platformOnly: true },
    { name: 'base-map', kind: 'map', label: 'console.configuration.tabs.baseMap' },
    { name: 'embedding', label: 'console.configuration.tabs.embedding', route: MANAGER_INFERENCE_CONFIGURATION_ROUTE },
    { name: 'segmentation', label: 'console.configuration.tabs.segmentation', route: MANAGER_SEGMENTATION_CONFIGURATION_ROUTE }
  ],
  transfer: [{ name: 'continuous-policy', kind: 'policy', label: 'console.configuration.tabs.continuousPolicy' }],
  monitor: [
    { name: 'runtime-policy', kind: 'policy', label: 'console.configuration.tabs.runtimePolicy' },
    { name: 'smtp-relay', kind: 'smtp', label: 'console.configuration.tabs.smtpRelay' }
  ],
  service: [{ name: 'runtime-policy', kind: 'policy', label: 'console.configuration.tabs.runtimePolicy' }]
}
const owner = computed(() => props.owner)
const tabs = computed(() => (tabsByOwner[owner.value] || []).filter(tab =>
  (!tab.platformOnly || authStore.contextType === 'platform') &&
  (!tab.route || allowsConsoleRoute(tab.route, authStore.contextType, authStore.permissions))))
const selectedTab = ref('')
const activeTab = computed(() => {
  if (props.showManagerInference) return route.query.tab === 'segmentation' ? 'segmentation' : 'embedding'
  return tabs.value.find(tab => tab.name === selectedTab.value)?.name || tabs.value[0]?.name || ''
})

async function selectTab(name) {
  if (name === activeTab.value) return true
  const tab = tabs.value.find(item => item.name === name)
  if (tab?.route) return !(await router.push(tab.route))
  const previousTab = selectedTab.value
  selectedTab.value = name
  if (!props.showManagerInference) return true
  const failure = await router.push('/configuration/manager')
  if (failure) selectedTab.value = previousTab
  return !failure
}
</script>

<style scoped>
.module-configuration { width: 100%; }
.configuration-tabs :deep(.el-tabs__content) { overflow: visible; }
.module-configuration--embedded {
  display: flex;
  flex-direction: column;
}
.module-configuration--embedded,
.module-configuration--embedded .configuration-tabs {
  flex: 1;
  min-height: 0;
}
.module-configuration--embedded :deep(.el-tabs__content),
.embedded-pane {
  display: flex;
  flex: 1;
  min-width: 0;
  min-height: 0;
}
</style>
