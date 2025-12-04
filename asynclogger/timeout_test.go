package asynclogger

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestFlushTimeout_SimulateSlowWrites tests the timeout scenario
// where writes take longer than FlushTimeout to complete
func TestFlushTimeout_SimulateSlowWrites(t *testing.T) {
	// Create temporary log file
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "timeout_test.log")

	// Use very short timeout to trigger timeout easily
	config := Config{
		LogFilePath:   logFile,
		BufferSize:    1024 * 1024,            // 1MB buffer
		NumShards:     2,                      // 2 shards for simplicity
		FlushInterval: 100 * time.Millisecond, // Fast flush interval
		FlushTimeout:  1 * time.Millisecond,   // Very short timeout (1ms)
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	// Channel to coordinate slow writes
	slowWriteStart := make(chan struct{})
	slowWriteDone := make(chan struct{})
	var wg sync.WaitGroup

	// Start a goroutine that does slow writes (large data that takes time to copy)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(slowWriteDone)

		// Wait for signal to start slow write
		<-slowWriteStart

		// Create a large log entry (200KB) that will take time to copy
		// This simulates a slow write that exceeds the 1ms timeout
		largeData := make([]byte, 200*1024)
		for i := range largeData {
			largeData[i] = byte('A' + (i % 26))
		}
		// Add newline at end
		largeData[len(largeData)-1] = '\n'

		// This write will take time due to size (copying 200KB)
		logger.LogBytes(largeData)
	}()

	// Write some normal entries first (these should complete normally)
	for i := 0; i < 10; i++ {
		msg := fmt.Sprintf("Normal entry %d\n", i)
		logger.LogBytes([]byte(msg))
	}

	// Signal slow write to start
	close(slowWriteStart)

	// Give slow write time to start (CAS succeeds, writesStarted++), but not finish copying
	time.Sleep(2 * time.Millisecond)

	// Manually trigger swap to flush (this should cause timeout)
	// The slow write is still copying data, so GetData() should timeout
	activeSet := logger.activeSet.Load()
	if activeSet != nil && activeSet.HasData() {
		logger.trySwap()
	}

	// Wait for flush to complete (should timeout waiting for slow write)
	time.Sleep(20 * time.Millisecond)

	// Wait for slow write to complete
	<-slowWriteDone
	wg.Wait()

	// Close logger to flush any remaining data
	logger.Close()

	// Read and inspect the log file
	fileData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Parse and analyze the file
	analyzeLogFile(t, fileData, logFile)
}

