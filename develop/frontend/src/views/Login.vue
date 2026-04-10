<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <h2>{{ t('develop.login.title') }}</h2>
      </template>

      <el-form :model="form" :rules="rules" ref="formRef" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            :placeholder="t('develop.login.usernamePlaceholder')"
            prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            :placeholder="t('develop.login.passwordPlaceholder')"
            prefix-icon="Lock"
            size="large"
            @keyup.enter="handleLogin"
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            size="large"
            :loading="loading"
            @click="handleLogin"
            style="width: 100%"
          >
            {{ t('develop.login.submit') }}
          </el-button>
        </el-form-item>
      </el-form>

      <el-alert
        v-if="error"
        :title="error"
        type="error"
        :closable="false"
        style="margin-top: 10px"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const error = ref('')

const form = ref({
  username: '',
  password: ''
})

const rules = {
  username: [{ required: true, message: t('develop.login.usernameRequired'), trigger: 'blur' }],
  password: [{ required: true, message: t('develop.login.passwordRequired'), trigger: 'blur' }]
}

const handleLogin = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    loading.value = true
    error.value = ''

    try {
      await authStore.login(form.value.username, form.value.password)
      ElMessage.success(t('develop.login.success'))

      // 优先跳转到 redirect 参数指定的页面
      const redirect = route.query.redirect || '/sql'
      window.location.href = redirect
    } catch (err) {
      console.error('[Login] Login or redirect failed:', err)

      // 区分登录失败和跳转失败
      if (err.response) {
        // API 请求失败
        error.value = err.response?.data?.error || t('develop.login.failed')
      } else if (err.name === 'NavigationDuplicated') {
        // 路由跳转重复（可忽略）
        console.warn('[Login] Navigation duplicated, already at target page')
      } else {
        // 其他错误（可能是路由守卫拒绝）
        error.value = t('develop.login.redirectFailed')
        console.error('[Login] Unexpected error:', err)
      }
    } finally {
      loading.value = false
    }
  })
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
  background: var(--addp-primary-gradient);
}

.login-card {
  width: 400px;
}

.login-card h2 {
  text-align: center;
  margin: 0;
}
</style>
