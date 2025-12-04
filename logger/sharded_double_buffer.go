package logger

import (
	"sync/atomic"
)

// ShardedBufferSet represents a set of shards that can be swapped atomically
type ShardedBufferSet struct {
	shards    []*Shard
	numShards int
	id        uint32
	counter   atomic.Uint64 // For round-robin shard selection
}

// NewShardedBufferSet creates a new set of shards
func NewShardedBufferSet(totalCapacity int, numShards int, setID uint32) *ShardedBufferSet {
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

	return &ShardedBufferSet{
		shards:    shards,
		numShards: numShards,
		id:        setID,
	}
}

// Write writes data to a shard using round-robin selection
func (sbs *ShardedBufferSet) Write(p []byte) (n int, needsFlush bool, shardID int) {
	if len(p) == 0 {
		return 0, false, -1
	}

	// Round-robin shard selection
	shardIdx := int(sbs.counter.Add(1) % uint64(sbs.numShards))
	shard := sbs.shards[shardIdx]

	n, needsFlush = shard.Write(p)
	return n, needsFlush, shardIdx
}

// GetShard returns a specific shard by index
func (sbs *ShardedBufferSet) GetShard(idx int) *Shard {
	if idx < 0 || idx >= sbs.numShards {
		return nil
	}
	return sbs.shards[idx]
}

// NumShards returns the number of shards
func (sbs *ShardedBufferSet) NumShards() int {
	return sbs.numShards
}

// Shards returns all shards for iteration
func (sbs *ShardedBufferSet) Shards() []*Shard {
	return sbs.shards
}

// ID returns the set identifier
func (sbs *ShardedBufferSet) ID() uint32 {
	return sbs.id
}

// SetID updates the set identifier
func (sbs *ShardedBufferSet) SetID(id uint32) {
	sbs.id = id
}

// HasData returns true if any shard has data
func (sbs *ShardedBufferSet) HasData() bool {
	for _, shard := range sbs.shards {
		if shard.Offset() > 0 {
			return true
		}
	}
	return false
}

// AnyShardFull returns true if any shard is marked for flush
func (sbs *ShardedBufferSet) AnyShardFull() bool {
	for _, shard := range sbs.shards {
		if shard.IsFull() {
			return true
		}
	}
	return false
}

// ResetAll resets all shards in the set
func (sbs *ShardedBufferSet) ResetAll() {
	for _, shard := range sbs.shards {
		shard.Reset()
	}
}

// ShardedCASBufferSet represents a set of CAS shards that can be swapped atomically
type ShardedCASBufferSet struct {
	shards    []*CASShard
	numShards int
	id        uint32
	counter   atomic.Uint64 // For round-robin shard selection
}

// NewShardedCASBufferSet creates a new set of CAS shards
func NewShardedCASBufferSet(totalCapacity int, numShards int, setID uint32) *ShardedCASBufferSet {
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

	shards := make([]*CASShard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewCASShard(shardCapacity, uint32(i))
	}

	return &ShardedCASBufferSet{
		shards:    shards,
		numShards: numShards,
		id:        setID,
	}
}

// Write writes data to a shard using round-robin selection
func (scbs *ShardedCASBufferSet) Write(p []byte) (n int, needsFlush bool, shardID int) {
	if len(p) == 0 {
		return 0, false, -1
	}

	// Round-robin shard selection
	shardIdx := int(scbs.counter.Add(1) % uint64(scbs.numShards))
	shard := scbs.shards[shardIdx]

	n, needsFlush = shard.Write(p)
	return n, needsFlush, shardIdx
}

// GetShard returns a specific shard by index
func (scbs *ShardedCASBufferSet) GetShard(idx int) *CASShard {
	if idx < 0 || idx >= scbs.numShards {
		return nil
	}
	return scbs.shards[idx]
}

// NumShards returns the number of shards
func (scbs *ShardedCASBufferSet) NumShards() int {
	return scbs.numShards
}

// Shards returns all shards for iteration
func (scbs *ShardedCASBufferSet) Shards() []*CASShard {
	return scbs.shards
}

// ID returns the set identifier
func (scbs *ShardedCASBufferSet) ID() uint32 {
	return scbs.id
}

// SetID updates the set identifier
func (scbs *ShardedCASBufferSet) SetID(id uint32) {
	scbs.id = id
}

// HasData returns true if any shard has data
func (scbs *ShardedCASBufferSet) HasData() bool {
	for _, shard := range scbs.shards {
		if shard.Offset() > 0 {
			return true
		}
	}
	return false
}

// AnyShardFull returns true if any shard is marked for flush
func (scbs *ShardedCASBufferSet) AnyShardFull() bool {
	for _, shard := range scbs.shards {
		if shard.IsFull() {
			return true
		}
	}
	return false
}

// ResetAll resets all shards in the set
func (scbs *ShardedCASBufferSet) ResetAll() {
	for _, shard := range scbs.shards {
		shard.Reset()
	}
}
