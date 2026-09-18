export function createOntologyAPI(client) {
  const payload = (response) => response.data
  const head = (id) => `/ontologies/${encodeURIComponent(id)}`
  const revision = (id, number) => `${head(id)}/revisions/${number}`
  return {
    list: (page) =>
      client
        .get('/ontologies', { params: { page, page_size: 20 } })
        .then(payload),
    head: (id) => client.get(head(id)).then(payload),
    revisions: (id, page) =>
      client
        .get(`${head(id)}/revisions`, { params: { page, page_size: 20 } })
        .then(payload),
    get: (id, number) => client.get(revision(id, number)).then(payload),
    projection: (id, number) =>
      client.get(`${revision(id, number)}/projection`).then(payload),
    create: (id, number, definition) =>
      client
        .post(`${head(id)}/revisions`, { revision: number, definition })
        .then(payload),
    save: (id, number, version, definition) =>
      client.put(revision(id, number), { version, definition }).then(payload),
    transition: (id, number, action, body) =>
      client.post(`${revision(id, number)}/${action}`, body).then(payload)
  }
}
