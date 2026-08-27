<template>
  <main class="invitation-page">
    <el-card class="invitation-card" shadow="never">
      <template #header>
        <div class="invitation-header">
          <h1>{{ t('console.invitation.title') }}</h1>
          <p>{{ t('console.invitation.description') }}</p>
        </div>
      </template>

      <el-alert
        v-if="invalidInvitation"
        type="error"
        :closable="false"
        show-icon
        :title="t('console.invitation.invalid')"
      />

      <template v-else-if="authStore.isAuthenticated">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          :title="t('console.invitation.acceptAs', { name: currentUserName })"
        />
        <div class="invitation-actions">
          <el-button :disabled="submitting" @click="useAnotherAccount">
            {{ t('console.invitation.useAnotherAccount') }}
          </el-button>
          <el-button type="primary" :loading="submitting" @click="acceptForCurrentUser">
            {{ t('console.invitation.accept') }}
          </el-button>
        </div>
      </template>

      <el-form v-else label-position="top" @submit.prevent="register">
        <el-form-item :label="t('console.invitation.displayName')" required>
          <el-input v-model="form.displayName" autocomplete="name" />
        </el-form-item>
        <el-form-item :label="t('console.invitation.username')" required>
          <el-input v-model="form.username" autocomplete="username" />
        </el-form-item>
        <el-form-item :label="t('console.invitation.password')" required>
          <el-input v-model="form.password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item :label="t('console.invitation.confirmPassword')" required>
          <el-input v-model="form.confirmPassword" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <div class="invitation-actions">
          <el-button :disabled="submitting" @click="loginExistingAccount">
            {{ t('console.invitation.existingAccount') }}
          </el-button>
          <el-button type="primary" native-type="submit" :loading="submitting">
            {{ t('console.invitation.registerAndAccept') }}
          </el-button>
        </div>
      </el-form>
    </el-card>
  </main>
</template>

<script setup>
import { computed, reactive, ref, watchEffect } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { authAPI } from '../api/auth'
import {
  tenantInvitationRegistrationRequest,
  tenantInvitationSecret,
  tenantInvitationSession
} from '../invitations/tenantInvitation'
import { useAuthStore } from '../store/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const { locale, t } = useI18n()
const submitting = ref(false)
const form = reactive({ displayName: '', username: '', password: '', confirmPassword: '' })

const invitationSecret = computed(() => {
  try {
    return tenantInvitationSecret(route.query.invitation)
  } catch {
    return ''
  }
})
const invalidInvitation = computed(() => !invitationSecret.value)
const currentUserName = computed(() => authStore.user?.display_name || authStore.user?.local_account?.username || t('console.welcome.defaultName'))

watchEffect(() => {
  document.title = `${t('console.invitation.title')}-addp`
})

function errorMessage(error) {
  return error?.response?.data?.error || t('console.invitation.failed')
}

async function finish(response) {
  await authStore.acceptIssuedSession(tenantInvitationSession(response))
  ElMessage.success(t('console.invitation.succeeded'))
  await router.replace('/')
}

async function register() {
  if (invalidInvitation.value || submitting.value) return
  if (form.password !== form.confirmPassword) {
    ElMessage.error(t('console.invitation.passwordMismatch'))
    return
  }
  submitting.value = true
  try {
    const request = tenantInvitationRegistrationRequest({
      invitationSecret: invitationSecret.value,
      username: form.username,
      password: form.password,
      displayName: form.displayName,
      locale: locale.value
    })
    await finish(await authAPI.registerTenantInvitation(request))
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    submitting.value = false
  }
}

async function acceptForCurrentUser() {
  if (invalidInvitation.value || submitting.value || !authStore.token) return
  submitting.value = true
  try {
    await finish(await authAPI.acceptTenantInvitation(invitationSecret.value, authStore.token))
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    submitting.value = false
  }
}

function loginExistingAccount() {
  router.push({ name: 'Login', query: { redirect: route.fullPath } })
}

async function useAnotherAccount() {
  submitting.value = true
  await authStore.logout()
  submitting.value = false
  await router.replace({ name: 'Login', query: { redirect: route.fullPath } })
}
</script>

<style scoped>
.invitation-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px 20px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
}

.invitation-card {
  width: min(560px, 100%);
  background: var(--addp-bg-primary);
  border-color: var(--addp-border-color);
}

.invitation-header h1 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.invitation-header p {
  margin: 8px 0 0;
  color: var(--addp-text-secondary);
}

.invitation-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
}

@media (max-width: 560px) {
  .invitation-actions {
    flex-direction: column-reverse;
  }

  .invitation-actions :deep(.el-button) {
    width: 100%;
    margin-left: 0;
  }
}
</style>
