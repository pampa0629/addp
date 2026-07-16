<template>
  <div class="notification-list">
    <el-tabs v-model="activeChannel" class="channel-tabs">
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
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EmailList from './EmailList.vue'
import WebhookList from './WebhookList.vue'

const { t } = useI18n()
const activeChannel = ref('webhook')
</script>

<style scoped>
.notification-list { min-height: 100%; background: var(--addp-bg-secondary); }
.channel-tabs { padding: 12px 20px 0; }
.channel-tabs :deep(.el-tabs__content) { margin: 0 -20px; }
</style>
