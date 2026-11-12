<template>
  <section class="cdc-monitor">
    <h2>{{ t('monitor.cdc.title') }}</h2>
    <el-tabs :model-value="view" @update:model-value="selectView">
      <el-tab-pane :label="t('monitor.cdc.taskView')" name="tasks" />
      <el-tab-pane :label="t('monitor.cdc.resourceView')" name="resources" />
    </el-tabs>
    <el-alert v-if="view === 'tasks' && auth.contextType !== 'tenant'" :title="t('monitor.cdc.tenantContext')" type="info" :closable="false" />
    <el-alert v-else-if="view === 'resources' && auth.contextType !== 'platform'" :title="t('monitor.cdc.platformContext')" type="info" :closable="false" />
    <CDCTaskChain v-else-if="view === 'tasks'" :key="identity" />
    <CDCRuntimeResources v-else :key="identity" />
  </section>
</template>
<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { navigateMonitorRoute } from '../utils/moduleNavigation'
import { cdcView } from '../utils/cdcTasks'
import CDCRuntimeResources from '../components/CDCRuntimeResources.vue'
import CDCTaskChain from '../components/CDCTaskChain.vue'
const auth = useAuthStore(), route = useRoute(), router = useRouter(), { t } = useI18n()
const view = computed(() => cdcView(route.query, auth.contextType))
const identity = computed(() => JSON.stringify(auth.authContext))
function selectView(view) { navigateMonitorRoute(router, { path: '/cdc-resources', query: { view } }, { history: 'push' }) }
</script>
<style scoped>
.cdc-monitor { min-width: 0; padding: 20px; }
h2 { margin: 0 0 12px; }
.cdc-monitor :deep(.cdc-resources) { padding: 0; }
</style>
