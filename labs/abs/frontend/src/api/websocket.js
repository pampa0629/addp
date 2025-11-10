class WebSocketService {
  constructor() {
    this.ws = null
    this.listeners = []
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = 5
  }

  connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws`

    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('WebSocket connected')
      this.reconnectAttempts = 0
    }

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        this.notifyListeners(data)
      } catch (error) {
        console.error('Failed to parse WebSocket message:', error)
      }
    }

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error)
    }

    this.ws.onclose = () => {
      console.log('WebSocket disconnected')
      this.attemptReconnect()
    }
  }

  attemptReconnect() {
    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++
      const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 10000)
      console.log(`Attempting to reconnect in ${delay}ms...`)
      setTimeout(() => this.connect(), delay)
    }
  }

  addListener(callback) {
    this.listeners.push(callback)
  }

  removeListener(callback) {
    this.listeners = this.listeners.filter(cb => cb !== callback)
  }

  notifyListeners(data) {
    this.listeners.forEach(callback => callback(data))
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }
}

export default new WebSocketService()
