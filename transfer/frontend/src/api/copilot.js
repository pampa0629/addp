import client from './client'

export const transferCopilotAPI = {
  generate: (data) => client.post('/copilot/transfer/generate', data)
}
