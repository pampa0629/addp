import client from './client'

export function navigateGuide(data) {
  return client.post('/copilot/navigate/guide', data)
}
