import client from './client'
export const traceAPI = {
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  read: (params, config) => {
    const { trace_id, ...query } = params
    return client.get(trace_id ? `/monitor/platform/traces/${trace_id}` : '/monitor/platform/traces', { ...config, params: query })
  }
}
