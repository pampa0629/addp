import client from './client'

// 正式 Owner API；沿用 Transfer 当前用户授权，不通过 Copilot 代读。
export const managerResourceAPI = {
  search: async (query, engineID) => {
    const response = await client.get('/manager/search', {
      params: { q: query, ...(engineID ? { engine_id: engineID } : {}), page: 1, page_size: 20 }
    })
    return response.data
  },
  facts: locator => client.get('/manager/resource-facts', { params: { locator } })
}
