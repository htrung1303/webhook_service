package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	// Server configuration
	Port int `json:"port"`
	MockEnabled bool `json:"mock_enabled"`

	// Database configuration
	DBHost     string `json:"db_host"`
	DBPort     string `json:"db_port"`
	DBName     string `json:"db_name"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`
	DBSchema   string `json:"db_schema"`
	
	// Database connection pool configuration
	DBMaxOpenConns    int           `json:"db_max_open_conns"`
	DBMaxIdleConns    int           `json:"db_max_idle_conns"`
	DBConnMaxLifetime time.Duration `json:"db_conn_max_lifetime"`
	DBConnMaxIdleTime time.Duration `json:"db_conn_max_idle_time"`
	BackupPollingInterval time.Duration `json:"backup_polling_interval"`
	MaxEventsPerPoll int `json:"max_events_per_poll"`
	RetryCheckInterval time.Duration `json:"retry_check_interval"`

	// Worker pool configuration
	MinWorkers     int           `json:"min_workers"`
	MaxWorkers     int           `json:"max_workers"`
	QueueBufferSize int          `json:"queue_buffer_size"`
	MinRequestWorkers int           `json:"min_request_workers"`
	MaxRequestWorkers int           `json:"max_request_workers"`
	RequestBufferSize int           `json:"request_buffer_size"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	
	// Retry configuration
	InitialDelay time.Duration `json:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay"`
	MaxRetries   int           `json:"max_retries"`
}

