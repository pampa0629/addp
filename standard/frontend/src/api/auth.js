import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// 创建指向 System 后端的独立客户端（用于认证）
// 使用相对路径，通过 Vite proxy 转发到 Gateway (开发环境) 或 Nginx (生产环境)
const systemClient = axios.create({
  baseURL: '/api/system',
  timeout: 10000
})

export const authAPI = {
  ...createAuthAPI(systemClient, {
    includeRegister: true  // Model 模块需要注册功能
  })
}
