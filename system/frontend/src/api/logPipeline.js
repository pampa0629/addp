import client from './client'
const base = '/monitor/platform/log-pipeline'
export const logPipelineAPI = {
  summary: signal => client.get(base, { signal }),
  policy: () => client.get(`${base}/policy`),
  updatePolicy: data => client.put(`${base}/policy`, data)
}
