<template>
  <section class="resource-configuration">
    <header class="resource-header"><div><h2>{{ t('console.configuration.resources.smtpRelay.title') }}</h2><p>{{ contextLabel }}</p></div><el-button :icon="Refresh" circle :aria-label="t('common.refresh')" :loading="loading" :disabled="saving" @click="load" /></header>
    <el-form v-if="form" :model="form" :disabled="!canSave" label-position="top" class="resource-form" @submit.prevent>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.enabled')"><el-switch v-model="form.enabled" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.host')"><el-input v-model="form.host" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.port')"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.tlsMode')"><el-select v-model="form.tls_mode"><el-option label="STARTTLS" value="starttls" /><el-option label="TLS" value="tls" /></el-select></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.fromAddress')"><el-input v-model="form.from_address" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.fromName')"><el-input v-model="form.from_name" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.username')"><el-input v-model="form.username" /></el-form-item>
      <el-form-item :label="t('console.configuration.resources.smtpRelay.credential')"><el-input v-model="credential" type="password" show-password :placeholder="credentialPlaceholder" /></el-form-item>
      <div class="resource-status"><el-tag :type="form.credential?.configured ? 'success' : 'warning'" effect="plain">{{ form.credential?.configured ? t('console.configuration.resources.configured') : t('console.configuration.resources.notConfigured') }}</el-tag><span>v{{ form.credential?.version || 0 }}</span></div>
      <footer class="form-actions"><el-button type="primary" :icon="Check" :loading="saving" :disabled="!canSave" @click="save">{{ t('console.configuration.save') }}</el-button></footer>
    </el-form>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../store/auth'
import { moduleConfigurationAPI } from '../../api/moduleConfiguration'

const { t } = useI18n()
const authStore = useAuthStore()
const loading = ref(false)
const saving = ref(false)
const form = ref(null)
const credential = ref('')
const canSave = computed(() => Boolean(form.value) && !saving.value && authStore.hasPermission('monitor.configuration.update'))
const contextLabel = computed(() => authStore.contextType === 'platform' ? t('console.configuration.platformContext') : t('console.configuration.tenantContext'))
const credentialPlaceholder = computed(() => form.value?.credential?.configured ? t('console.configuration.resources.smtpRelay.replaceCredential') : t('console.configuration.resources.smtpRelay.enterCredential'))

async function load() {
  form.value = null
  credential.value = ''
  loading.value = true
  try {
    form.value = await moduleConfigurationAPI.getSMTPRelay()
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('console.configuration.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!canSave.value) return
  const { version, enabled, host, port, tls_mode, from_address, from_name, username } = form.value
  saving.value = true
  try {
    form.value = await moduleConfigurationAPI.updateSMTPRelay({
      version, enabled, host, port, tls_mode, from_address, from_name, username
    })
    if (credential.value) await moduleConfigurationAPI.setSMTPRelayCredential(credential.value)
    ElMessage.success(t('console.configuration.saveSuccess'))
    await load()
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('console.configuration.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>.resource-configuration{width:100%}.resource-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.resource-header h2{margin:0 0 6px;color:var(--addp-text-primary);font-size:20px;font-weight:600;letter-spacing:0}.resource-header p{margin:0;color:var(--addp-text-secondary);font-size:14px}.resource-form{max-width:560px}.resource-status{display:flex;gap:12px;align-items:center;color:var(--addp-text-secondary);font-size:12px}.form-actions{display:flex;justify-content:flex-end;margin-top:12px}</style>
