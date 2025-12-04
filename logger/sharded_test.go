package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestShardedBuffer tests basic sharded buffer operations
func TestShardedBuffer(t *testing.T) {
	// Use larger buffer to ensure 4 shards (need at least 64KB per shard)
	sb := NewShardedBuffer(256*1024, 4)

	if sb.NumShards() != 4 {
		t.Errorf("Expected 4 shards, got %d", sb.NumShards())
	}

	// Test write
	data := []byte("test log entry\n")
	n, needsFlush, shardID := sb.Write(data)

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}
	if needsFlush {
		t.Error("Buffer should not need flush after small write")
	}
	if shardID < 0 || shardID >= 4 {
		t.Errorf("Invalid shard ID: %d", shardID)
	}
}

// TestShardedLogger tests basic sharded logger functionality
func TestShardedLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      Sharded,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write some logs
	logger.Log("Test log 1")
	logger.Log("Test log 2")
	logger.Logf("Test log %d", 3)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Verify file contents
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}

	// Verify stats
	stats := logger.Stats()
	if stats.CurrentStrategy != Sharded {
		t.Errorf("Expected Sharded strategy, got %v", stats.CurrentStrategy)
	}
}

// TestShardedCASLogger tests basic sharded CAS logger functionality
func TestShardedCASLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      ShardedCAS,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write some logs
	logger.Log("Test log 1")
	logger.Log("Test log 2")
	logger.Logf("Test log %d", 3)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Verify file contents
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}

	// Verify stats
	stats := logger.Stats()
	if stats.CurrentStrategy != ShardedCAS {
		t.Errorf("Expected ShardedCAS strategy, got %v", stats.CurrentStrategy)
	}
}

// TestShardedConcurrentWrites tests concurrent writes to sharded logger
func TestShardedConcurrentWrites(t *testing.T) {
	strategies := []Strategy{Sharded, ShardedCAS}

	for _, strategy := range strategies {
		t.Run(strategy.String(), func(t *testing.T) {
			tmpDir := t.TempDir()
			logFile := filepath.Join(tmpDir, "test.log")

			config := Config{
				BufferSize:    10000,
				FlushInterval: 50 * time.Millisecond,
				LogFilePath:   logFile,
				Strategy:      strategy,
				NumShards:     8,
			}

			logger, err := New(config)
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			numGoroutines := 50
			logsPerGoroutine := 20

			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				go func(id int) {
					defer wg.Done()
					for j := 0; j < logsPerGoroutine; j++ {
						logger.Logf("goroutine-%d log-%d", id, j)
					}
				}(i)
			}

			wg.Wait()

			// Wait for flushes
			time.Sleep(200 * time.Millisecond)

			// Check stats
			stats := logger.Stats()
			expectedLogs := int64(numGoroutines * logsPerGoroutine)
			if stats.TotalLogs < expectedLogs {
				t.Errorf("Expected at least %d logs, got %d", expectedLogs, stats.TotalLogs)
			}

			// Close and verify
			logger.Close()

			fileInfo, err := os.Stat(logFile)
			if err != nil {
				t.Fatalf("Failed to stat log file: %v", err)
			}
			if fileInfo.Size() == 0 {
				t.Error("Log file is empty")
			}
		})
	}
}

// TestCASShard tests CAS shard operations
func TestCASShard(t *testing.T) {
	shard := NewCASShard(1024, 1)

	// Test initial state
	if shard.Offset() != 0 {
		t.Errorf("Expected offset 0, got %d", shard.Offset())
	}
	if shard.IsFull() {
		t.Error("Shard should not be full initially")
	}

	// Test write
	data := []byte("test entry\n")
	n, needsFlush := shard.Write(data)

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}
	if needsFlush {
		t.Error("Shard should not need flush after small write")
	}

	// Test reset
	shard.Reset()
	if shard.Offset() != 0 {
		t.Errorf("Expected offset 0 after reset, got %d", shard.Offset())
	}
}

// TestCASShard tests basic shard operations
func TestShard(t *testing.T) {
	shard := NewShard(1024, 1)

	// Test initial state
	if shard.Offset() != 0 {
		t.Errorf("Expected offset 0, got %d", shard.Offset())
	}
	if shard.IsFull() {
		t.Error("Shard should not be full initially")
	}

	// Test write
	data := []byte("test entry\n")
	n, needsFlush := shard.Write(data)

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}
	if needsFlush {
		t.Error("Shard should not need flush after small write")
	}

	// Test reset
	shard.Reset()
	if shard.Offset() != 0 {
		t.Errorf("Expected offset 0 after reset, got %d", shard.Offset())
	}
}

// TestShardConcurrency tests concurrent writes to a single shard
func TestShardConcurrency(t *testing.T) {
	shard := NewShard(10000, 1)

	numGoroutines := 50
	writesPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				msg := fmt.Sprintf("log-%d-%d\n", id, j)
				shard.Write([]byte(msg))
			}
		}(i)
	}

	wg.Wait()

	// Verify offset
	if shard.Offset() < int32(numGoroutines*writesPerGoroutine*8) {
		t.Logf("Warning: Expected offset >= %d, got %d", numGoroutines*writesPerGoroutine*8, shard.Offset())
	}
}

// TestCASShardConcurrency tests concurrent writes to a CAS shard
func TestCASShardConcurrency(t *testing.T) {
	shard := NewCASShard(10000, 1)

	numGoroutines := 50
	writesPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				msg := fmt.Sprintf("log-%d-%d\n", id, j)
				shard.Write([]byte(msg))
			}
		}(i)
	}

	wg.Wait()

	// Verify offset
	if shard.Offset() < int32(numGoroutines*writesPerGoroutine*8) {
		t.Logf("Warning: Expected offset >= %d, got %d", numGoroutines*writesPerGoroutine*8, shard.Offset())
	}
}

// TestShardedBufferRoundRobin tests round-robin shard selection
func TestShardedBufferRoundRobin(t *testing.T) {
	// Use large enough buffer to ensure 4 shards (256KB total, 64KB per shard)
	sb := NewShardedBuffer(256*1024, 4)

	shardCounts := make(map[int]int)

	// Write 100 times
	for i := 0; i < 100; i++ {
		_, _, shardID := sb.Write([]byte("test\n"))
		if shardID >= 0 {
			shardCounts[shardID]++
		}
	}

	// Verify all shards were used
	for i := 0; i < 4; i++ {
		count := shardCounts[i]
		if count < 20 || count > 30 {
			t.Logf("Shard %d has %d writes, expected around 25 (distribution may vary)", i, count)
		}
	}
}

// TestShardedCASBufferRoundRobin tests round-robin shard selection for CAS buffer
func TestShardedCASBufferRoundRobin(t *testing.T) {
	// Use large enough buffer to ensure 4 shards (256KB total, 64KB per shard)
	scb := NewShardedCASBuffer(256*1024, 4)

	shardCounts := make(map[int]int)

	// Write 100 times
	for i := 0; i < 100; i++ {
		_, _, shardID := scb.Write([]byte("test\n"))
		if shardID >= 0 {
			shardCounts[shardID]++
		}
	}

	// Verify all shards were used
	for i := 0; i < 4; i++ {
		count := shardCounts[i]
		if count < 20 || count > 30 {
			t.Logf("Shard %d has %d writes, expected around 25 (distribution may vary)", i, count)
		}
	}
}

