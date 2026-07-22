<template>
  <main class="oauth-page">
    <section v-loading="loadingRequest" class="oauth-panel">
      <el-icon class="oauth-icon"><Connection /></el-icon>
      <h1>{{ t('console.oauth.authorize.title') }}</h1>
      <p v-if="authorizationRequest">{{ t('console.oauth.authorize.description', { client: clientName }) }}</p>
      <dl v-if="authorizationRequest">
        <div><dt>{{ t('console.oauth.client') }}</dt><dd>{{ clientId }}</dd></div>
        <div><dt>{{ t('console.oauth.scope') }}</dt><dd>{{ scope }}</dd></div>
      </dl>
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <div v-if="authorizationRequest" class="actions">
        <el-button :icon="Close" :disabled="loading" @click="submit('rejected')">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :icon="Check" :loading="loading" @click="approve">
          {{ t('console.oauth.authorize.approve') }}
        </el-button>
      </div>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Check, Close, Connection } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { oauthAPI } from '../api/oauth'
import {
  authorizationDecisionRequest,
  authorizationRequestId,
  redirectToAuthorizationResult
} from '../oauth/authorization'

const route = useRoute()
const { t } = useI18n()
const loadingRequest = ref(true)
const loading = ref(false)
const error = ref('')
const requestId = ref('')
const authorizationRequest = ref(null)
const clientId = computed(() => authorizationRequest.value?.client_id || '')
const clientName = computed(() => authorizationRequest.value?.client_name || clientId.value)
const scope = computed(() => authorizationRequest.value?.scope || '')

onMounted(async () => {
  try {
    requestId.value = authorizationRequestId(route.query)
  } catch {
    error.value = t('console.oauth.invalidRequest')
    loadingRequest.value = false
    return
  }
  try {
    authorizationRequest.value = await oauthAPI.getAuthorizationRequest(requestId.value)
  } catch (requestError) {
    error.value = requestError.response?.status === 410
      ? t('console.oauth.authorize.expired')
      : t('console.oauth.failed')
  } finally {
    loadingRequest.value = false
  }
})

async function submit(decision) {
  loading.value = true
  error.value = ''
  try {
    const response = await oauthAPI.authorize(authorizationDecisionRequest(requestId.value, decision))
    redirectToAuthorizationResult(response, (url) => window.location.assign(url))
  } catch (requestError) {
    error.value = requestError.response?.status === 410
      ? t('console.oauth.authorize.expired')
      : t('console.oauth.failed')
    if (requestError.response?.status === 410) {
      authorizationRequest.value = null
    }
  } finally {
    loading.value = false
  }
}

function approve() {
  return submit('approved')
}
</script>

<style scoped>
.oauth-page { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: var(--addp-bg-secondary); box-sizing: border-box; }
.oauth-panel { width: min(520px, 100%); padding: 28px; border: 1px solid var(--addp-border-color); background: var(--addp-bg-primary); border-radius: 8px; box-sizing: border-box; }
.oauth-icon { font-size: 32px; color: var(--addp-primary-color); }
h1 { margin: 16px 0 8px; color: var(--addp-text-primary); }
p { margin: 0 0 20px; color: var(--addp-text-secondary); line-height: 1.6; }
dl { margin: 0 0 20px; }
dl div { display: grid; grid-template-columns: 100px minmax(0, 1fr); gap: 12px; padding: 10px 0; border-bottom: 1px solid var(--addp-border-color); }
dt { color: var(--addp-text-secondary); }
dd { margin: 0; color: var(--addp-text-primary); overflow-wrap: anywhere; }
.actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 24px; }
</style>
