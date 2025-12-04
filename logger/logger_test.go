package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestLogBuffer tests basic buffer operations
func TestLogBuffer(t *testing.T) {
	buffer := NewLogBuffer(1024, 1)

	// Test initial state
	if buffer.Offset() != 0 {
		t.Errorf("Expected offset 0, got %d", buffer.Offset())
	}
	if buffer.IsFull() {
		t.Error("Buffer should not be full initially")
	}

	// Test write
	data := []byte("test log entry\n")
	n, needsFlush := buffer.Write(data)
	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}
	if needsFlush {
		t.Error("Buffer should not need flush after small write")
	}

	// Test buffer content
	bytes := buffer.Bytes()
	if string(bytes) != string(data) {
		t.Errorf("Expected %s, got %s", data, bytes)
	}

	// Test reset
	buffer.Reset()
	if buffer.Offset() != 0 {
		t.Errorf("Expected offset 0 after reset, got %d", buffer.Offset())
	}
}

// TestLogBufferOverflow tests buffer overflow handling
func TestLogBufferOverflow(t *testing.T) {
	buffer := NewLogBuffer(100, 1)

	// Fill buffer beyond capacity
	largeData := make([]byte, 200)
	for i := range largeData {
		largeData[i] = 'A'
	}

	n, needsFlush := buffer.Write(largeData)
	if n != 0 {
		t.Errorf("Expected 0 bytes written on overflow, got %d", n)
	}
	if !needsFlush {
		t.Error("Buffer should need flush when full")
	}
	if !buffer.IsFull() {
		t.Error("Buffer should be marked as full")
	}
}

// TestLogBufferConcurrentWrites tests concurrent writes to buffer
func TestLogBufferConcurrentWrites(t *testing.T) {
	buffer := NewLogBuffer(10000, 1)
	numGoroutines := 100
	writesPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				msg := fmt.Sprintf("log-%d-%d\n", id, j)
				buffer.Write([]byte(msg))
			}
		}(i)
	}

	wg.Wait()

	// Verify offset is correct (some writes may be rejected if buffer fills)
	expectedMinOffset := int32(numGoroutines * writesPerGoroutine * 8) // rough estimate, accounting for rejections
	if buffer.Offset() < expectedMinOffset {
		t.Logf("Warning: Expected offset >= %d, got %d (some writes may have been rejected)", expectedMinOffset, buffer.Offset())
	}
}

// TestAtomicLoggerBasic tests basic atomic logger functionality
func TestAtomicLoggerBasic(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      Atomic,
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

	// Close logger explicitly before reading (defer will be no-op due to idempotent Close)
	logger.Close()

	// Verify file contents
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Error("Log file is empty")
	}
}

// TestMutexLoggerBasic tests basic mutex logger functionality
func TestMutexLoggerBasic(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      Mutex,
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

	// Close logger explicitly before reading (defer will be no-op due to idempotent Close)
	logger.Close()

	// Verify file contents
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Error("Log file is empty")
	}
}

// TestLoggerConcurrentWrites tests concurrent writes
func TestLoggerConcurrentWrites(t *testing.T) {
	strategies := []Strategy{Atomic, Mutex}

	for _, strategy := range strategies {
		t.Run(strategy.String(), func(t *testing.T) {
			tmpDir := t.TempDir()
			logFile := filepath.Join(tmpDir, "test.log")

			config := Config{
				BufferSize:    10000,
				FlushInterval: 50 * time.Millisecond,
				LogFilePath:   logFile,
				Strategy:      strategy,
			}

			logger, err := New(config)
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			numGoroutines := 50
			logsPerGoroutine := 100

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

// TestLoggerBufferOverflow tests behavior when buffer overflows
func TestLoggerBufferOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    500, // Small buffer
		FlushInterval: 1 * time.Second,
		LogFilePath:   logFile,
		Strategy:      Atomic,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write logs to overflow buffer
	for i := 0; i < 100; i++ {
		logger.Logf("This is test log number %d with some content", i)
	}

	// Wait for flushes
	time.Sleep(500 * time.Millisecond)

	stats := logger.Stats()
	if stats.TotalFlushes == 0 {
		t.Error("Expected at least one flush")
	}

	logger.Close()
}

// TestLoggerShutdown tests graceful shutdown
func TestLoggerShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 10 * time.Second, // Long interval
		LogFilePath:   logFile,
		Strategy:      Atomic,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Write logs without waiting for periodic flush
	for i := 0; i < 10; i++ {
		logger.Logf("Shutdown test log %d", i)
	}

	// Close immediately (should flush pending logs)
	if err := logger.Close(); err != nil {
		t.Errorf("Failed to close logger: %v", err)
	}

	// Verify logs were written
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected logs to be flushed on shutdown")
	}
}

// TestLoggerStats tests statistics tracking
func TestLoggerStats(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	config := Config{
		BufferSize:    1024,
		FlushInterval: 100 * time.Millisecond,
		LogFilePath:   logFile,
		Strategy:      Atomic,
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Write logs
	numLogs := 50
	for i := 0; i < numLogs; i++ {
		logger.Logf("Test log %d", i)
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	stats := logger.Stats()
	if stats.TotalLogs != int64(numLogs) {
		t.Errorf("Expected %d total logs, got %d", numLogs, stats.TotalLogs)
	}
	if stats.TotalFlushes == 0 {
		t.Error("Expected at least one flush")
	}
	if stats.BytesWritten == 0 {
		t.Error("Expected some bytes written")
	}

	logger.Close()
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name: "valid config",
			config: Config{
				BufferSize:    1024,
				FlushInterval: time.Second,
				LogFilePath:   "test.log",
				Strategy:      Atomic,
			},
			valid: true,
		},
		{
			name: "zero buffer size",
			config: Config{
				BufferSize:    0,
				FlushInterval: time.Second,
				LogFilePath:   "test.log",
				Strategy:      Atomic,
			},
			valid: true, // Should use default
		},
		{
			name: "empty log path",
			config: Config{
				BufferSize:    1024,
				FlushInterval: time.Second,
				LogFilePath:   "",
				Strategy:      Atomic,
			},
			valid: true, // Should use default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
		})
	}
}

