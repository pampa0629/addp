import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Manager',
  timeout: 60000 // 增加到 60 秒，因为预缓存任务需要查询大表的记录数
})

export default client
