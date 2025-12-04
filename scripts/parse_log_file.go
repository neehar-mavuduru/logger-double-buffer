package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <log_file>\n", os.Args[0])
		os.Exit(1)
	}

	filePath := os.Args[1]
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Log File Analysis ===\n")
	fmt.Printf("File: %s\n", filePath)
	fmt.Printf("Total size: %d bytes\n\n", len(data))

	offset := 0
	shardNum := 0
	completeEntries := 0
	incompleteEntries := 0

	for offset < len(data) {
		if offset+8 > len(data) {
			fmt.Printf("WARNING: Incomplete shard header at offset %d\n", offset)
			break
		}

		capacity := binary.LittleEndian.Uint32(data[offset : offset+4])
		validDataBytes := binary.LittleEndian.Uint32(data[offset+4 : offset+8])

		fmt.Printf("--- Shard %d ---\n", shardNum)
		fmt.Printf("  Offset: %d\n", offset)
		fmt.Printf("  Capacity: %d bytes\n", capacity)
		fmt.Printf("  Valid data: %d bytes\n", validDataBytes)

		// Safety check
		remainingBytes := len(data) - offset - 8
		if int(validDataBytes) > remainingBytes {
			fmt.Printf("  ⚠ WARNING: validDataBytes (%d) exceeds remaining file size (%d)\n", validDataBytes, remainingBytes)
			fmt.Printf("  → This indicates corrupted header or timeout scenario\n")
			validDataBytes = uint32(remainingBytes)
		}

		offset += 8
		shardEnd := offset + int(validDataBytes)
		if shardEnd > len(data) {
			shardEnd = len(data)
		}
		entryOffset := offset

		for entryOffset < shardEnd {
			if entryOffset+4 > shardEnd {
				fmt.Printf("  ⚠ INCOMPLETE: Length prefix at offset %d\n", entryOffset)
				incompleteEntries++
				break
			}

			entryLength := binary.LittleEndian.Uint32(data[entryOffset : entryOffset+4])
			entryOffset += 4

			// Validate entry length (sanity check)
			if entryLength == 0 || entryLength > 10*1024*1024 { // Max 10MB per entry
				fmt.Printf("  ⚠ INVALID ENTRY LENGTH at offset %d: %d bytes (likely corrupted length prefix)\n", entryOffset-4, entryLength)
				fmt.Printf("    → Cannot recover: corrupted length prefix means we don't know where next entry starts\n")
				incompleteEntries++
				break
			}

			if entryOffset+int(entryLength) > shardEnd {
				fmt.Printf("  ⚠ INCOMPLETE ENTRY:\n")
				fmt.Printf("    Offset: %d\n", entryOffset-4)
				fmt.Printf("    Expected length: %d bytes\n", entryLength)
				fmt.Printf("    Available: %d bytes\n", shardEnd-entryOffset)
				fmt.Printf("    Missing: %d bytes\n", int(entryLength)-(shardEnd-entryOffset))

				// Show partial data
				if entryOffset < shardEnd {
					partial := data[entryOffset:shardEnd]
					preview := string(partial)
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					fmt.Printf("    Partial data: %q\n", preview)

					// Show hex dump of first 64 bytes
					fmt.Printf("    Hex dump (first 64 bytes):\n")
					hexLen := len(partial)
					if hexLen > 64 {
						hexLen = 64
					}
					for i := 0; i < hexLen; i += 16 {
						end := i + 16
						if end > hexLen {
							end = hexLen
						}
						fmt.Printf("      %04x: ", i)
						for j := i; j < end; j++ {
							fmt.Printf("%02x ", partial[j])
						}
						fmt.Printf("\n")
					}
				}

				// RECOVERY: Skip to next entry using length prefix
				nextEntryOffset := entryOffset - 4 + 4 + int(entryLength)
				fmt.Printf("    → Skipping to next entry at offset %d (using length prefix for recovery)\n", nextEntryOffset)

				incompleteEntries++

				// Check if we can continue to next entry
				if nextEntryOffset <= shardEnd {
					entryOffset = nextEntryOffset
					continue // Continue parsing next entry
				} else {
					// Next entry would be beyond shard boundary
					break
				}
			}

			entryData := data[entryOffset : entryOffset+int(entryLength)]
			entryOffset += int(entryLength)
			completeEntries++

			if completeEntries <= 5 {
				preview := string(entryData)
				if len(preview) > 80 {
					preview = preview[:80] + "..."
				}
				fmt.Printf("  Entry %d: %d bytes - %q\n", completeEntries, entryLength, preview)
			}
		}

		// Move to next shard (512-byte aligned)
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

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Shards: %d\n", shardNum)
	fmt.Printf("Complete entries: %d\n", completeEntries)
	fmt.Printf("Incomplete entries: %d\n", incompleteEntries)

	if incompleteEntries > 0 {
		fmt.Printf("\n✓ TIMEOUT DETECTED: Found %d incomplete entries\n", incompleteEntries)
		fmt.Printf("  This indicates GetData() timed out while writes were in progress\n")
		fmt.Printf("  The incomplete entry represents data that was being written when flush occurred\n")
	} else {
		fmt.Printf("\n✓ No incomplete entries found - all writes completed successfully\n")
	}
}
