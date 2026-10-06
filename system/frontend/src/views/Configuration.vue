<template>
  <section class="system-configuration" data-testid="system-configuration">
    <h2>{{ t('system.configuration.title') }}</h2>
    <el-tabs v-model="activeTab">
      <el-tab-pane v-for="tab in tabs" :key="tab.name" :name="tab.name" :label="t(tab.label)">
        <component :is="tab.component" v-if="activeTab === tab.name" />
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, markRaw } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import SecurityPolicy from './SecurityPolicy.vue'
import EngineConfiguration from '../components/configuration/EngineConfiguration.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const domains = [
  { name: 'security-policy', label: 'system.iam.tabs.securityPolicy', permission: 'iam.security_policy.read', platformOnly: true, component: markRaw(SecurityPolicy) },
  { name: 'engine-configuration', label: 'system.configuration.engines', permission: 'system.engine_raster_policy.read', component: markRaw(EngineConfiguration) }
]
const tabs = computed(() => domains.filter(tab =>
  (!tab.platformOnly || auth.contextType === 'platform') && auth.hasPermission(tab.permission)))
const activeTab = computed({
  get: () => tabs.value.find(tab => tab.name === route.query.tab)?.name || tabs.value[0]?.name || '',
  set: name => router.replace({ query: { ...route.query, tab: name === tabs.value[0]?.name ? undefined : name } })
})
</script>

<style scoped>
.system-configuration { min-width: 0; }
.system-configuration h2 { margin-top: 0; }
</style>
