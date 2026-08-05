<template>
  <main class="login-page">
    <el-card class="login-panel">
      <template #header><strong>{{ t('inference.login.title') }}</strong></template>
      <el-form ref="formRef" :model="form" :rules="rules" @submit.prevent="submit">
        <el-form-item prop="username">
          <el-input v-model="form.username" :placeholder="t('inference.login.username')" :prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="form.password" type="password" show-password :placeholder="t('inference.login.password')" :prefix-icon="Lock" />
        </el-form-item>
        <el-button class="login-button" type="primary" native-type="submit" :loading="loading">
          {{ t('inference.login.submit') }}
        </el-button>
      </el-form>
    </el-card>
  </main>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const formRef = ref()
const loading = ref(false)
const form = reactive({ username: '', password: '' })
const rules = computed(() => ({
  username: [{ required: true, message: t('inference.login.usernameRequired') }],
  password: [{ required: true, message: t('inference.login.passwordRequired') }]
}))

async function submit() {
  if (!await formRef.value.validate().catch(() => false)) return
  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') && !route.query.redirect.startsWith('//')
      ? route.query.redirect
      : '/settings/models'
    await router.replace(redirect)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('inference.login.failed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page { min-height: 100%; display: grid; place-items: center; background: var(--addp-bg-secondary); }
.login-panel { width: min(380px, calc(100vw - 32px)); border-radius: 6px; }
.login-button { width: 100%; }
</style>
