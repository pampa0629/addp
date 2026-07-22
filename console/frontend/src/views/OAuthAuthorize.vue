<template>
  <main class="oauth-page">
    <section class="oauth-panel">
      <el-icon class="oauth-icon"><Connection /></el-icon>
      <h1>{{ t('console.oauth.authorize.title') }}</h1>
      <p>{{ t('console.oauth.authorize.description', { client: clientId }) }}</p>
      <dl>
        <div><dt>{{ t('console.oauth.client') }}</dt><dd>{{ clientId }}</dd></div>
        <div><dt>{{ t('console.oauth.scope') }}</dt><dd>{{ scope }}</dd></div>
      </dl>
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <div class="actions">
        <el-button :icon="Close" :disabled="loading" @click="submit('rejected')">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :icon="Check" :loading="loading" @click="approve">
          {{ t('console.oauth.authorize.approve') }}
        </el-button>
      </div>
    </section>
  </main>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Check, Close, Connection } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { oauthAPI } from '../api/oauth'
import {
  authorizationDecisionRequest,
  redirectToAuthorizationResult
} from '../oauth/authorization'

const route = useRoute()
const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const clientId = computed(() => String(route.query.client_id || ''))
const scope = computed(() => String(route.query.scope || 'addp.api'))

async function submit(decision) {
  loading.value = true
  error.value = ''
  try {
    const response = await oauthAPI.authorize(authorizationDecisionRequest(route.query, decision))
    redirectToAuthorizationResult(response, (url) => window.location.assign(url))
  } catch (requestError) {
    error.value = requestError.response?.data?.error || t('console.oauth.failed')
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
