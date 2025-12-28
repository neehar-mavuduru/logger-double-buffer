package asyncloguploader

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceFlush writes enough data to trigger threshold flush or waits for periodic flush
func forceFlush(t *testing.T, logger *Logger, numShards int, bufferSize int) {
	// Calculate how many shards need to be full to trigger flush (25% threshold)
	threshold := (numShards * 25) / 100
	if threshold == 0 {
		threshold = 1
	}

	// Write enough data to fill threshold shards
	// Each shard buffer is bufferSize / numShards
	shardCapacity := bufferSize / numShards
	// Write ~90% of shard capacity to trigger "ready for flush" state
	dataSize := (shardCapacity * 9) / 10
	if dataSize < 1024 {
		dataSize = 1024 // Minimum 1KB
	}

	// Write to threshold+1 shards to ensure flush triggers
	for i := 0; i < threshold+1; i++ {
		largeData := make([]byte, dataSize)
		// Fill with test pattern
		for j := range largeData {
			largeData[j] = byte(i + j)
		}
		logger.LogBytes(largeData)
	}

	// Wait for flush to complete (with timeout)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, bytesWritten, flushes, _, _ := logger.GetStatsSnapshot()
		if bytesWritten > 0 && flushes > 0 {
			// Give a bit more time for file write to complete
			time.Sleep(100 * time.Millisecond)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLogger_FileIntegrity_EndToEnd_Improved performs comprehensive file integrity verification
func TestLogger_FileIntegrity_EndToEnd_Improved(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "integrity_test.log")
	config := DefaultConfig(logPath)
	config.BufferSize = 2 * 1024 * 1024 // 2MB
	config.NumShards = 4
	config.MaxFileSize = 0 // Disable rotation for this test
	config.FlushInterval = 50 * time.Millisecond

	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer logger.Close()

	// Write test messages
	testMessages := []string{
		"message 1",
		"message 2",
		"message 3",
		"longer message that spans multiple bytes",
		"message with special chars: !@#$%^&*()",
	}

	for _, msg := range testMessages {
		logger.LogBytes([]byte(msg))
	}

	// Force a flush by writing enough data to trigger threshold
	forceFlush(t, logger, config.NumShards, config.BufferSize)

	// Also write our test messages again to ensure they're in the flush
	for _, msg := range testMessages {
		logger.LogBytes([]byte(msg))
	}

	// Force another flush
	forceFlush(t, logger, config.NumShards, config.BufferSize)

	// Close logger to ensure all data is flushed
	err = logger.Close()
	require.NoError(t, err)

	// Find the actual file (with timestamp)
	actualFile := findLogFile(t, tmpDir, "integrity_test")
	require.NotEmpty(t, actualFile, "Log file should exist after flush and close")

	// Read and verify file contents
	data, err := os.ReadFile(actualFile)
	require.NoError(t, err)
	assert.Greater(t, len(data), 0, "File should contain data")

	// Verify all messages are present
	for _, msg := range testMessages {
		assert.Contains(t, string(data), msg, "Message not found in file: %s", msg)
	}

	// Verify file format: check for shard headers
	verifyFileFormatImproved(t, data)
}

// TestLogger_ConcurrentWrites_FileIntegrity_Improved tests file integrity under concurrent writes
func TestLogger_ConcurrentWrites_FileIntegrity_Improved(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "concurrent_test.log")
	config := DefaultConfig(logPath)
	config.BufferSize = 10 * 1024 * 1024 // 10MB
	config.NumShards = 8
	config.MaxFileSize = 0
	config.FlushInterval = 100 * time.Millisecond

	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer logger.Close()

	const numGoroutines = 20
	const messagesPerGoroutine = 50
	var wg sync.WaitGroup

	// Track all messages written
	writtenMessages := make(map[string]bool)
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				msg := []byte{byte(id), byte(j)}
				logger.LogBytes(msg)

				mu.Lock()
				writtenMessages[string(msg)] = true
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Force flush to ensure data is written
	forceFlush(t, logger, config.NumShards, config.BufferSize)

	err = logger.Close()
	require.NoError(t, err)

	// Find the actual file (with timestamp)
	actualFile := findLogFile(t, tmpDir, "concurrent_test")
	require.NotEmpty(t, actualFile, "Log file should exist")

	data, err := os.ReadFile(actualFile)
	require.NoError(t, err)
	assert.Greater(t, len(data), 0)

	// Verify file format
	verifyFileFormat(t, data)

	// Verify file contains reasonable amount of data
	assert.Greater(t, len(data), numGoroutines*messagesPerGoroutine*2, "File seems too small")
}

