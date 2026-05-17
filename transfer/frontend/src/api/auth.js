import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// 创建指向 System 后端的独立客户端（用于认证）
const systemClient = axios.create({
  baseURL: '/api/v1/system',
  timeout: 10000
})

export const authAPI = createAuthAPI(systemClient)