// analyzeLogFile parses the log file and checks for incomplete writes
func analyzeLogFile(t *testing.T, data []byte, logFile string) {
	t.Logf("=== Log File Analysis ===")
	t.Logf("File: %s", logFile)
	t.Logf("Total file size: %d bytes", len(data))

	offset := 0
	shardNum := 0
	completeEntries := 0
	incompleteEntries := 0
	var incompleteDetails []string

	for offset < len(data) {
		if offset+8 > len(data) {
			t.Logf("WARNING: Incomplete shard header at offset %d", offset)
			break
		}

		// Read shard header
		capacity := binary.LittleEndian.Uint32(data[offset : offset+4])
		validDataBytes := binary.LittleEndian.Uint32(data[offset+4 : offset+8])

		t.Logf("\n--- Shard %d ---", shardNum)
		t.Logf("Header offset: %d", offset)
		t.Logf("Capacity: %d bytes", capacity)
		t.Logf("Valid data bytes: %d bytes", validDataBytes)

		// Safety check: ensure validDataBytes doesn't exceed remaining file size
		remainingBytes := len(data) - offset - 8
		if int(validDataBytes) > remainingBytes {
			t.Logf("WARNING: validDataBytes (%d) exceeds remaining file size (%d)", validDataBytes, remainingBytes)
			validDataBytes = uint32(remainingBytes)
		}

		offset += 8 // Skip header

		// Parse log entries in this shard
		shardEnd := offset + int(validDataBytes)
		if shardEnd > len(data) {
			shardEnd = len(data)
		}
		entryOffset := offset

		for entryOffset < shardEnd {
			if entryOffset+4 > shardEnd {
				t.Logf("WARNING: Incomplete length prefix at offset %d", entryOffset)
				incompleteEntries++
				incompleteDetails = append(incompleteDetails, fmt.Sprintf("Shard %d: Incomplete length prefix at offset %d", shardNum, entryOffset))
				break
			}

			// Read entry length
			entryLength := binary.LittleEndian.Uint32(data[entryOffset : entryOffset+4])
			entryOffset += 4

			// Validate entry length (sanity check)
			if entryLength == 0 || entryLength > 10*1024*1024 { // Max 10MB per entry
				t.Logf("⚠ INVALID ENTRY LENGTH at offset %d: %d bytes (likely corrupted)", entryOffset-4, entryLength)
				incompleteEntries++
				incompleteDetails = append(incompleteDetails, fmt.Sprintf("Shard %d: Invalid entry length at offset %d (%d bytes)",
					shardNum, entryOffset-4, entryLength))
				// Can't recover from invalid length prefix - stop parsing this shard
				break
			}

			if entryOffset+int(entryLength) > shardEnd {
				t.Logf("⚠ INCOMPLETE ENTRY DETECTED:")
				t.Logf("  Offset: %d", entryOffset-4)
				t.Logf("  Expected length: %d bytes", entryLength)
				t.Logf("  Available bytes: %d bytes", shardEnd-entryOffset)
				t.Logf("  Missing bytes: %d bytes", int(entryLength)-(shardEnd-entryOffset))

				// Show partial data
				if entryOffset < shardEnd {
					partial := data[entryOffset:shardEnd]
					preview := string(partial)
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					t.Logf("  Partial data preview: %q", preview)
				}

				// RECOVERY: Skip to next entry using length prefix
				// Length prefix tells us where next entry starts: currentOffset + 4 + entryLength
				nextEntryOffset := entryOffset - 4 + 4 + int(entryLength) // Skip length prefix (4) + data (entryLength)
				t.Logf("  → Skipping to next entry at offset %d (using length prefix)", nextEntryOffset)

				incompleteEntries++
				incompleteDetails = append(incompleteDetails, fmt.Sprintf("Shard %d: Incomplete entry at offset %d (expected %d bytes, got %d bytes) - SKIPPED to offset %d",
					shardNum, entryOffset-4, entryLength, shardEnd-entryOffset, nextEntryOffset))

				// Check if we can continue to next entry
				if nextEntryOffset <= shardEnd {
					entryOffset = nextEntryOffset
					continue // Continue parsing next entry
				} else {
					// Next entry would be beyond shard boundary
					break
				}
			}

			// Read entry data (complete entry)
			entryData := data[entryOffset : entryOffset+int(entryLength)]
			entryOffset += int(entryLength)

			completeEntries++
			if completeEntries <= 5 {
				// Log first few entries
				preview := string(entryData)
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				t.Logf("Entry %d: length=%d, preview=%q", completeEntries, entryLength, preview)
			}
		}

		// Move to next shard (aligned to 512 bytes for Direct I/O)
		// Safety check: ensure we don't go beyond file size
		nextShardStart := offset + int(capacity)
		if nextShardStart > len(data) {
			// No more data, we're done
			break
		}
		alignedOffset := ((nextShardStart + 511) / 512) * 512
		if alignedOffset > len(data) {
			// Aligned offset exceeds file size, we're done
			break
		}
		offset = alignedOffset
		shardNum++
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Total shards: %d", shardNum)
	t.Logf("Complete entries: %d", completeEntries)
	t.Logf("Incomplete entries: %d", incompleteEntries)

	if incompleteEntries > 0 {
		t.Logf("\n✓ TIMEOUT SCENARIO DETECTED:")
		t.Logf("  Found %d incomplete entries - this indicates GetData() timed out", incompleteEntries)
		t.Logf("  Details:")
		for _, detail := range incompleteDetails {
			t.Logf("    - %s", detail)
		}
		t.Logf("\n  This is EXPECTED behavior when FlushTimeout expires before writes complete.")
		t.Logf("  The incomplete entry represents data that was being written when flush occurred.")
	} else {
		t.Logf("\n⚠ No incomplete entries found")
		t.Logf("  This could mean:")
		t.Logf("    1. All writes completed before timeout")
		t.Logf("    2. Timeout didn't occur (writes were fast enough)")
		t.Logf("    3. Need to increase data size or reduce timeout further")
	}
}
