import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// 创建指向 System 后端的独立客户端（用于认证）
// 开发模式: 通过 vite proxy 转发到 Gateway
// 生产模式: 通过主 Nginx 转发到 Gateway
const systemClient = axios.create({
  baseURL: '/api/v1/system',  // 使用相对路径，让 proxy 生效
  timeout: 10000
})

export const authAPI = {
  ...createAuthAPI(systemClient),
  registerTenantInvitation: (data) => systemClient.post('/tenant/invitations/registrations', data, {
    withCredentials: true
  }),
  acceptTenantInvitation: (invitationSecret, accessToken) => systemClient.post('/tenant/invitations/acceptances', {
    invitation_secret: invitationSecret
  }, {
    headers: { Authorization: `Bearer ${accessToken}` },
    withCredentials: true
  })
}
