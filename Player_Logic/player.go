package Player_Logic

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Player struct {
	ID       string          `json:"id"`
	Username string          `json:"username"`
	RoomID   string          `json:"room_id"`
	Position Position        `json:"position"`
	IsActive bool            `json:"is_active"`
	LastSeen time.Time       `json:"last_seen"`
	WS       *websocket.Conn `json:"-"`
	mu       sync.RWMutex
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Thread-safe position update
func (p *Player) UpdatePosition(pos Position) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Position = pos
}

// Thread-safe position get
func (p *Player) GetPosition() Position {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Position
}

// MarkDisconnected marks the player as disconnected
func (p *Player) MarkDisconnected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.IsActive = false
	p.LastSeen = time.Now()
}

// IsGracePeriodActive checks if player is still in grace period
func (p *Player) IsGracePeriodActive() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.IsActive {
		return true
	}
	return time.Since(p.LastSeen) < 80*time.Second
}
