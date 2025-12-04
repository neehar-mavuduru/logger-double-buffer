package logger

import (
	"fmt"
	"os"
	"path/filepath"
)

// Logger is the interface for async logging
type Logger interface {
	// Log writes a log message
	Log(msg string)

	// Logf writes a formatted log message
	Logf(format string, args ...interface{})

	// Close gracefully shuts down the logger, flushing all pending logs
	Close() error

	// Stats returns logging statistics
	Stats() Statistics
}

// Statistics holds logger statistics
type Statistics struct {
	TotalLogs       int64
	TotalFlushes    int64
	DroppedLogs     int64
	BytesWritten    int64
	FlushErrors     int64
	CurrentStrategy Strategy
}

// New creates a new async logger based on the provided configuration
func New(config Config) (Logger, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Ensure log directory exists
	logDir := filepath.Dir(config.LogFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in append mode
	file, err := os.OpenFile(config.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create logger based on strategy
	switch config.Strategy {
	case Atomic:
		return newAtomicLogger(config, file)
	case Mutex:
		return newMutexLogger(config, file)
	case Sharded:
		return newShardedLogger(config, file)
	case ShardedCAS:
		return newShardedCASLogger(config, file)
	case ShardedDoubleBuffer:
		return newShardedDoubleBufferMutexLogger(config, file)
	case ShardedDoubleBufferCAS:
		return newShardedDoubleBufferCASLogger(config, file)
	default:
		file.Close()
		return nil, fmt.Errorf("unknown strategy: %v", config.Strategy)
	}
}
