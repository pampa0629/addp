import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// 创建指向 System 后端的独立客户端（用于认证）
const systemClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8080/api' : '/api',
  timeout: 10000
})

export const authAPI = {
  ...createAuthAPI(systemClient, {
    includeRegister: true  // Portal 需要注册功能
  }),

  // 保留 getMe() 用于向后兼容
  getMe: () => {
    return systemClient.get('/users/me')
  }
}
