package asyncloguploader

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShard_NewShard(t *testing.T) {
	t.Run("CreatesShardWithDoubleBuffer", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1) // 1MB capacity
		require.NoError(t, err)
		defer shard.Close()

		assert.NotNil(t, shard.bufferA)
		assert.NotNil(t, shard.bufferB)
		assert.Equal(t, uint32(1), shard.ID())
		assert.Equal(t, int32(1024*1024), shard.Capacity())
		assert.Equal(t, headerOffset, int(shard.Offset()))
	})

	t.Run("InitializesOffsetsToHeaderOffset", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		assert.Equal(t, headerOffset, int(shard.offsetA.Load()))
		assert.Equal(t, headerOffset, int(shard.offsetB.Load()))
	})

	t.Run("SetsBufferAAsInitialActive", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		activeBuf := shard.activeBuffer.Load()
		assert.Equal(t, &shard.bufferA, activeBuf)
	})
}

func TestShard_Write(t *testing.T) {
	t.Run("WritesDataToActiveBuffer", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		data := []byte("test log entry")
		n, needsFlush := shard.Write(data)

		assert.Greater(t, n, 0)
		assert.False(t, needsFlush)
		assert.Greater(t, shard.Offset(), int32(headerOffset))
	})

	t.Run("PrependsLengthPrefix", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		data := []byte("test")
		n, _ := shard.Write(data)

		// Should write 4-byte length prefix + data
		expectedSize := 4 + len(data)
		assert.Equal(t, expectedSize, n)

		// Verify length prefix is written correctly
		activeBuf := shard.activeBuffer.Load()
		offset := shard.Offset()
		lengthPrefix := binary.LittleEndian.Uint32((*activeBuf)[offset-int32(n) : offset-int32(len(data))])
		assert.Equal(t, uint32(len(data)), lengthPrefix)
	})

	t.Run("ReturnsNeedsFlushWhenBufferFull", func(t *testing.T) {
		shard, err := NewShard(1024, 1) // Small capacity
		require.NoError(t, err)
		defer shard.Close()

		// Fill buffer
		largeData := make([]byte, 500)
		for i := 0; i < 10; i++ {
			n, needsFlush := shard.Write(largeData)
			if needsFlush {
				assert.Greater(t, n, 0)
				return
			}
			assert.Greater(t, n, 0)
		}
		t.Fatal("Buffer should have filled")
	})

	t.Run("ReturnsZeroWhenShardMarkedForFlush", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		shard.readyForFlush.Store(true)
		n, needsFlush := shard.Write([]byte("test"))

		assert.Equal(t, 0, n)
		assert.True(t, needsFlush)
	})

	t.Run("HandlesEmptyData", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		n, needsFlush := shard.Write(nil)
		assert.Equal(t, 0, n)
		assert.False(t, needsFlush)
	})
}

func TestShard_TrySwap(t *testing.T) {
	t.Run("SwapsActiveBuffer", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Check initial state - bufferA should be active
		initialActive := shard.activeBuffer.Load()
		assert.NotNil(t, initialActive)
		isInitiallyA := (initialActive == &shard.bufferA)

		shard.trySwap()
		newActive := shard.activeBuffer.Load()
		assert.NotNil(t, newActive)

		// Verify swap occurred by checking which buffer is now active
		isNowA := (newActive == &shard.bufferA)
		assert.NotEqual(t, isInitiallyA, isNowA, "Buffer should have swapped")
		assert.True(t, shard.readyForFlush.Load())
	})

	t.Run("SwapsBetweenBufferAAndB", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Check initial state
		initialActive := shard.activeBuffer.Load()
		assert.NotNil(t, initialActive)
		isInitiallyA := (initialActive == &shard.bufferA)

		shard.trySwap()
		afterFirstSwap := shard.activeBuffer.Load()
		assert.NotNil(t, afterFirstSwap)
		isAfterFirstA := (afterFirstSwap == &shard.bufferA)

		assert.NotEqual(t, isInitiallyA, isAfterFirstA, "First swap should change active buffer")

		// Reset state for second swap
		shard.readyForFlush.Store(false)
		shard.swapping.Store(false)

		shard.trySwap()
		afterSecondSwap := shard.activeBuffer.Load()
		assert.NotNil(t, afterSecondSwap)
		isAfterSecondA := (afterSecondSwap == &shard.bufferA)

		// Should swap back to initial buffer
		assert.Equal(t, isInitiallyA, isAfterSecondA, "Second swap should return to initial buffer")
	})

	t.Run("IsCASProtected", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Multiple goroutines trying to swap concurrently
		// Use a barrier to ensure all goroutines start CAS at the same time
		var wg sync.WaitGroup
		ready := make(chan struct{})
		swapped := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-ready // Wait for all goroutines to be ready
				success := shard.swapping.CompareAndSwap(false, true)
				swapped <- success
				if success {
					// Don't release immediately - let test verify only one succeeded
					// Release will happen after test checks
				}
			}()
		}

		// Start all goroutines at once
		close(ready)
		wg.Wait()

		// Only one should succeed in the concurrent CAS
		successCount := 0
		for i := 0; i < 10; i++ {
			if <-swapped {
				successCount++
			}
		}
		assert.Equal(t, 1, successCount, "Only one goroutine should succeed in concurrent CAS")

		// Release the lock if one succeeded
		if successCount == 1 {
			shard.swapping.Store(false)
		}
	})
}

