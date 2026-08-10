package filex

import (
	testx "github.com/lcylpzls/testx"
	"sync"
	"testing"
)

func TestShardIndex(t *testing.T) {
	first := shardIndex("a", "b")
	second := shardIndex("a", "b")
	testx.RequireEqual(t, first, second)

	if first == shardIndex("b", "a") {
		t.Fatal("不同键应大概率不同分片（此处仅验证确定性）")
	}
	if shardIndex("", "") < 0 || shardIndex("", "") >= lockShards {
		t.Fatal("分片索引越界")
	}
}

func TestStripedLocksConcurrent(t *testing.T) {
	var l stripedLocks
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				unlock := l.lock("bucket", "key")
				unlock()
				runlock := l.rlock("bucket", "key")
				runlock()
			}
		}()
	}
	wg.Wait()
}
