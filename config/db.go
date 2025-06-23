package config

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

var (
	DB *sql.DB
	// Prepared statements for common queries
	preparedStatements struct {
		updateLastRoom *sql.Stmt
		mu             sync.RWMutex
	}
	// Channel for async database operations
	dbOperations chan func()
)

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultDatabaseConfig returns optimized settings for multiplayer games
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MaxOpenConns:    25,               // Maximum open connections
		MaxIdleConns:    10,               // Keep 10 idle connections ready
		ConnMaxLifetime: 30 * time.Minute, // Close connections after 30 minutes
		ConnMaxIdleTime: 15 * time.Minute, // Close idle connections after 15 minutes
	}
}

func InitDB() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL not set in environment")
	}

	var err error
	DB, err = sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Apply connection pool settings
	config := DefaultDatabaseConfig()
	DB.SetMaxOpenConns(config.MaxOpenConns)
	DB.SetMaxIdleConns(config.MaxIdleConns)
	DB.SetConnMaxLifetime(config.ConnMaxLifetime)
	DB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	// Test the connection
	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize prepared statements
	if err := initPreparedStatements(); err != nil {
		return fmt.Errorf("failed to initialize prepared statements: %w", err)
	}

	// Start async database worker
	initAsyncWorker()

	log.Printf("Database initialized with connection pool (max: %d, idle: %d)",
		config.MaxOpenConns, config.MaxIdleConns)

	return nil
}

// initPreparedStatements prepares commonly used SQL statements
func initPreparedStatements() error {
	preparedStatements.mu.Lock()
	defer preparedStatements.mu.Unlock()

	var err error

	// Prepare statement for updating user's last room
	preparedStatements.updateLastRoom, err = DB.Prepare(`UPDATE "User" SET last_room = $1 WHERE "userId" = $2`)
	if err != nil {
		return fmt.Errorf("failed to prepare updateLastRoom statement: %w", err)
	}

	log.Println("Prepared statements initialized successfully")
	return nil
}

// initAsyncWorker starts a goroutine to handle non-critical database operations
func initAsyncWorker() {
	dbOperations = make(chan func(), 1000) // Buffer up to 1000 operations

	go func() {
		for operation := range dbOperations {
			operation()
		}
	}()

	log.Println("Async database worker started")
}

// UpdateLastRoomAsync updates user's last room asynchronously (non-blocking)
func UpdateLastRoomAsync(userID, roomID string) {
	operation := func() {
		preparedStatements.mu.RLock()
		stmt := preparedStatements.updateLastRoom
		preparedStatements.mu.RUnlock()

		if stmt == nil {
			log.Printf("⚠️ Warning: updateLastRoom prepared statement not available")
			return
		}

		result, err := stmt.Exec(roomID, userID)
		if err != nil {
			log.Printf("⚠️ Warning: Failed to update last_room in database: %v", err)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected > 0 {
			log.Printf("✅ Successfully updated last_room for player %s to room %s", userID, roomID)
		} else {
			log.Printf("⚠️ Warning: No rows updated for player %s (user might not exist)", userID)
		}
	}

	// Try to queue the operation, but don't block if the channel is full
	select {
	case dbOperations <- operation:
		// Operation queued successfully
	default:
		log.Printf("⚠️ Warning: Database operation queue full, dropping update for user %s", userID)
	}
}

// UpdateLastRoomSync updates user's last room synchronously (blocking)
// Use this only when you need to ensure the operation completes before continuing
func UpdateLastRoomSync(userID, roomID string) error {
	preparedStatements.mu.RLock()
	stmt := preparedStatements.updateLastRoom
	preparedStatements.mu.RUnlock()

	if stmt == nil {
		return fmt.Errorf("updateLastRoom prepared statement not available")
	}

	result, err := stmt.Exec(roomID, userID)
	if err != nil {
		return fmt.Errorf("failed to update last_room: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated for user %s (user might not exist)", userID)
	}

	log.Printf("✅ Successfully updated last_room for player %s to room %s", userID, roomID)
	return nil
}

// GetDBStats returns database connection statistics for monitoring
func GetDBStats() sql.DBStats {
	if DB == nil {
		return sql.DBStats{}
	}
	return DB.Stats()
}

// CloseDB gracefully closes the database connection and prepared statements
func CloseDB() error {
	if dbOperations != nil {
		close(dbOperations)
	}

	preparedStatements.mu.Lock()
	defer preparedStatements.mu.Unlock()

	// Close prepared statements
	if preparedStatements.updateLastRoom != nil {
		preparedStatements.updateLastRoom.Close()
	}

	// Close database connection
	if DB != nil {
		return DB.Close()
	}

	log.Println("Database connections closed")
	return nil
}
