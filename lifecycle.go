package filex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lcylpzls/logx"
)

// lifecycleMeta 是桶生命周期配置。
type lifecycleMeta struct {
	ExpireDays  int `json:"expire_days,omitempty"`
	MaxVersions int `json:"max_versions,omitempty"`
}

// LifecycleOptions 是桶生命周期配置。
type LifecycleOptions struct {
	ExpireDays  int // 0 表示不过期
	MaxVersions int // 0 表示不限制（仅版本化桶生效）
}

// LifecycleReport 是生命周期清理报告。
type LifecycleReport struct {
	Scanned  int
	Expired  int
	Pruned   int
	Messages []string
}

// SweepReport 是孤儿巡检报告。
type SweepReport struct {
	Buckets     int
	RemovedData int
	RemovedTmp  int
	RemovedDirs int
}

// SetBucketLifecycle 设置桶生命周期配置。
func (s *Store) SetBucketLifecycle(ctx context.Context, bucket string, opts LifecycleOptions) (BucketInfo, error) {
	if err := validateBucketName(bucket); err != nil {
		return BucketInfo{}, err
	}
	if opts.ExpireDays < 0 || opts.MaxVersions < 0 {
		return BucketInfo{}, newCode(CodeInvalidArgument, "生命周期参数不能为负数")
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(bucket))
	if os.IsNotExist(err) {
		return BucketInfo{}, newCode(CodeBucketNotFound, "桶不存在")
	}
	if err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "读取桶元数据失败")
	}
	if opts.ExpireDays == 0 && opts.MaxVersions == 0 {
		meta.Lifecycle = nil
	} else {
		meta.Lifecycle = &lifecycleMeta{ExpireDays: opts.ExpireDays, MaxVersions: opts.MaxVersions}
	}
	meta.UpdatedAt = time.Now().UTC()
	if err := s.writeJSONAtomic(s.bucketMetaPath(bucket), meta); err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "写入桶元数据失败")
	}
	s.logInfo("设置桶生命周期",
		logx.String("bucket", bucket),
		logx.Int("expire_days", opts.ExpireDays),
		logx.Int("max_versions", opts.MaxVersions),
	)
	return bucketInfoFromMeta(*meta), nil
}

// RunLifecycle 执行过期删除与版本数收敛。
func (s *Store) RunLifecycle(ctx context.Context, bucket string) (LifecycleReport, error) {
	if err := validateBucketName(bucket); err != nil {
		return LifecycleReport{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(bucket))
	if os.IsNotExist(err) {
		return LifecycleReport{}, newCode(CodeBucketNotFound, "桶不存在")
	}
	if err != nil {
		return LifecycleReport{}, wrapCode(err, CodeStorageFailed, "读取桶元数据失败")
	}
	if meta.Lifecycle == nil {
		return LifecycleReport{}, nil
	}
	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		return LifecycleReport{}, err
	}
	metas, err := s.collectAllMetas(bucket)
	if err != nil {
		return LifecycleReport{}, err
	}
	report := LifecycleReport{}
	byKey := map[string][]objectMeta{}
	for _, m := range metas {
		report.Scanned++
		if meta.Lifecycle.ExpireDays > 0 && time.Since(m.UpdatedAt) > time.Duration(meta.Lifecycle.ExpireDays)*24*time.Hour {
			if err := s.removeObjectFiles(bucket, m, versioning); err != nil {
				report.Messages = append(report.Messages, "过期清理失败: "+m.Key+": "+err.Error())
				continue
			}
			report.Expired++
			continue
		}
		if versioning {
			byKey[m.Key] = append(byKey[m.Key], m)
		}
	}
	if meta.Lifecycle.MaxVersions > 0 && versioning {
		for key, versions := range byKey {
			sortMetasNewestFirst(versions)
			for i := meta.Lifecycle.MaxVersions; i < len(versions); i++ {
				if err := s.removeVersionFiles(bucket, key, versions[i].VersionID); err != nil {
					report.Messages = append(report.Messages, "版本收敛失败: "+key+": "+err.Error())
					continue
				}
				report.Pruned++
			}
		}
	}
	s.logInfo("执行生命周期清理",
		logx.String("bucket", bucket),
		logx.Int("scanned", report.Scanned),
		logx.Int("expired", report.Expired),
		logx.Int("pruned", report.Pruned),
	)
	return report, nil
}

// removeObjectFiles 删除对象文件（版本化走版本文件，非版本化走扁平文件）。
func (s *Store) removeObjectFiles(bucket string, m objectMeta, versioning bool) error {
	if versioning {
		return s.removeVersionFiles(bucket, m.Key, m.VersionID)
	}
	if err := s.fs.Remove(s.objectMetaPath(bucket, m.Key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.fs.Remove(s.objectDataPath(bucket, m.Key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SweepOrphans 巡检全部桶并清理孤儿数据与临时文件。
func (s *Store) SweepOrphans(ctx context.Context) (SweepReport, error) {
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	report := SweepReport{}
	entries, err := s.fs.ReadDir(s.bucketsDir)
	if err != nil {
		return report, wrapCode(err, CodeStorageFailed, "扫描桶目录失败")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bucket := e.Name()
		report.Buckets++
		objectsDir := s.objectsDir(bucket)
		objEntries, err := s.fs.ReadDir(objectsDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			continue
		}
		for _, oe := range objEntries {
			name := oe.Name()
			if name == ".uploads" {
				// 分片上传会话目录由上传流程管理，孤儿巡检不介入。
				continue
			}
			if strings.HasSuffix(name, ".data") {
				metaName := strings.TrimSuffix(name, ".data") + ".json"
				if !fileExists(s.fs, filepath.Join(objectsDir, metaName)) {
					_ = s.fs.Remove(filepath.Join(objectsDir, name))
					report.RemovedData++
				}
				continue
			}
			if strings.HasPrefix(name, ".tmp-") {
				_ = s.fs.Remove(filepath.Join(objectsDir, name))
				report.RemovedTmp++
				continue
			}
			if oe.IsDir() {
				sub, err := s.fs.ReadDir(filepath.Join(objectsDir, name))
				if err != nil {
					continue
				}
				empty := true
				for _, f := range sub {
					fn := f.Name()
					if strings.HasPrefix(fn, "v-") && strings.HasSuffix(fn, ".data") {
						metaName := strings.TrimSuffix(fn, ".data") + ".json"
						if !fileExists(s.fs, filepath.Join(objectsDir, name, metaName)) {
							_ = s.fs.Remove(filepath.Join(objectsDir, name, fn))
							report.RemovedData++
						} else {
							empty = false
						}
						continue
					}
					if strings.HasPrefix(fn, "v-") && strings.HasSuffix(fn, ".json") {
						empty = false
					}
					if strings.HasPrefix(fn, ".tmp-") {
						_ = s.fs.Remove(filepath.Join(objectsDir, name, fn))
						report.RemovedTmp++
					}
				}
				if empty {
					_ = s.fs.RemoveAll(filepath.Join(objectsDir, name))
					report.RemovedDirs++
				}
			}
		}
	}
	s.logInfo("执行孤儿巡检",
		logx.Int("buckets", report.Buckets),
		logx.Int("removed_data", report.RemovedData),
		logx.Int("removed_tmp", report.RemovedTmp),
	)
	return report, nil
}

func fileExists(fs fsOps, path string) bool {
	_, err := fs.Stat(path)
	return err == nil
}
