import client from './client'

export function getManagerPreview(locator, pageSize = 50) {
  return client.get('/manager/preview', {
    params: {
      locator,
      page: 1,
      page_size: pageSize
    },
    timeout: 60000
  })
}

