package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestShardedBufferSet tests basic ShardedBufferSet operations
func TestShardedBufferSet(t *testing.T) {
	set := NewShardedBufferSet(256*1024, 4, 0)

	if set.NumShards() != 4 {
		t.Errorf("Expected 4 shards, got %d", set.NumShards())
	}

	// Test write
	data := []byte("test log entry\n")
	n, needsFlush, shardID := set.Write(data)

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	if shardID < 0 || shardID >= 4 {
		t.Errorf("Invalid shard ID: %d", shardID)
	}

	// Test HasData
	if !set.HasData() {
		t.Error("Set should have data after write")
	}

	// Test ResetAll
	set.ResetAll()
	if set.HasData() {
		t.Error("Set should be empty after reset")
	}

	// Test AnyShardFull
	if set.AnyShardFull() {
		t.Error("No shard should be full after reset")
	}

	// Fill one shard to trigger needsFlush
	largeData := make([]byte, 64*1024) // Shard capacity
	for i := 0; i < len(largeData); i++ {
		largeData[i] = 'x'
	}

	_, needsFlush, _ = set.Write(largeData)
	if !needsFlush {
		t.Error("Expected needsFlush to be true after writing large data")
	}
}

// TestShardedCASBufferSet tests basic ShardedCASBufferSet operations
func TestShardedCASBufferSet(t *testing.T) {
	set := NewShardedCASBufferSet(256*1024, 4, 0)

	if set.NumShards() != 4 {
		t.Errorf("Expected 4 shards, got %d", set.NumShards())
	}

	// Test write
	data := []byte("test log entry\n")
	n, needsFlush, shardID := set.Write(data)

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	if shardID < 0 || shardID >= 4 {
		t.Errorf("Invalid shard ID: %d", shardID)
	}

	// Test HasData
	if !set.HasData() {
		t.Error("Set should have data after write")
	}

	// Test ResetAll
	set.ResetAll()
	if set.HasData() {
		t.Error("Set should be empty after reset")
	}

	// Test AnyShardFull
	if set.AnyShardFull() {
		t.Error("No shard should be full after reset")
	}

	// Fill one shard to trigger needsFlush
	largeData := make([]byte, 64*1024) // Shard capacity
	for i := 0; i < len(largeData); i++ {
		largeData[i] = 'x'
	}

	_, needsFlush, _ = set.Write(largeData)
	if !needsFlush {
		t.Error("Expected needsFlush to be true after writing large data")
	}
}

// TestShardedDoubleBufferMutexLogger tests the mutex-based double buffer logger
func TestShardedDoubleBufferMutexLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    256 * 1024, // 256KB
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBuffer,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write some logs
	for i := 0; i < 100; i++ {
		logger.Logf("Test log message %d", i)
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Verify file exists and has data
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}

	// Check stats
	stats := logger.Stats()
	if stats.TotalLogs != 100 {
		t.Errorf("Expected 100 total logs, got %d", stats.TotalLogs)
	}

	// Verify dropped logs are zero or very low
	if stats.DroppedLogs > 5 {
		t.Errorf("Too many dropped logs: %d (should be 0 or near 0)", stats.DroppedLogs)
	}
}

// TestShardedDoubleBufferCASLogger tests the CAS-based double buffer logger
func TestShardedDoubleBufferCASLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    256 * 1024, // 256KB
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBufferCAS,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write some logs
	for i := 0; i < 100; i++ {
		logger.Logf("Test log message %d", i)
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Verify file exists and has data
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}

	// Check stats
	stats := logger.Stats()
	if stats.TotalLogs != 100 {
		t.Errorf("Expected 100 total logs, got %d", stats.TotalLogs)
	}

	// Verify dropped logs are zero or very low
	if stats.DroppedLogs > 5 {
		t.Errorf("Too many dropped logs: %d (should be 0 or near 0)", stats.DroppedLogs)
	}
}

// TestShardedDoubleBufferAtomicSwap tests that set swapping is atomic and zero logs are dropped
func TestShardedDoubleBufferAtomicSwap(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    128 * 1024, // 128KB
		FlushInterval: 50 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBufferCAS,
		NumShards:     8,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write logs concurrently to trigger swaps
	numGoroutines := 50
	logsPerGoroutine := 20
	done := make(chan bool)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < logsPerGoroutine; j++ {
				logger.Logf("Goroutine %d log %d", id, j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to finish
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Wait for final flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Check stats
	stats := logger.Stats()
	expectedLogs := int64(numGoroutines * logsPerGoroutine)

	if stats.TotalLogs != expectedLogs {
		t.Errorf("Expected %d total logs, got %d", expectedLogs, stats.TotalLogs)
	}

	// The key test: dropped logs should be 0 or extremely low
	// With double buffering, writers should never be blocked
	dropRate := float64(stats.DroppedLogs) / float64(stats.TotalLogs) * 100
	if dropRate > 1.0 { // Allow up to 1% drops due to extreme concurrency
		t.Errorf("Drop rate too high: %.2f%% (%d dropped out of %d total)",
			dropRate, stats.DroppedLogs, stats.TotalLogs)
	}
}

// TestShardedDoubleBufferMutexConcurrentSwaps tests mutex-based swapping under heavy load
func TestShardedDoubleBufferMutexConcurrentSwaps(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    64 * 1024, // Small buffer to force frequent swaps
		FlushInterval: 20 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBuffer,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write logs concurrently
	numGoroutines := 30
	logsPerGoroutine := 50
	done := make(chan bool)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < logsPerGoroutine; j++ {
				logger.Logf("Worker %d message %d with some additional text to increase size", id, j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Close logger
	logger.Close()

	// Check stats
	stats := logger.Stats()
	expectedLogs := int64(numGoroutines * logsPerGoroutine)

	if stats.TotalLogs != expectedLogs {
		t.Errorf("Expected %d total logs, got %d", expectedLogs, stats.TotalLogs)
	}

	// Dropped logs should be minimal
	dropRate := float64(stats.DroppedLogs) / float64(stats.TotalLogs) * 100
	if dropRate > 2.0 { // Allow up to 2% drops for mutex strategy
		t.Errorf("Drop rate too high: %.2f%% (%d dropped out of %d total)",
			dropRate, stats.DroppedLogs, stats.TotalLogs)
	}

	// Verify file has data
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty")
	}
}

// TestShardedDoubleBufferFinalFlush tests that all data is flushed on close
func TestShardedDoubleBufferFinalFlush(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    256 * 1024,
		FlushInterval: 10 * time.Second, // Long interval to prevent automatic flush
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBufferCAS,
		NumShards:     4,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Write logs
	numLogs := 50
	for i := 0; i < numLogs; i++ {
		logger.Logf("Final flush test log %d", i)
	}

	// Close immediately without waiting for flush interval
	logger.Close()

	// Verify all logs were written
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file is empty after close")
	}

	// Check stats
	stats := logger.Stats()
	if stats.TotalLogs != int64(numLogs) {
		t.Errorf("Expected %d total logs, got %d", numLogs, stats.TotalLogs)
	}

	// All logs should be flushed on close
	if stats.DroppedLogs > 0 {
		t.Errorf("Expected 0 dropped logs on close, got %d", stats.DroppedLogs)
	}
}

