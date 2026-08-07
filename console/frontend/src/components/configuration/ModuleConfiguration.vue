<template>
  <section class="module-configuration">
    <el-tabs v-model="activeTab" class="configuration-tabs">
      <el-tab-pane
        v-for="tab in tabs"
        :key="tab.name"
        :label="t(tab.label)"
        :name="tab.name"
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
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import InferenceBindingsConfiguration from './InferenceBindingsConfiguration.vue'
import PolicyConfiguration from './PolicyConfiguration.vue'
import SMTPRelayConfiguration from './SMTPRelayConfiguration.vue'
import BaseMapConfiguration from './BaseMapConfiguration.vue'

const props = defineProps({ owner: { type: String, required: true } })
const { t } = useI18n()
const tabsByOwner = {
  agent: [{ name: 'inference', kind: 'inference', label: 'console.configuration.tabs.inference' }],
  copilot: [
    { name: 'inference', kind: 'inference', label: 'console.configuration.tabs.inference' },
    { name: 'matching-policy', kind: 'policy', label: 'console.configuration.tabs.matchingPolicy' }
  ],
  develop: [{ name: 'query-policy', kind: 'policy', label: 'console.configuration.tabs.queryPolicy' }],
  manager: [
    { name: 'quick-view-policy', kind: 'policy', label: 'console.configuration.tabs.quickViewPolicy' },
    { name: 'base-map', kind: 'map', label: 'console.configuration.tabs.baseMap' }
  ],
  transfer: [{ name: 'continuous-policy', kind: 'policy', label: 'console.configuration.tabs.continuousPolicy' }],
  monitor: [
    { name: 'runtime-policy', kind: 'policy', label: 'console.configuration.tabs.runtimePolicy' },
    { name: 'smtp-relay', kind: 'smtp', label: 'console.configuration.tabs.smtpRelay' }
  ],
  service: [{ name: 'runtime-policy', kind: 'policy', label: 'console.configuration.tabs.runtimePolicy' }]
}
const owner = computed(() => props.owner)
const tabs = computed(() => tabsByOwner[owner.value] || [])
const activeTab = ref(tabs.value[0]?.name || '')
</script>

<style scoped>
.module-configuration { width: 100%; }
.configuration-tabs :deep(.el-tabs__content) { overflow: visible; }
</style>
