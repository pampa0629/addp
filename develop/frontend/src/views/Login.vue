<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <h2>数据开发 - 登录</h2>
      </template>

      <el-form :model="form" :rules="rules" ref="formRef" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            placeholder="用户名"
            prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
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
            登录
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
import { useAuthStore } from '../stores/auth'
import { ElMessage } from 'element-plus'

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
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
}

const handleLogin = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    loading.value = true
    error.value = ''

    try {
      console.log('[Login] Starting login process...')
      await authStore.login(form.value.username, form.value.password)
      console.log('[Login] Login successful, token saved')
      console.log('[Login] Auth state:', {
        isAuthenticated: authStore.isAuthenticated,
        hasUser: !!authStore.user,
        username: authStore.user?.username
      })

      ElMessage.success('登录成功')

      // 🆕 优先跳转到 redirect 参数指定的页面
      const redirect = route.query.redirect || '/sql'
      console.log('[Login] Redirecting to:', redirect)
      console.log('[Login] ⏸️  等待 5 秒后跳转，方便查看日志...')

      // 延迟 5 秒后跳转，方便复制日志
      await new Promise(resolve => setTimeout(resolve, 5000))
      console.log('[Login] 开始跳转...')

      // 使用 window.location.href 强制刷新并导航
      window.location.href = redirect
    } catch (err) {
      console.error('[Login] Login or redirect failed:', err)

      // 区分登录失败和跳转失败
      if (err.response) {
        // API 请求失败
        error.value = err.response?.data?.error || '登录失败，请检查用户名和密码'
      } else if (err.name === 'NavigationDuplicated') {
        // 路由跳转重复（可忽略）
        console.warn('[Login] Navigation duplicated, already at target page')
      } else {
        // 其他错误（可能是路由守卫拒绝）
        error.value = '登录后跳转失败，请刷新页面重试'
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
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.login-card {
  width: 400px;
}

.login-card h2 {
  text-align: center;
  margin: 0;
}
</style>
