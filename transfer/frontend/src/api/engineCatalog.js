import client from './client'

// Meta owns tenant-visible engine selection; adapt its resource_type to the
// EngineType field expected by Transfer's task configuration UI.
export const engineCatalogAPI = {
  async list() {
    const engines = await client.get('/meta/engines')
    return engines.map(engine => ({ ...engine, engine_type: engine.resource_type }))
  }
}
