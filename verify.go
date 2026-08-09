package filex

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// IntegrityReport 是全量完整性审计报告。
type IntegrityReport struct {
	Scanned int
	Corrupt int
	Errors  []string
}

// VerifyObject 校验单个对象内容哈希。
func (s *Store) VerifyObject(ctx context.Context, bucket, key string) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return err
	}
	return s.verifyOne(ctx, bucket, key, "")
}

// VerifyAll 并发审计全部桶的当前对象与全部版本（跳过删除标记）。
func (s *Store) VerifyAll(ctx context.Context, concurrency int) (IntegrityReport, error) {
	if err := s.ensureOpen(); err != nil {
		return IntegrityReport{}, err
	}
	report := IntegrityReport{}
	if concurrency <= 0 {
		concurrency = 4
	}
	if concurrency > 32 {
		concurrency = 32
	}
	buckets, err := s.ListBuckets(ctx)
	if err != nil {
		return report, err
	}
	type job struct {
		bucket    string
		key       string
		versionID string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := s.verifyOne(ctx, j.bucket, j.key, j.versionID); err != nil {
					mu.Lock()
					report.Corrupt++
					report.Errors = append(report.Errors,
						fmt.Sprintf("%s/%s(%s): %v", j.bucket, j.key, j.versionID, err))
					mu.Unlock()
				}
			}
		}()
	}
	for _, b := range buckets {
		metas, err := s.collectAllMetas(b.Name)
		if err != nil {
			report.Errors = append(report.Errors, "枚举桶失败: "+b.Name+": "+err.Error())
			continue
		}
		for _, m := range metas {
			if m.Deleted {
				continue
			}
			report.Scanned++
			jobs <- job{bucket: b.Name, key: m.Key, versionID: m.VersionID}
		}
	}
	close(jobs)
	wg.Wait()
	return report, nil
}

func (s *Store) verifyOne(ctx context.Context, bucket, key, versionID string) error {
	var obj *Object
	var err error
	if versionID == "" {
		obj, err = s.Get(ctx, bucket, key, GetOptions{Verify: true})
	} else {
		obj, err = s.GetVersion(ctx, bucket, key, versionID, GetOptions{Verify: true})
	}
	if err != nil {
		return err
	}
	defer obj.Close()
	_, err = io.Copy(io.Discard, obj)
	return err
}
