import client from './client'
export const traceAPI = {
  modules: config => client.get('/system/platform/modules', config),
  instances: (params, config) => client.get('/system/platform/module-instances', { ...config, params }),
  samples: (params, config) => client.get('/monitor/platform/traces/samples', { ...config, params }),
  read: (params, config) => {
    const { trace_id, ...query } = params
    return client.get(trace_id ? `/monitor/platform/traces/${trace_id}` : '/monitor/platform/traces', { ...config, params: query })
  }
}
