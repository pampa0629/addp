import client from './client'

export const authAPI = {
  login(credentials) {
    return client.post('/auth/login', credentials)
  },

  register(userData) {
    return client.post('/auth/register', userData)
  },

  getMe() {
    return client.get('/users/me')
  }
}