<template>
  <div class="instance-node">
    <div class="node-association" data-testid="instance-node-binding">
      <el-tag size="small" :type="bindingState === 'bound' ? 'success' : bindingState === 'rejected' ? 'warning' : 'info'">{{ t(`system.hostNodes.bindingStates.${bindingState}`) }}</el-tag>
      <el-button v-if="bindingState === 'bound' && instance.node_id && canReadNode" text type="primary" @click="openNode">{{ t('system.hostNodes.details') }}</el-button>
    </div>
    <div v-if="bindingState === 'bound'" :title="instance.node_id">{{ t('system.hostNodes.id') }}：{{ instance.node_id }}</div>
    <div v-else-if="instance.declared_node_id" :title="instance.declared_node_id">{{ t('system.hostNodes.declaredID') }}：{{ instance.declared_node_id }}</div>
    <div v-if="bindingState === 'rejected'">{{ t(`system.hostNodes.bindingReasons.${instance.node_binding_reason}`) }}</div>
    <div :title="instance.host_node_name || t('system.module.instances.nodeUnknown')">
      <span>{{ t('system.module.instances.hostNodeName') }}：</span>{{ instance.host_node_name || t('system.module.instances.nodeUnknown') }}
    </div>
    <div :title="instance.host_node_ips?.join(', ') || t('system.module.instances.nodeUnknown')">
      <span>{{ t('system.module.instances.hostNodeIPs') }}：</span>{{ instance.host_node_ips?.join(', ') || t('system.module.instances.nodeUnknown') }}
    </div>
    <div :title="instance.runtime_hostname || t('system.module.instances.nodeUnknown')">
      <span>{{ t('system.module.instances.runtimeHostname') }}：</span>{{ instance.runtime_hostname || t('system.module.instances.nodeUnknown') }}
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { navigateSystemRoute } from '../utils/moduleNavigation'
const props = defineProps({ instance: { type: Object, required: true } })
const { t } = useI18n()
const auth = useAuthStore(), router = useRouter()
const canReadNode = computed(() => auth.contextType === 'platform' && auth.hasPermission('platform.host_node.read'))
const bindingState = computed(() => ['bound', 'rejected', 'unbound'].includes(props.instance.node_binding_state) ? props.instance.node_binding_state : 'unknown')
function openNode() {
  if (bindingState.value === 'bound' && props.instance.node_id && canReadNode.value) {
    return navigateSystemRoute(router, { path: `/host-nodes/${encodeURIComponent(props.instance.node_id)}` })
  }
}
</script>

<style scoped>
.instance-node > div { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.instance-node span { color: var(--addp-text-secondary); }
</style>
