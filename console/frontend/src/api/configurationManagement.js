import client from './client'

export function listConfigurationManagementEntries() {
  return client.get('/system/configuration-management/entries')
}
