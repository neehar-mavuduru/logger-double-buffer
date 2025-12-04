package logger

import "time"

// Strategy defines the buffer swapping mechanism
type Strategy int

const (
	// Atomic uses atomic.Pointer for lock-free buffer swapping
	Atomic Strategy = iota
	// Mutex uses sync.RWMutex for buffer swapping
	Mutex
	// Sharded uses sharded buffers with mutex-based locking
	Sharded
	// ShardedCAS uses sharded buffers with atomic CAS operations
	ShardedCAS
	// ShardedDoubleBuffer uses double-buffered sharded sets with mutex-based swapping
	ShardedDoubleBuffer
	// ShardedDoubleBufferCAS uses double-buffered sharded sets with atomic CAS swapping
	ShardedDoubleBufferCAS
)

// String returns the string representation of the strategy
func (s Strategy) String() string {
	switch s {
	case Atomic:
		return "atomic"
	case Mutex:
		return "mutex"
	case Sharded:
		return "sharded"
	case ShardedCAS:
		return "sharded-cas"
	case ShardedDoubleBuffer:
		return "sharded-double-buffer"
	case ShardedDoubleBufferCAS:
		return "sharded-double-buffer-cas"
	default:
		return "unknown"
	}
}

// Config holds the configuration for the async logger
type Config struct {
	// BufferSize is the size of each buffer in bytes (default: 1MB)
	BufferSize int

	// FlushInterval is the time-based flush trigger (default: 1s)
	FlushInterval time.Duration

	// LogFilePath is the path to the append-only log file
	LogFilePath string

	// Strategy defines whether to use atomic CAS or mutex for buffer swapping
	Strategy Strategy

	// NumShards is the number of shards for sharded strategies (default: 16)
	// Only used for Sharded and ShardedCAS strategies
	NumShards int
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() Config {
	return Config{
		BufferSize:    1024 * 1024, // 1MB
		FlushInterval: time.Second,
		LogFilePath:   "logs/server.log",
		Strategy:      Atomic,
		NumShards:     16, // Default for sharded strategies
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.BufferSize <= 0 {
		c.BufferSize = 1024 * 1024
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = time.Second
	}
	if c.LogFilePath == "" {
		c.LogFilePath = "logs/server.log"
	}
	if c.NumShards <= 0 {
		c.NumShards = 16
	}
	return nil
}
