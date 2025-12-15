package asynclogger

import (
	"fmt"
	"time"
)

// SizeConfig holds the configuration for the async logger with size-based rotation
type SizeConfig struct {
	// LogFilePath is the path to the log file (required)
	LogFilePath string

	// BufferSize is the total buffer size in bytes (default: 64MB)
	BufferSize int

	// NumShards is the number of shards (default: 8)
	NumShards int

	// FlushInterval is the time-based flush trigger (default: 10s)
	FlushInterval time.Duration

	// FlushTimeout is the maximum time to wait for writes to complete before flushing (default: 10ms)
	// If timeout expires, flush proceeds anyway (may result in one corrupted log line)
	FlushTimeout time.Duration

	// MaxFileSize is the maximum file size in bytes before rotation (default: 1GB)
	// Set to 0 to disable rotation. Rotated files are named with timestamp: {baseName}_{YYYY-MM-DD_HH-MM-SS}.log
	MaxFileSize int64

	// PreallocateFileSize is the size to preallocate using fallocate (default: MaxFileSize)
	// Preallocation ensures extents are ready for Direct I/O, improving write performance
	// Set to 0 to use MaxFileSize
	PreallocateFileSize int64
}

// DefaultSizeConfig returns a configuration with baseline defaults for size-based rotation
// logPath is required - the path where logs will be written
func DefaultSizeConfig(logPath string) SizeConfig {
	maxFileSize := int64(1024 * 1024 * 1024) // 1GB default
	return SizeConfig{
		LogFilePath:         logPath,
		BufferSize:          64 * 1024 * 1024,      // 64MB (baseline configuration)
		NumShards:           8,                     // 8 shards
		FlushInterval:       10 * time.Second,      // 10 seconds
		FlushTimeout:        10 * time.Millisecond, // 10ms timeout for write completion
		MaxFileSize:         maxFileSize,           // 1GB default
		PreallocateFileSize: maxFileSize,           // Preallocate same as max file size
	}
}

// Validate checks if the configuration is valid and applies defaults where needed
func (c *SizeConfig) Validate() error {
	if c.LogFilePath == "" {
		return fmt.Errorf("LogFilePath is required")
	}

	if c.BufferSize <= 0 {
		c.BufferSize = 64 * 1024 * 1024 // 64MB default
	}

	if c.NumShards <= 0 {
		c.NumShards = 8 // 8 shards default
	}

	if c.FlushInterval <= 0 {
		c.FlushInterval = 10 * time.Second
	}

	if c.FlushTimeout <= 0 {
		c.FlushTimeout = 10 * time.Millisecond
	}

	// Ensure minimum shard size
	shardSize := c.BufferSize / c.NumShards
	if shardSize < 64*1024 {
		return fmt.Errorf("shard size too small (%d bytes), increase BufferSize or decrease NumShards", shardSize)
	}

	// Set default MaxFileSize if not specified
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = 10 * 1024 * 1024 * 1024 // 10GB default
	}

	// Set PreallocateFileSize to MaxFileSize if not specified
	if c.PreallocateFileSize <= 0 {
		c.PreallocateFileSize = c.MaxFileSize
	}

	// Ensure PreallocateFileSize doesn't exceed MaxFileSize
	if c.PreallocateFileSize > c.MaxFileSize {
		c.PreallocateFileSize = c.MaxFileSize
	}

	return nil
}
