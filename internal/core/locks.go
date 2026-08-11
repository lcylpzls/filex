package core

import "sync"

// lockShards 是分片读写锁数量。
const lockShards = 64

// stripedLocks 提供按 bucket+key 分片的读写锁，减少同桶不同键的互斥。
type stripedLocks struct {
	shards [lockShards]sync.RWMutex
}

func (l *stripedLocks) lock(bucket, key string) func() {
	shard := shardIndex(bucket, key)
	l.shards[shard].Lock()
	return l.shards[shard].Unlock
}

func (l *stripedLocks) rlock(bucket, key string) func() {
	shard := shardIndex(bucket, key)
	l.shards[shard].RLock()
	return l.shards[shard].RUnlock
}

// shardIndex 使用 FNV-1a 把 bucket+key 映射到分片。
func shardIndex(bucket, key string) int {
	h := uint32(2166136261)
	for i := 0; i < len(bucket); i++ {
		h ^= uint32(bucket[i])
		h *= 16777619
	}
	h ^= uint32(0xff) // 分隔
	h *= 16777619
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % lockShards)
}
