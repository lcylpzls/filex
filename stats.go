package filex

import "context"

// BucketStats 是桶统计快照。
type BucketStats struct {
	ObjectCount  int
	VersionCount int
	Usage        int64
}

// BucketStats 返回桶的对象数、版本数与字节用量（均不含删除标记）。
func (s *Store) BucketStats(ctx context.Context, bucket string) (BucketStats, error) {
	if err := s.ensureOpen(); err != nil {
		return BucketStats{}, err
	}
	if err := validateBucketName(bucket); err != nil {
		return BucketStats{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return BucketStats{}, err
	}
	metas, err := s.collectAllMetas(bucket)
	if err != nil {
		return BucketStats{}, err
	}
	stats := BucketStats{}
	keys := map[string]struct{}{}
	for _, m := range metas {
		if m.Deleted {
			continue
		}
		keys[m.Key] = struct{}{}
		stats.VersionCount++
		stats.Usage += m.Size
	}
	stats.ObjectCount = len(keys)
	return stats, nil
}
