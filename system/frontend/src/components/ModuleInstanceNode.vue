<template>
  <div class="instance-node">
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
import { useI18n } from 'vue-i18n'
defineProps({ instance: { type: Object, required: true } })
const { t } = useI18n()
</script>

<style scoped>
.instance-node > div { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.instance-node span { color: var(--addp-text-secondary); }
</style>