func New() *Config {
	cfg := &Config{
		Port:            8080,
		MinWorkers:      5,
		MaxWorkers:      20,
		MinRequestWorkers: 5,
		MaxRequestWorkers: 20,
		RequestBufferSize: 1000,
		QueueBufferSize: 1000,
		ShutdownTimeout: 30 * time.Second,
		InitialDelay:    1 * time.Second,
		MaxDelay:        1 * time.Minute,
		MaxRetries:      5,
		MockEnabled:     false,
		
		// Default database connection pool settings
		// Set to 0 to trigger automatic calculation in database.go
		DBMaxOpenConns:    0, // Will be calculated based on MaxWorkers
		DBMaxIdleConns:    0, // Will be calculated based on MaxOpenConns
		DBConnMaxLifetime: 0, // Will use default in database.go
		DBConnMaxIdleTime: 0, // Will use default in database.go
		BackupPollingInterval: 15 * time.Minute,
		MaxEventsPerPoll: 100,
		RetryCheckInterval: 30 * time.Second,
	}

	// Override with environment variables if present
	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	// Database config
	if host := os.Getenv("WS_DB_HOST"); host != "" {
		cfg.DBHost = host
	} else {
		cfg.DBHost = "localhost"
	}

	if port := os.Getenv("WS_DB_PORT"); port != "" {
		cfg.DBPort = port
	} else {
		cfg.DBPort = "5432"
	}

	if name := os.Getenv("WS_DB_DATABASE"); name != "" {
		cfg.DBName = name
	} else {
		cfg.DBName = "webhook_service"
	}

	if user := os.Getenv("WS_DB_USERNAME"); user != "" {
		cfg.DBUser = user
	} else {
		cfg.DBUser = "admin"
	}

	if password := os.Getenv("WS_DB_PASSWORD"); password != "" {
		cfg.DBPassword = password
	} else {
		cfg.DBPassword = "password1234"
	}

	if schema := os.Getenv("WS_DB_SCHEMA"); schema != "" {
		cfg.DBSchema = schema
	} else {
		cfg.DBSchema = "public"
	}
	
	// Database connection pool config
	if maxOpen := os.Getenv("WS_DB_MAX_OPEN_CONNS"); maxOpen != "" {
		if val, err := strconv.Atoi(maxOpen); err == nil {
			cfg.DBMaxOpenConns = val
		}
	}
	
	if maxIdle := os.Getenv("WS_DB_MAX_IDLE_CONNS"); maxIdle != "" {
		if val, err := strconv.Atoi(maxIdle); err == nil {
			cfg.DBMaxIdleConns = val
		}
	}
	
	if maxLifetime := os.Getenv("WS_DB_CONN_MAX_LIFETIME"); maxLifetime != "" {
		if val, err := time.ParseDuration(maxLifetime); err == nil {
			cfg.DBConnMaxLifetime = val
		}
	}
	
	if maxIdleTime := os.Getenv("WS_DB_CONN_MAX_IDLE_TIME"); maxIdleTime != "" {
		if val, err := time.ParseDuration(maxIdleTime); err == nil {
			cfg.DBConnMaxIdleTime = val
		}
	}

	// Worker pool config
	if minW := os.Getenv("MIN_WORKERS"); minW != "" {
		if w, err := strconv.Atoi(minW); err == nil {
			cfg.MinWorkers = w
		}
	}

	if maxW := os.Getenv("MAX_WORKERS"); maxW != "" {
		if w, err := strconv.Atoi(maxW); err == nil {
			cfg.MaxWorkers = w
		}
	}

	if minR := os.Getenv("MIN_REQUEST_WORKERS"); minR != "" {
		if w, err := strconv.Atoi(minR); err == nil {
			cfg.MinRequestWorkers = w
		}
	}

	if maxR := os.Getenv("MAX_REQUEST_WORKERS"); maxR != "" {
		if w, err := strconv.Atoi(maxR); err == nil {
			cfg.MaxRequestWorkers = w
		}
	}

	if rbs := os.Getenv("REQUEST_BUFFER_SIZE"); rbs != "" {
		if b, err := strconv.Atoi(rbs); err == nil {
			cfg.RequestBufferSize = b
		}
	}

	if qbs := os.Getenv("QUEUE_BUFFER_SIZE"); qbs != "" {
		if q, err := strconv.Atoi(qbs); err == nil {
			cfg.QueueBufferSize = q
		}
	}

	if st := os.Getenv("SHUTDOWN_TIMEOUT"); st != "" {
		if d, err := time.ParseDuration(st); err == nil {
			cfg.ShutdownTimeout = d
		}
	}

	// Retry config
	if id := os.Getenv("INITIAL_DELAY"); id != "" {
		if d, err := time.ParseDuration(id); err == nil {
			cfg.InitialDelay = d
		}
	}

	if md := os.Getenv("MAX_DELAY"); md != "" {
		if d, err := time.ParseDuration(md); err == nil {
			cfg.MaxDelay = d
		}
	}

	if mr := os.Getenv("MAX_RETRIES"); mr != "" {
		if r, err := strconv.Atoi(mr); err == nil {
			cfg.MaxRetries = r
		}
	}

	if bp := os.Getenv("BACKUP_POLLING_INTERVAL"); bp != "" {
		if d, err := time.ParseDuration(bp); err == nil {
			cfg.BackupPollingInterval = d
		}
	}

	if mep := os.Getenv("MAX_EVENTS_PER_POLL"); mep != "" {
		if i, err := strconv.Atoi(mep); err == nil {
			cfg.MaxEventsPerPoll = i
		}
	}

	if rci := os.Getenv("RETRY_CHECK_INTERVAL"); rci != "" {
		if d, err := time.ParseDuration(rci); err == nil {
			cfg.RetryCheckInterval = d
		}
	}

	if mockEnabled := os.Getenv("MOCK_ENABLED"); mockEnabled != "" {
		if b, err := strconv.ParseBool(mockEnabled); err == nil {
			cfg.MockEnabled = b
		}
	}

	return cfg
}

// GetDSN returns the database connection string
func (c *Config) GetDSN() string {
	return "postgres://" + c.DBUser + ":" + c.DBPassword + "@" + c.DBHost + ":" + c.DBPort + "/" + c.DBName + "?sslmode=disable&search_path=" + c.DBSchema
}
