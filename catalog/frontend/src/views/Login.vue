<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>{{ t('catalog.login.title') }}</h2>
          <p>{{ t('catalog.login.subtitle') }}</p>
        </div>
      </template>
      <el-form ref="formRef" :model="form" :rules="rules" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="form.username" :placeholder="t('catalog.login.username')" :prefix-icon="User" size="large" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="form.password" type="password" show-password :placeholder="t('catalog.login.password')" :prefix-icon="Lock" size="large" />
        </el-form-item>
        <el-button class="submit-button" native-type="submit" type="primary" size="large" :loading="loading">
          {{ loading ? t('catalog.login.loading') : t('catalog.login.submit') }}
        </el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const form = reactive({ username: '', password: '' })
const rules = {
  username: [{ required: true, message: t('catalog.login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('catalog.login.passwordRequired'), trigger: 'blur' }]
}

async function handleLogin() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    await authStore.login(form.username, form.password)
    ElMessage.success(t('catalog.login.success'))
    await router.push(typeof route.query.redirect === 'string' ? route.query.redirect : '/entries')
  } catch (error) {
    ElMessage.error(error?.message || t('catalog.login.failed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--addp-bg-secondary);
}
.login-box { width: min(400px, calc(100vw - 32px)); }
.card-header { text-align: center; }
.card-header h2 { margin: 0; color: var(--addp-text-primary); }
.card-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.submit-button { width: 100%; }
</style>
