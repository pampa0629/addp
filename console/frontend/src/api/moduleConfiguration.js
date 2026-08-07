import client from './client'

export const moduleConfigurationAPI = {
  getSMTPRelay: () => client.get('/monitor/settings/smtp-relay'),
  updateSMTPRelay: (payload) => client.put('/monitor/settings/smtp-relay', payload),
  setSMTPRelayCredential: (credential) => client.put('/monitor/settings/smtp-relay/credential', { credential }),
  deleteSMTPRelayCredential: () => client.delete('/monitor/settings/smtp-relay/credential'),
  listBaseMapProviders: () => client.get('/manager/settings/base-map/providers'),
  updateBaseMapProvider: (payload) => client.put('/manager/settings/base-map/providers', payload)
}
