package service

import (
	"abs/internal/models"
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// WebSocketManager manages WebSocket connections
type WebSocketManager struct {
	clients map[*websocket.Conn]bool
	mu      sync.RWMutex
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		clients: make(map[*websocket.Conn]bool),
	}
}

// AddClient registers a new WebSocket client
func (m *WebSocketManager) AddClient(conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[conn] = true
	log.Printf("WebSocket client connected. Total clients: %d", len(m.clients))
}

// RemoveClient unregisters a WebSocket client
func (m *WebSocketManager) RemoveClient(conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, conn)
	conn.Close()
	log.Printf("WebSocket client disconnected. Total clients: %d", len(m.clients))
}

// BroadcastProgress sends a progress update to all connected clients
func (m *WebSocketManager) BroadcastProgress(update *models.ProgressUpdate) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal progress update: %v", err)
		return
	}

	for client := range m.clients {
		err := client.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			log.Printf("Failed to send message to client: %v", err)
			client.Close()
			delete(m.clients, client)
		}
	}
}
