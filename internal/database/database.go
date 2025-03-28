package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"
	"webhook-service/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/joho/godotenv/autoload"
)

// Service represents a service that interacts with a database.
type Service interface {
	// Health returns a map of health status information.
	// The keys and values in the map are service-specific.
	Health() map[string]string

	// Close terminates the database connection.
	// It returns an error if the connection cannot be closed.
	Close() error

	// GetDB returns the underlying database connection.
	// This should be used carefully and only when direct database access is required.
	GetDB() *sql.DB
}

type service struct {
	db *sql.DB
}

var (
	dbInstance *service
)

func New(config *config.Config) Service {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	connStr := config.GetDSN()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatal(err)
	}
	
	// Configure connection pool
	
	// Set max open connections
	maxOpenConns := config.DBMaxOpenConns
	if maxOpenConns <= 0 {
		// Rule: maxWorkers * 1.5
		maxOpenConns = config.MaxWorkers + (config.MaxWorkers / 2)
		log.Printf("No max open connections configured, calculated value: %d", maxOpenConns)
	}
	db.SetMaxOpenConns(maxOpenConns)
	
	// Set max idle connections
	maxIdleConns := config.DBMaxIdleConns
	if maxIdleConns <= 0 {
		// Rule: maxOpenConns / 4, but at least minWorkers
		maxIdleConns = maxOpenConns / 4
		if maxIdleConns < config.MinWorkers {
			maxIdleConns = config.MinWorkers
		}
		log.Printf("No max idle connections configured, calculated value: %d", maxIdleConns)
	}
	db.SetMaxIdleConns(maxIdleConns)
	
	// Set connection lifetimes
	// Use default values if not configured
	connMaxLifetime := config.DBConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = 30 * time.Minute
		log.Printf("No connection max lifetime configured, using default: %s", connMaxLifetime)
	}
	db.SetConnMaxLifetime(connMaxLifetime)
	
	connMaxIdleTime := config.DBConnMaxIdleTime
	if connMaxIdleTime <= 0 {
		connMaxIdleTime = 5 * time.Minute
		log.Printf("No connection max idle time configured, using default: %s", connMaxIdleTime)
	}
	db.SetConnMaxIdleTime(connMaxIdleTime)
	
	log.Printf("Database connection pool configured: maxOpen=%d, maxIdle=%d, maxLifetime=%s, maxIdleTime=%s", 
		maxOpenConns, maxIdleConns, connMaxLifetime, connMaxIdleTime)
	
	dbInstance = &service{
		db: db,
	}
	return dbInstance
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)

	// Ping the database
	err := s.db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Fatalf("db down: %v", err) // Log the error and terminate the program
		return stats
	}

	// Database is up, add more statistics
	stats["status"] = "up"
	stats["message"] = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := s.db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats["message"] = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

// Close closes the database connection.
// It logs a message indicating the disconnection from the specific database.
// If the connection is successfully closed, it returns nil.
// If an error occurs while closing the connection, it returns the error.
func (s *service) Close() error {
	log.Printf("Disconnected from database: %s", s.db)
	return s.db.Close()
}

// GetDB returns the underlying database connection
func (s *service) GetDB() *sql.DB {
	return s.db
}