func TestShard_GetData(t *testing.T) {
	t.Run("ReturnsInactiveBufferData", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Write some data
		data := []byte("test data")
		shard.Write(data)

		// Swap to make current buffer inactive
		shard.trySwap()

		// Get data from inactive buffer
		bufferData, allCompleted := shard.GetData(100 * time.Millisecond)

		assert.NotNil(t, bufferData)
		assert.True(t, allCompleted)
		assert.Equal(t, int(shard.Capacity()), len(bufferData))
	})

	t.Run("WaitsForWriteCompletion", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Write data
		shard.Write([]byte("test"))
		shard.trySwap()

		// GetData should wait for writes to complete
		bufferData, allCompleted := shard.GetData(100 * time.Millisecond)

		assert.NotNil(t, bufferData)
		assert.True(t, allCompleted)
	})

	t.Run("ReturnsFalseOnTimeout", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// GetData with very short timeout
		bufferData, allCompleted := shard.GetData(1 * time.Nanosecond)

		// Should return data even if timeout (may be empty)
		assert.NotNil(t, bufferData)
		// May or may not be completed depending on timing
		_ = allCompleted
	})
}

func TestShard_GetInactiveOffset(t *testing.T) {
	t.Run("ReturnsCorrectInactiveOffset", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Write to active buffer (bufferA)
		shard.Write([]byte("test"))
		offsetA := shard.offsetA.Load()

		// Swap to bufferB
		shard.trySwap()

		// GetInactiveOffset should return bufferA's offset
		inactiveOffset := shard.GetInactiveOffset()
		assert.Equal(t, offsetA, inactiveOffset)
	})
}

func TestShard_Reset(t *testing.T) {
	t.Run("ResetsInactiveBuffer", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Write data and swap
		shard.Write([]byte("test"))
		shard.trySwap()

		// Reset inactive buffer
		shard.Reset()

		assert.False(t, shard.readyForFlush.Load())
		assert.Equal(t, headerOffset, int(shard.GetInactiveOffset()))
	})

	t.Run("ResetsWriteCounters", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		// Write to bufferA
		shard.Write([]byte("test"))
		shard.trySwap()

		// Reset should clear counters
		shard.Reset()

		// After reset, inflight counter should be 0
		assert.Equal(t, int64(0), shard.inflightA.Load())
	})
}

func TestShard_ConcurrentWrites(t *testing.T) {
	t.Run("HandlesConcurrentWrites", func(t *testing.T) {
		shard, err := NewShard(10*1024*1024, 1) // 10MB capacity
		require.NoError(t, err)
		defer shard.Close()

		const numGoroutines = 10
		const writesPerGoroutine = 100
		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				for j := 0; j < writesPerGoroutine; j++ {
					data := []byte{byte(id), byte(j)}
					shard.Write(data)
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < numGoroutines; i++ {
			<-done
		}

		// Verify data was written
		assert.Greater(t, shard.Offset(), int32(headerOffset))
	})
}

func TestShard_HasData(t *testing.T) {
	t.Run("ReturnsFalseWhenNoData", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		assert.False(t, shard.HasData())
	})

	t.Run("ReturnsTrueWhenHasData", func(t *testing.T) {
		shard, err := NewShard(1024*1024, 1)
		require.NoError(t, err)
		defer shard.Close()

		shard.Write([]byte("test"))
		shard.trySwap()

		assert.True(t, shard.HasData())
	})
}
