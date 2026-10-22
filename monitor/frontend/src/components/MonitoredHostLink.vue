<template>
  <span :title="nodeId || undefined" class="monitored-host-link">
    <el-button v-if="linked" text type="primary" :title="`${t('monitor.resources.view')} · ${nodeId}`" @click="open">{{ host.display_name }}</el-button>
    <span v-else>{{ nodeId || emptyLabel || t('monitor.resources.unboundHost') }}</span>
  </span>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { isTargetUUID } from '../utils/monitoringTargets'
import { navigateMonitorRoute } from '../utils/moduleNavigation'

const props = defineProps({ nodeId: { type: String, default: '' }, host: { type: Object, default: null }, allowed: Boolean, emptyLabel: { type: String, default: '' } })
const router = useRouter(), { t } = useI18n()
const linked = computed(() => Boolean(props.allowed && isTargetUUID(props.nodeId) && props.host?.node_id === props.nodeId && typeof props.host.display_name === 'string' && props.host.display_name.trim()))
function open() {
  if (linked.value) return navigateMonitorRoute(router, { path: `/node-resources/${props.nodeId}` }, { history: 'push' })
}
</script>

<style scoped>
.monitored-host-link { min-width: 0; overflow-wrap: anywhere; }
.monitored-host-link .el-button { max-width: 100%; height: auto; white-space: normal; text-align: left; }
</style>
