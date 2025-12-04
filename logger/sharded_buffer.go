package logger

import (
	"sync"
	"sync/atomic"
)

// Shard represents a single shard in the sharded buffer
type Shard struct {
	mu       sync.Mutex
	data     []byte
	offset   int32
	capacity int32
	id       uint32
	readyForFlush atomic.Bool
}

// ShardedBuffer manages multiple shards for distributed writes
type ShardedBuffer struct {
	shards    []*Shard
	numShards int
	counter   atomic.Uint64 // For round-robin shard selection
	totalCapacity int32
}

// NewShard creates a new shard with the given capacity
func NewShard(capacity int, id uint32) *Shard {
	return &Shard{
		data:     make([]byte, capacity),
		offset:   0,
		capacity: int32(capacity),
		id:       id,
	}
}

// NewShardedBuffer creates a new sharded buffer
func NewShardedBuffer(totalCapacity int, numShards int) *ShardedBuffer {
	if numShards <= 0 {
		numShards = 16 // Default
	}
	
	shardCapacity := totalCapacity / numShards
	if shardCapacity < 64*1024 {
		// Ensure minimum 64KB per shard
		shardCapacity = 64 * 1024
		numShards = totalCapacity / shardCapacity
		if numShards == 0 {
			numShards = 1
		}
	}
	
	shards := make([]*Shard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewShard(shardCapacity, uint32(i))
	}
	
	return &ShardedBuffer{
		shards:        shards,
		numShards:     numShards,
		totalCapacity: int32(totalCapacity),
	}
}

// Write writes data to a shard using round-robin selection
func (sb *ShardedBuffer) Write(p []byte) (n int, needsFlush bool, shardID int) {
	if len(p) == 0 {
		return 0, false, -1
	}
	
	// Round-robin shard selection
	shardIdx := int(sb.counter.Add(1) % uint64(sb.numShards))
	shard := sb.shards[shardIdx]
	
	n, needsFlush = shard.Write(p)
	return n, needsFlush, shardIdx
}

// GetShard returns a specific shard by index
func (sb *ShardedBuffer) GetShard(idx int) *Shard {
	if idx < 0 || idx >= sb.numShards {
		return nil
	}
	return sb.shards[idx]
}

// NumShards returns the number of shards
func (sb *ShardedBuffer) NumShards() int {
	return sb.numShards
}

// Shards returns all shards (for iteration)
func (sb *ShardedBuffer) Shards() []*Shard {
	return sb.shards
}

// Write writes data to the shard (mutex-protected)
func (s *Shard) Write(p []byte) (n int, needsFlush bool) {
	if len(p) == 0 {
		return 0, false
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Check if already marked for flush
	if s.readyForFlush.Load() {
		return 0, true
	}
	
	// Check if we have space
	newOffset := s.offset + int32(len(p))
	if newOffset > s.capacity {
		s.readyForFlush.Store(true)
		return 0, true
	}
	
	// Write data
	copy(s.data[s.offset:newOffset], p)
	s.offset = newOffset
	
	// Check if now full
	if newOffset >= s.capacity {
		s.readyForFlush.Store(true)
		return len(p), true
	}
	
	return len(p), false
}

// Bytes returns the current data in the shard
func (s *Shard) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.offset == 0 {
		return nil
	}
	return s.data[:s.offset]
}

// Reset clears the shard for reuse
func (s *Shard) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.offset = 0
	s.readyForFlush.Store(false)
}

// Offset returns the current offset
func (s *Shard) Offset() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offset
}

// IsFull returns whether the shard is ready for flushing
func (s *Shard) IsFull() bool {
	return s.readyForFlush.Load()
}

// ID returns the shard identifier
func (s *Shard) ID() uint32 {
	return s.id
}

// SetID updates the shard identifier
func (s *Shard) SetID(id uint32) {
	s.id = id
}

// Capacity returns the shard capacity
func (s *Shard) Capacity() int32 {
	return s.capacity
}

