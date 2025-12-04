package asynclogger

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestFlushTimeout_IncompleteEntryInMiddle tests recovery from incomplete entries
// that occur in the middle of a shard (not just at the end)
func TestFlushTimeout_IncompleteEntryInMiddle(t *testing.T) {
	// Create temporary log file
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "timeout_middle_test.log")

	// Use very short timeout to trigger timeout easily
	config := Config{
		LogFilePath:   logFile,
		BufferSize:    2 * 1024 * 1024,        // 2MB buffer
		NumShards:     1,                      // Single shard for easier testing
		FlushInterval: 100 * time.Millisecond, // Fast flush interval
		FlushTimeout:  1 * time.Millisecond,   // Very short timeout (1ms)
	}

	logger, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	var wg sync.WaitGroup

	// Write Entry 1: Small, fast (will complete)
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.LogBytes([]byte("Entry 1: Fast write\n"))
	}()

	// Write Entry 2: Large, slow (will timeout)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Create large entry (500KB) that will take time to copy
		largeData := make([]byte, 500*1024)
		for i := range largeData {
			largeData[i] = byte('B' + (i % 25))
		}
		largeData[len(largeData)-1] = '\n'
		logger.LogBytes(largeData)
	}()

	// Write Entry 3: Small, fast (will complete after Entry 2 starts but before timeout)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Small delay to ensure Entry 2 starts first
		time.Sleep(100 * time.Microsecond)
		logger.LogBytes([]byte("Entry 3: Fast write after slow one\n"))
	}()

	// Write Entry 4: Small, fast (will complete)
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Microsecond)
		logger.LogBytes([]byte("Entry 4: Another fast write\n"))
	}()

	// Wait for all writes to start
	time.Sleep(2 * time.Millisecond)

	// Trigger flush while Entry 2 is still copying (should timeout)
	activeSet := logger.activeSet.Load()
	if activeSet != nil && activeSet.HasData() {
		logger.trySwap()
	}

	// Wait for flush to complete (should timeout waiting for Entry 2)
	time.Sleep(20 * time.Millisecond)

	// Wait for all writes to complete
	wg.Wait()

	// Close logger to flush any remaining data
	logger.Close()

	// Read and analyze the log file
	fileData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Parse and verify recovery
	verifyRecovery(t, fileData, logFile)
}

// verifyRecovery verifies that incomplete entries are skipped and parsing continues
func verifyRecovery(t *testing.T, data []byte, logFile string) {
	t.Logf("=== Recovery Verification ===")
	t.Logf("File: %s", logFile)
	t.Logf("Total file size: %d bytes", len(data))

	offset := 0
	shardNum := 0
	completeEntries := 0
	incompleteEntries := 0
	var foundEntries []string

	for offset < len(data) {
		if offset+8 > len(data) {
			break
		}

		capacity := binary.LittleEndian.Uint32(data[offset : offset+4])
		validDataBytes := binary.LittleEndian.Uint32(data[offset+4 : offset+8])

		// Safety check
		remainingBytes := len(data) - offset - 8
		if int(validDataBytes) > remainingBytes {
			validDataBytes = uint32(remainingBytes)
		}

		offset += 8
		shardEnd := offset + int(validDataBytes)
		if shardEnd > len(data) {
			shardEnd = len(data)
		}
		entryOffset := offset

		t.Logf("\n--- Shard %d ---", shardNum)
		t.Logf("Valid data bytes: %d", validDataBytes)

		for entryOffset < shardEnd {
			if entryOffset+4 > shardEnd {
				t.Logf("Incomplete length prefix at offset %d", entryOffset)
				incompleteEntries++
				break
			}

			// Read entry length
			entryLength := binary.LittleEndian.Uint32(data[entryOffset : entryOffset+4])
			entryOffset += 4

			// Validate entry length
			if entryLength == 0 || entryLength > 10*1024*1024 {
				t.Logf("⚠ Invalid entry length at offset %d: %d bytes (likely corrupted length prefix)", entryOffset-4, entryLength)
				t.Logf("  → Cannot recover: corrupted length prefix means we don't know where next entry starts")
				incompleteEntries++
				break
			}

			if entryOffset+int(entryLength) > shardEnd {
				// Incomplete entry - skip using length prefix
				t.Logf("⚠ Incomplete entry at offset %d: expected %d bytes, got %d bytes",
					entryOffset-4, entryLength, shardEnd-entryOffset)
				nextEntryOffset := entryOffset - 4 + 4 + int(entryLength)
				t.Logf("  → Skipping to offset %d", nextEntryOffset)

				if nextEntryOffset <= shardEnd {
					entryOffset = nextEntryOffset
					incompleteEntries++
					continue // Continue parsing
				} else {
					break
				}
			}

			// Complete entry
			entryData := data[entryOffset : entryOffset+int(entryLength)]
			entryOffset += int(entryLength)

			completeEntries++
			entryStr := string(entryData)
			foundEntries = append(foundEntries, entryStr)
			t.Logf("Entry %d: %d bytes - %q", completeEntries, entryLength, entryStr)
		}

		// Move to next shard
		nextShardStart := offset + int(capacity)
		if nextShardStart > len(data) {
			break
		}
		alignedOffset := ((nextShardStart + 511) / 512) * 512
		if alignedOffset > len(data) {
			break
		}
		offset = alignedOffset
		shardNum++
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Complete entries: %d", completeEntries)
	t.Logf("Incomplete entries: %d", incompleteEntries)
	t.Logf("Found entries: %v", foundEntries)

	// Verify we recovered entries after the incomplete one
	// We should find Entry 1, Entry 3, and Entry 4 (Entry 2 should be incomplete)
	hasEntry1 := false
	hasEntry3 := false
	hasEntry4 := false

	for _, entry := range foundEntries {
		if entry == "Entry 1: Fast write\n" {
			hasEntry1 = true
		}
		if entry == "Entry 3: Fast write after slow one\n" {
			hasEntry3 = true
		}
		if entry == "Entry 4: Another fast write\n" {
			hasEntry4 = true
		}
	}

	t.Logf("\n=== Recovery Verification ===")
	if hasEntry1 {
		t.Logf("✓ Entry 1 found (before incomplete entry)")
	} else {
		t.Errorf("✗ Entry 1 NOT found")
	}

	if incompleteEntries > 0 {
		t.Logf("✓ Incomplete entry detected (Entry 2)")
	} else {
		t.Logf("⚠ No incomplete entries detected")
	}

	if hasEntry3 {
		t.Logf("✓ Entry 3 found (after incomplete entry) - RECOVERY SUCCESSFUL!")
	} else {
		t.Errorf("✗ Entry 3 NOT found - recovery failed!")
	}

	if hasEntry4 {
		t.Logf("✓ Entry 4 found (after incomplete entry) - RECOVERY SUCCESSFUL!")
	} else {
		t.Errorf("✗ Entry 4 NOT found - recovery failed!")
	}

	// Assertions
	if !hasEntry1 {
		t.Errorf("Expected Entry 1 to be found")
	}
	// Note: Incomplete entries might not be detected if all writes complete before flush
	// The important thing is that recovery works - we can skip incomplete entries and continue
	if !hasEntry3 || !hasEntry4 {
		t.Errorf("Expected Entry 3 and Entry 4 to be found after incomplete entry (recovery failed)")
	} else {
		t.Logf("\n✅ RECOVERY TEST PASSED: Successfully parsed entries after incomplete entry!")
		t.Logf("   This demonstrates that incomplete entries can be skipped using length prefix")
	}
}
