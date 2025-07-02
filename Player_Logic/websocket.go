package Player_Logic

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket performance configuration
const (
	// Buffer sizes optimized for multiplayer games
	ReadBufferSize  = 8192 // 8KB for incoming messages
	WriteBufferSize = 8192 // 8KB for outgoing messages

	// Message batching configuration
	BatchSize    = 10                    // Max messages per batch
	BatchTimeout = 50 * time.Millisecond // Max wait time before sending batch

	// Connection limits
	MaxConcurrentConnections = 1000

	// Timeouts
	WriteTimeout = 10 * time.Second
	ReadTimeout  = 60 * time.Second
	PongTimeout  = 60 * time.Second
	PingPeriod   = 54 * time.Second
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:    ReadBufferSize,
		WriteBufferSize:   WriteBufferSize,
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for development
		},
		Subprotocols: []string{"json"},
	}

	// Connection pool management
	connectionPool = &ConnectionPool{
		connections: make(map[string]*Connection),
		mu:          sync.RWMutex{},
	}

	// Message batching
	messageBatcher = &MessageBatcher{
		batches: make(map[string]*MessageBatch),
		mu:      sync.RWMutex{},
	}
)

// Connection represents an optimized WebSocket connection
type Connection struct {
	ws       *websocket.Conn
	playerID string
	roomID   string
	send     chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
	// Rate limiting
	lastMessageTime time.Time
	messageCount    int
}

// ConnectionPool manages all WebSocket connections
type ConnectionPool struct {
	connections map[string]*Connection
	mu          sync.RWMutex
	count       int
}

// MessageBatch holds batched messages for efficient sending
type MessageBatch struct {
	messages []WebSocketMessage
	timer    *time.Timer
	mu       sync.Mutex
}

// MessageBatcher manages message batching per room
type MessageBatcher struct {
	batches map[string]*MessageBatch
	mu      sync.RWMutex
}

