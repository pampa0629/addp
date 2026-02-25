import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// 创建专用的 System 客户端用于认证
// 所有认证请求都发送到 System 后端 (端口 8180)
const systemClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8180/api/system' : '/api/system',
  timeout: 10000
})

// 创建认证 API
export const authAPI = createAuthAPI(systemClient)
