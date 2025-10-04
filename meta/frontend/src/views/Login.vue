<template>
  <div class="login-container">
    <el-card class="login-box">
      <template #header>
        <div class="card-header">
          <h2>元数据管理系统</h2>
          <p class="subtitle">Metadata Management System</p>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="loginForm"
        :rules="rules"
        @submit.prevent="handleLogin"
      >
        <el-form-item prop="username">
          <el-input
            v-model="loginForm.username"
            placeholder="请输入用户名"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            placeholder="请输入密码"
            :prefix-icon="Lock"
            size="large"
            show-password
          />
        </el-form-item>

        <el-form-item>
          <el-button
            native-type="submit"
            type="primary"
            size="large"
            style="width: 100%"
            :loading="loading"
          >
            {{ loading ? '登录中...' : '登录' }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { User, Lock } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const router = useRouter()
const formRef = ref(null)

// 页面加载时显示上次登录的调试日志
onMounted(() => {
  const logs = localStorage.getItem('login_debug_logs')
  if (logs) {
    console.log('=== 上次登录的调试日志 ===')
    console.log(JSON.parse(logs))
    console.log('=========================')
  }

  const guardLogs = localStorage.getItem('guard_logs')
  if (guardLogs) {
    console.log('=== 路由守卫日志 ===')
    console.log(JSON.parse(guardLogs))
    console.log('===================')
  }
})

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ]
}

const loading = ref(false)

const handleLogin = async () => {
  const log = (msg, data = null) => {
    const logMsg = data ? `${msg} ${JSON.stringify(data)}` : msg
    console.log(logMsg)
    // 保存日志到localStorage，防止页面刷新丢失
    const logs = JSON.parse(localStorage.getItem('login_debug_logs') || '[]')
    logs.push({ time: new Date().toISOString(), msg: logMsg })
    localStorage.setItem('login_debug_logs', JSON.stringify(logs.slice(-20))) // 只保留最后20条
  }

  log('=== 开始登录流程 ===')
  log('handleLogin 被调用')

  if (!formRef.value) {
    log('ERROR: formRef.value 不存在')
    alert('ERROR: formRef.value 不存在')
    return
  }

  log('开始表单验证...')
  await formRef.value.validate(async (valid) => {
    log('表单验证结果:', valid)
    if (valid) {
      loading.value = true
      try {
        log('开始登录请求...')
        log('请求数据:', { username: loginForm.username, password: '***' })

        // 调用System模块的登录接口
        const response = await axios.post('http://localhost:8080/api/auth/login', {
          username: loginForm.username,
          password: loginForm.password
        })

        log('登录响应状态:', response.status)
        log('登录响应数据:', response.data)

        // System后端返回的是 access_token 和 token_type
        const token = response.data.access_token

        if (!token) {
          const errMsg = '响应中没有access_token!'
          log('ERROR: ' + errMsg)
          alert('ERROR: ' + errMsg + '\n响应: ' + JSON.stringify(response.data))
          ElMessage.error('登录失败：服务器未返回token')
          return
        }

        // 解析JWT token获取用户信息（JWT格式: header.payload.signature）
        const payload = JSON.parse(atob(token.split('.')[1]))
        const user = {
          id: payload.user_id,
          username: payload.username
        }

        // 保存token和用户信息
        localStorage.setItem('token', token)
        localStorage.setItem('user', JSON.stringify(user))

        log('登录成功，token已保存:', token.substring(0, 20) + '...')
        log('user已保存:', user)
        log('localStorage验证 - token:', localStorage.getItem('token')?.substring(0, 20) + '...')
        log('localStorage验证 - user:', localStorage.getItem('user'))

        ElMessage.success('登录成功')

        log('准备跳转到 /metadata')
        // 使用 nextTick 确保 localStorage 写入完成后再跳转
        await nextTick()
        log('nextTick 后，再次验证 localStorage')
        log('localStorage验证2 - token:', localStorage.getItem('token')?.substring(0, 20) + '...')
        log('localStorage验证2 - user:', localStorage.getItem('user'))

        // 使用 window.location.href 强制完整页面跳转，避免路由守卫问题
        log('使用 window.location.href 跳转')
        window.location.href = '/meta/metadata'
      } catch (err) {
        const errMsg = `登录错误: ${err.message}`
        const errData = err.response?.data
        log('ERROR: ' + errMsg)
        log('错误详情:', err)
        log('错误响应:', errData)

        // 使用alert显示错误，防止刷新丢失
        alert(`登录失败!\n${errMsg}\n响应: ${JSON.stringify(errData)}`)

        ElMessage.error(err.response?.data?.error || '登录失败')
      } finally {
        loading.value = false
        log('loading 设置为 false')
      }
    } else {
      log('ERROR: 表单验证失败')
      alert('表单验证失败！请检查输入')
    }
  })
  log('=== 登录流程结束 ===')
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.login-box {
  width: 100%;
  max-width: 400px;
}

.card-header {
  text-align: center;
}

.card-header h2 {
  margin: 0;
  color: #303133;
  font-size: 24px;
}

.card-header .subtitle {
  margin: 5px 0 0 0;
  color: #909399;
  font-size: 14px;
}
</style>