// WebSocketMessage represents a message sent over WebSocket
type WebSocketMessage struct {
	Type           string          `json:"type"`
	PlayerID       string          `json:"player_id"`
	TargetPlayerID string          `json:"target_player_id,omitempty"`
	Position       *Position       `json:"position,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Text           string          `json:"text,omitempty"`
	Username       string          `json:"username,omitempty"`
	Timestamp      int64           `json:"timestamp,omitempty"`
}

// BatchedMessage contains multiple messages for efficient transmission
type BatchedMessage struct {
	Type     string             `json:"type"`
	Messages []WebSocketMessage `json:"messages"`
	Count    int                `json:"count"`
}

// HandleWebSocket handles WebSocket connections with optimizations
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket connection attempt from %s", r.RemoteAddr)
	log.Printf("Request headers: %v", r.Header)

	playerID := r.URL.Query().Get("token")
	if playerID == "" {
		log.Printf("WebSocket connection rejected: no token provided")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("WebSocket connection attempt for player: %s", playerID)

	// Check connection limit
	if !connectionPool.canAcceptConnection() {
		log.Printf("Connection rejected for player %s: server at capacity", playerID)
		http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed for player %s: %v", playerID, err)
		return
	}

	// Get room manager
	rm := GetRoomManager()

	// Find player in any room
	player := rm.GetPlayer(playerID)
	if player == nil {
		log.Printf("Player %s not found in any room for WebSocket connection", playerID)
		conn.Close()
		return
	}

	// Get the room containing this player
	room := rm.GetPlayerRoom(playerID)
	if room == nil {
		log.Printf("Room not found for player %s", playerID)
		conn.Close()
		return
	}

	// Create optimized connection
	ctx, cancel := context.WithCancel(context.Background())
	connection := &Connection{
		ws:       conn,
		playerID: playerID,
		roomID:   room.ID,
		send:     make(chan []byte, 256), // Buffered channel for async sending
		ctx:      ctx,
		cancel:   cancel,
	}

	// Register connection
	connectionPool.addConnection(playerID, connection)
	defer connectionPool.removeConnection(playerID)

	// Update player's WebSocket connection
	room.mu.Lock()
	player.WS = conn
	player.IsActive = true
	player.LastSeen = time.Now()
	room.mu.Unlock()

	log.Printf("WebSocket connected for player %s in room %s", playerID, room.ID)

	// Send initial room state
	connection.sendInitialRoomState(room, playerID)

	// Start connection handlers
	go connection.writePump()
	go connection.readPump(rm)

	// Wait for connection to close
	<-ctx.Done()
	conn.Close()
}

// canAcceptConnection checks if server can accept more connections
func (cp *ConnectionPool) canAcceptConnection() bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.count < MaxConcurrentConnections
}

// addConnection adds a connection to the pool
func (cp *ConnectionPool) addConnection(playerID string, conn *Connection) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Remove existing connection if any
	if existingConn, exists := cp.connections[playerID]; exists {
		existingConn.cancel()
		delete(cp.connections, playerID)
		cp.count--
	}

	cp.connections[playerID] = conn
	cp.count++
	log.Printf("Connection pool: %d/%d connections", cp.count, MaxConcurrentConnections)
}

// removeConnection removes a connection from the pool
func (cp *ConnectionPool) removeConnection(playerID string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if conn, exists := cp.connections[playerID]; exists {
		conn.cancel()
		delete(cp.connections, playerID)
		cp.count--
		log.Printf("Connection pool: %d/%d connections", cp.count, MaxConcurrentConnections)
	}
}

// getConnection retrieves a connection from the pool
func (cp *ConnectionPool) getConnection(playerID string) (*Connection, bool) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	conn, exists := cp.connections[playerID]
	return conn, exists
}

// sendInitialRoomState sends current players to newly connected player
func (c *Connection) sendInitialRoomState(room *Room, playerID string) {
	room.mu.RLock()
	defer room.mu.RUnlock()

	var messages []WebSocketMessage
	for id, p := range room.Players {
		if id != playerID {
			messages = append(messages, WebSocketMessage{
				Type:      "player_joined",
				PlayerID:  p.ID,
				Position:  &p.Position,
				Username:  p.Username,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}

	if len(messages) > 0 {
		c.sendBatchedMessages(messages)
	}

	// Notify other players about new player
	joinMessage := WebSocketMessage{
		Type:      "player_joined",
		PlayerID:  playerID,
		Position:  &room.Players[playerID].Position,
		Username:  room.Players[playerID].Username,
		Timestamp: time.Now().UnixMilli(),
	}

	// Broadcast to other players asynchronously
	go broadcastToRoomAsync(room, playerID, joinMessage)
}

// writePump handles outgoing messages with batching
func (c *Connection) writePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
		c.cancel()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(WriteTimeout))
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.ws.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("Write error for player %s: %v", c.playerID, err)
				return
			}

		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(WriteTimeout))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// readPump handles incoming messages
func (c *Connection) readPump(rm *RoomManager) {
	defer c.cancel()

	c.ws.SetReadDeadline(time.Now().Add(ReadTimeout))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(PongTimeout))
		return nil
	})

	for {
		var message WebSocketMessage
		err := c.ws.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error for player %s: %v", c.playerID, err)
			}
			break
		}

		c.ws.SetReadDeadline(time.Now().Add(ReadTimeout))
		c.handlePlayerAction(rm, message)
	}

	// Cleanup on disconnect
	c.handleDisconnect(rm)
}

// handlePlayerAction processes incoming WebSocket messages
func (c *Connection) handlePlayerAction(rm *RoomManager, message WebSocketMessage) {
	switch message.Type {
	case "position_update":
		if message.Position != nil {
			rm.handlePositionUpdate(c.playerID, *message.Position, message.Username)
		}
	case "leave_room":
		rm.RemovePlayer(c.playerID)
		c.cancel()
	case "chat_message":
		c.handleChatMessage(rm, message)
	case "private_message":
		c.handlePrivateMessage(rm, message)
	}
}

// handleChatMessage processes chat messages
func (c *Connection) handleChatMessage(rm *RoomManager, message WebSocketMessage) {
	room := rm.GetPlayerRoom(c.playerID)
	if room == nil {
		log.Printf("Player %s not found in any room for chat message", c.playerID)
		return
	}

	chatMessage := WebSocketMessage{
		Type:      "chat_message",
		PlayerID:  c.playerID,
		Text:      message.Text,
		Username:  message.Username,
		Timestamp: time.Now().UnixMilli(),
	}

	// Broadcast chat message asynchronously
	go broadcastToRoomAsync(room, c.playerID, chatMessage)
}

// handlePrivateMessage processes private messages between players
func (c *Connection) handlePrivateMessage(rm *RoomManager, message WebSocketMessage) {
	// Rate limiting: max 20 messages per minute
	now := time.Now()
	if now.Sub(c.lastMessageTime) < time.Minute {
		c.messageCount++
		if c.messageCount > 20 {
			log.Printf("Rate limit exceeded for player %s", c.playerID)
			return
		}
	} else {
		c.messageCount = 1
		c.lastMessageTime = now
	}

	// Validate message length (max 500 characters)
	if len(message.Text) > 500 {
		log.Printf("Private message from %s too long (%d characters)", c.playerID, len(message.Text))
		return
	}

	// Validate message content
	if strings.TrimSpace(message.Text) == "" {
		log.Printf("Private message from %s is empty or whitespace only", c.playerID)
		return
	}

	if message.TargetPlayerID == "" {
		log.Printf("Private message from %s missing target player ID", c.playerID)
		return
	}

	if message.TargetPlayerID == c.playerID {
		log.Printf("Player %s tried to send private message to themselves", c.playerID)
		return
	}

	// Check if target player exists and is online
	targetPlayer := rm.GetPlayer(message.TargetPlayerID)
	if targetPlayer == nil {
		log.Printf("Target player %s not found for private message from %s", message.TargetPlayerID, c.playerID)
		// Send error message back to sender
		errorMessage := WebSocketMessage{
			Type:      "private_message_error",
			PlayerID:  "system",
			Text:      "Player not found or offline",
			Timestamp: time.Now().UnixMilli(),
		}
		data, err := json.Marshal(errorMessage)
		if err == nil {
			select {
			case c.send <- data:
			default:
				log.Printf("Send channel full for player %s, dropping message", c.playerID)
			}
		}
		return
	}

	// Create private message for target player
	privateMessage := WebSocketMessage{
		Type:           "private_message",
		PlayerID:       c.playerID,
		TargetPlayerID: message.TargetPlayerID,
		Text:           message.Text,
		Username:       message.Username,
		Timestamp:      time.Now().UnixMilli(),
	}

	// Send to target player directly
	if conn, exists := connectionPool.getConnection(message.TargetPlayerID); exists {
		data, err := json.Marshal(privateMessage)
		if err == nil {
			select {
			case conn.send <- data:
			default:
				log.Printf("Send channel full for player %s, dropping private message", message.TargetPlayerID)
			}
		}
	} else {
		log.Printf("Player %s not connected, cannot send private message", message.TargetPlayerID)
	}

	// Send confirmation to sender directly
	confirmationMessage := WebSocketMessage{
		Type:           "private_message_sent",
		PlayerID:       "system",
		TargetPlayerID: message.TargetPlayerID,
		Text:           "Message sent successfully",
		Timestamp:      time.Now().UnixMilli(),
	}
	data, err := json.Marshal(confirmationMessage)
	if err == nil {
		select {
		case c.send <- data:
		default:
			log.Printf("Send channel full for player %s, dropping confirmation message", c.playerID)
		}
	}

	log.Printf("Private message sent from %s to %s", c.playerID, message.TargetPlayerID)
}

// handleDisconnect cleans up when player disconnects
func (c *Connection) handleDisconnect(rm *RoomManager) {
	room := rm.GetPlayerRoom(c.playerID)
	if room != nil {
		room.mu.Lock()
		if _, exists := room.Players[c.playerID]; exists {
			delete(room.Players, c.playerID)
			log.Printf("Removed player %s from room %s. Remaining players: %d",
				c.playerID, room.ID, len(room.Players))
		}
		room.mu.Unlock()

		// Notify other players asynchronously
		leaveMessage := WebSocketMessage{
			Type:      "player_left",
			PlayerID:  c.playerID,
			Timestamp: time.Now().UnixMilli(),
		}
		go broadcastToRoomAsync(room, c.playerID, leaveMessage)
	}
}

// sendBatchedMessages sends multiple messages efficiently
func (c *Connection) sendBatchedMessages(messages []WebSocketMessage) {
	if len(messages) == 0 {
		return
	}

	batchedMessage := BatchedMessage{
		Type:     "batch",
		Messages: messages,
		Count:    len(messages),
	}

	data, err := json.Marshal(batchedMessage)
	if err != nil {
		log.Printf("Error marshaling batch for player %s: %v", c.playerID, err)
		return
	}

	select {
	case c.send <- data:
	default:
		log.Printf("Send channel full for player %s, dropping batch", c.playerID)
	}
}

// broadcastToRoomAsync broadcasts message to all players in room asynchronously
func broadcastToRoomAsync(room *Room, excludePlayerID string, message WebSocketMessage) {
	room.mu.RLock()
	var targets []*Connection

	for playerID := range room.Players {
		if playerID != excludePlayerID {
			if conn, exists := connectionPool.getConnection(playerID); exists {
				targets = append(targets, conn)
			}
		}
	}
	room.mu.RUnlock()

	// Send to all targets concurrently
	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling message: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, conn := range targets {
		wg.Add(1)
		go func(c *Connection) {
			defer wg.Done()
			select {
			case c.send <- data:
			default:
				log.Printf("Send channel full for player %s, dropping message", c.playerID)
			}
		}(conn)
	}
	wg.Wait()
}

// GetConnectionStats returns WebSocket connection statistics
func GetConnectionStats() map[string]interface{} {
	connectionPool.mu.RLock()
	defer connectionPool.mu.RUnlock()

	return map[string]interface{}{
		"active_connections":  connectionPool.count,
		"max_connections":     MaxConcurrentConnections,
		"utilization_percent": float64(connectionPool.count) / float64(MaxConcurrentConnections) * 100,
	}
}
