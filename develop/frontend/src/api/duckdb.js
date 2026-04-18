import client from './client'

export const executeFederatedQuery = (sql, timeout = 30) => {
  return client.post('/develop/duckdb/query', { sql, timeout })
}

export const getFederatedSources = () => {
  return client.get('/develop/duckdb/sources')
}

export const testDuckDBConnection = () => {
  return client.get('/develop/duckdb/test')
}

export const getDuckDBSampleQuery = () => {
  return client.get('/develop/duckdb/sample-query')
}
