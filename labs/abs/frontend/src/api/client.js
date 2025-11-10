import axios from 'axios'

const client = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

export const taskAPI = {
  create(prompt) {
    return client.post('/tasks', { prompt })
  },

  get(id) {
    return client.get(`/tasks/${id}`)
  },

  list() {
    return client.get('/tasks')
  }
}

export const appAPI = {
  list() {
    return client.get('/apps')
  },
  create(payload) {
    return client.post('/apps', payload)
  },
  get(id) {
    return client.get(`/apps/${id}`)
  },
  launch(id) {
    return client.post(`/apps/${id}/launch`)
  },
  modify(id, prompt) {
    return client.post(`/apps/${id}/modify`, { prompt })
  },
  delete(id) {
    return client.delete(`/apps/${id}`)
  }
}

export default client
