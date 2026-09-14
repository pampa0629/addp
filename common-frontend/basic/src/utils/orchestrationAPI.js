// Public Orchestrator API; authentication and language belong to the host client.
export const createOrchestrationAPI = client => ({
  list: () => client.get('/orchestrator/orchestrations'),
  get: id => client.get(`/orchestrator/orchestrations/${id}`),
  execute: id => client.post(`/orchestrator/orchestrations/${id}/execute`)
})
