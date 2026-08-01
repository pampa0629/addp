<template>
  <div class="notification-list">
    <el-tabs v-model="activeChannel" class="channel-tabs" @tab-change="handleTabChange">
      <el-tab-pane :label="t('monitor.notification.webhook_tab')" name="webhook">
        <WebhookList v-if="activeChannel === 'webhook'" />
      </el-tab-pane>
      <el-tab-pane :label="t('monitor.notification.email_tab')" name="email">
        <EmailList v-if="activeChannel === 'email'" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import EmailList from './EmailList.vue'
import WebhookList from './WebhookList.vue'
import { navigateMonitorRoute } from '@/utils/moduleNavigation'
import { resolveMonitorTabRouteState } from '@/utils/tabRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const resolveRouteState = routeQuery => resolveMonitorTabRouteState(routeQuery, ['webhook', 'email'], 'webhook')
const activeChannel = ref(resolveRouteState(route.query).tab)

async function handleTabChange(tab) {
  const routeState = resolveRouteState({ tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateMonitorRoute(router, location, { history: 'replace' })
  }
}

async function restoreTabFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeChannel.value = routeState.tab
  if (routeState.changed) {
    await navigateMonitorRoute(router, { path: route.path, query: routeState.query }, { history: 'replace' })
  }
}

watch(() => route.query, restoreTabFromRoute)
onMounted(restoreTabFromRoute)
</script>

<style scoped>
.notification-list { min-height: 100%; background: var(--addp-bg-secondary); }
.channel-tabs { padding: 12px 20px 0; }
.channel-tabs :deep(.el-tabs__content) { margin: 0 -20px; }
</style>