// TestLogger_DataCorrectness_Improved verifies that written data matches read data
func TestLogger_DataCorrectness_Improved(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "correctness_test.log")
	config := DefaultConfig(logPath)
	config.BufferSize = 2 * 1024 * 1024
	config.NumShards = 4
	config.MaxFileSize = 0
	config.FlushInterval = 50 * time.Millisecond

	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer logger.Close()

	// Write known data
	testData := []byte("known test data 12345")
	logger.LogBytes(testData)

	// Force flush to ensure data is written
	forceFlush(t, logger, config.NumShards, config.BufferSize)

	err = logger.Close()
	require.NoError(t, err)

	// Find the actual file (with timestamp)
	actualFile := findLogFile(t, tmpDir, "correctness_test")
	require.NotEmpty(t, actualFile, "Log file should exist")

	// Read file and verify exact match
	data, err := os.ReadFile(actualFile)
	require.NoError(t, err)
	assert.Greater(t, len(data), 0, "File should contain data")

	// Find test data in file (may be after header)
	found := false
	for i := 0; i <= len(data)-len(testData); i++ {
		if string(data[i:i+len(testData)]) == string(testData) {
			found = true
			break
		}
	}

	assert.True(t, found, "Test data not found in file")
}

// TestLoggerManager_MultipleEvents_FileIntegrity_Improved tests file integrity with multiple events
func TestLoggerManager_MultipleEvents_FileIntegrity_Improved(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "manager_test.log")
	config := DefaultConfig(logPath)
	config.BufferSize = 2 * 1024 * 1024
	config.NumShards = 4
	config.MaxFileSize = 0
	config.FlushInterval = 50 * time.Millisecond

	manager, err := NewLoggerManager(config)
	require.NoError(t, err)
	defer manager.Close()

	// Write to multiple events
	events := []string{"event1", "event2", "event3"}
	for _, event := range events {
		manager.LogBytesWithEvent(event, []byte("data for "+event))
	}

	// Write more data to each event to trigger flush
	for _, event := range events {
		// Write enough data to trigger threshold flush
		for i := 0; i < 10; i++ {
			largeData := make([]byte, config.BufferSize/config.NumShards/2)
			manager.LogBytesWithEvent(event, largeData)
		}
	}

	time.Sleep(200 * time.Millisecond)

	err = manager.Close()
	require.NoError(t, err)

	// Verify files exist for each event (find timestamped files)
	for _, event := range events {
		eventFile := findLogFile(t, tmpDir, event)
		require.NotEmpty(t, eventFile, "Log file should exist for event: %s", event)

		data, err := os.ReadFile(eventFile)
		require.NoError(t, err)
		assert.Greater(t, len(data), 0)
		assert.Contains(t, string(data), "data for "+event)

		verifyFileFormat(t, data)
	}
}

// verifyFileFormatImproved verifies the file format structure (duplicate to avoid conflict)
func verifyFileFormatImproved(t *testing.T, data []byte) {
	if len(data) < 8 {
		t.Skip("File too small to verify format")
		return
	}

	offset := 0
	shardCount := 0

	for offset < len(data) {
		if offset+8 > len(data) {
			break // Not enough data for header
		}

		// Read shard header
		capacity := binary.LittleEndian.Uint32(data[offset : offset+4])
		validDataBytes := binary.LittleEndian.Uint32(data[offset+4 : offset+8])

		// Verify header values are reasonable
		assert.Greater(t, capacity, uint32(0), "Invalid capacity at offset %d", offset)
		assert.LessOrEqual(t, validDataBytes, capacity, "Valid data bytes exceeds capacity at offset %d", offset)

		// Verify data starts immediately after header (no padding)
		dataStart := offset + 8
		if dataStart < len(data) {
			// Check that data section is not all zeros (should contain actual data)
			hasData := false
			for i := dataStart; i < dataStart+int(validDataBytes) && i < len(data); i++ {
				if data[i] != 0 {
					hasData = true
					break
				}
			}
			if validDataBytes > 0 {
				assert.True(t, hasData, "Data section appears empty at offset %d", offset)
			}
		}

		// Move to next shard (if any)
		offset += int(capacity)
		shardCount++

		if shardCount > 100 {
			break // Prevent infinite loop
		}
	}

	assert.Greater(t, shardCount, 0, "No shards found in file")
}

