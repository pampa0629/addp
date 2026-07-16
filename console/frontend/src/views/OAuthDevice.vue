<template>
  <main class="oauth-page">
    <section class="oauth-panel">
      <el-icon class="oauth-icon"><Monitor /></el-icon>
      <h1>{{ t('console.oauth.device.title') }}</h1>
      <p>{{ t('console.oauth.device.description') }}</p>
      <el-input v-model="userCode" size="large" :placeholder="t('console.oauth.device.placeholder')" maxlength="9" />
      <el-alert v-if="message" :title="message" :type="messageType" :closable="false" />
      <div class="actions">
        <el-button :icon="Close" :disabled="loading" @click="submit(false)">{{ t('console.oauth.device.deny') }}</el-button>
        <el-button type="primary" :icon="Check" :loading="loading" @click="submit(true)">
          {{ t('console.oauth.device.approve') }}
        </el-button>
      </div>
    </section>
  </main>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Check, Close, Monitor } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { oauthAPI } from '../api/oauth'

const route = useRoute()
const { t } = useI18n()
const userCode = ref('')
const loading = ref(false)
const message = ref('')
const messageType = ref('success')

onMounted(() => { userCode.value = String(route.query.user_code || '') })

async function submit(approve) {
  if (!userCode.value.trim()) {
    messageType.value = 'warning'
    message.value = t('console.oauth.device.required')
    return
  }
  loading.value = true
  message.value = ''
  try {
    await oauthAPI.approveDevice({ user_code: userCode.value, approve })
    messageType.value = approve ? 'success' : 'info'
    message.value = t(approve ? 'console.oauth.device.approved' : 'console.oauth.device.denied')
  } catch (requestError) {
    messageType.value = 'error'
    message.value = requestError.response?.data?.error || t('console.oauth.failed')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.oauth-page { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: var(--addp-bg-secondary); box-sizing: border-box; }
.oauth-panel { width: min(520px, 100%); padding: 28px; border: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); border-radius: 8px; box-sizing: border-box; }
.oauth-icon { font-size: 32px; color: var(--addp-primary-color); }
h1 { margin: 16px 0 8px; color: var(--addp-text-primary); }
p { margin: 0 0 20px; color: var(--addp-text-secondary); line-height: 1.6; }
.el-alert { margin-top: 16px; }
.actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 24px; }
</style>
