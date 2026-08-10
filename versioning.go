package filex

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lcylpzls/cryptox"
	"github.com/lcylpzls/logx"
)

// versionRand 可注入，便于测试随机数失败分支。
var versionRand = cryptox.RandomBytes

// emptySHA256 是空内容的 SHA256，用于删除标记。
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func newVersionID() string {
	b, err := versionRand(16)
	if err != nil {
		return fmt.Sprintf("v-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s *Store) bucketVersioning(bucket string) (bool, error) {
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(bucket))
	if err != nil {
		return false, err
	}
	return meta.Versioning, nil
}

func (s *Store) versionDir(bucket, key string) string {
	return filepath.Join(s.objectsDir(bucket), hashKey(key))
}

func (s *Store) versionDataPath(bucket, key, versionID string) string {
	return filepath.Join(s.versionDir(bucket, key), "v-"+versionID+".data")
}

func (s *Store) versionMetaPath(bucket, key, versionID string) string {
	return filepath.Join(s.versionDir(bucket, key), "v-"+versionID+".json")
}

func (s *Store) objectPaths(bucket, key, versionID string) (string, string) {
	if versionID == "" {
		return s.objectDataPath(bucket, key), s.objectMetaPath(bucket, key)
	}
	return s.versionDataPath(bucket, key, versionID), s.versionMetaPath(bucket, key, versionID)
}

// readVersionMetas 读取某个键的全部版本元数据（不排序）。
func (s *Store) readVersionMetas(bucket, key string) ([]objectMeta, error) {
	entries, err := s.fs.ReadDir(s.versionDir(bucket, key))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	metas := make([]objectMeta, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "v-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		meta, err := readObjectMeta(s.fs, filepath.Join(s.versionDir(bucket, key), name))
		if err != nil {
			continue
		}
		metas = append(metas, *meta)
	}
	return metas, nil
}

// readCurrentMeta 读取当前可见元数据；版本化桶无版本时回退扁平对象，
// 兼容「先建对象再开版本化」的历史数据。
func (s *Store) readCurrentMeta(bucket, key string, versioning bool) (*objectMeta, error) {
	if !versioning {
		return readObjectMeta(s.fs, s.objectMetaPath(bucket, key))
	}
	metas, err := s.readVersionMetas(bucket, key)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return readObjectMeta(s.fs, s.objectMetaPath(bucket, key))
	}
	sortMetasNewestFirst(metas)
	if metas[0].Deleted {
		return nil, os.ErrNotExist
	}
	return &metas[0], nil
}

func sortMetasNewestFirst(metas []objectMeta) {
	sort.Slice(metas, func(i, j int) bool {
		if !metas[i].UpdatedAt.Equal(metas[j].UpdatedAt) {
			return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
		}
		return metas[i].VersionID > metas[j].VersionID
	})
}

// collectCurrentMetas 汇总桶内当前可见对象（版本化桶取每键最新未删除版本）。
func (s *Store) collectCurrentMetas(bucket string) ([]objectMeta, error) {
	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		return nil, err
	}
	entries, err := s.fs.ReadDir(s.objectsDir(bucket))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !versioning {
		metas := make([]objectMeta, 0, len(entries))
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			meta, err := readObjectMeta(s.fs, filepath.Join(s.objectsDir(bucket), e.Name()))
			if err != nil {
				continue
			}
			metas = append(metas, *meta)
		}
		return metas, nil
	}
	group := map[string]objectMeta{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := s.fs.ReadDir(filepath.Join(s.objectsDir(bucket), e.Name()))
		if err != nil {
			continue
		}
		metas := make([]objectMeta, 0, len(sub))
		for _, f := range sub {
			if !strings.HasPrefix(f.Name(), "v-") || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			meta, err := readObjectMeta(s.fs, filepath.Join(s.objectsDir(bucket), e.Name(), f.Name()))
			if err != nil {
				continue
			}
			metas = append(metas, *meta)
		}
		sortMetasNewestFirst(metas)
		if len(metas) == 0 || metas[0].Deleted {
			continue
		}
		if cur, ok := group[metas[0].Key]; !ok || metas[0].UpdatedAt.After(cur.UpdatedAt) {
			group[metas[0].Key] = metas[0]
		}
	}
	// 兼容历史扁平对象：无版本化版本时纳入当前视图
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		meta, err := readObjectMeta(s.fs, filepath.Join(s.objectsDir(bucket), e.Name()))
		if err != nil {
			continue
		}
		if _, ok := group[meta.Key]; ok {
			continue
		}
		group[meta.Key] = *meta
	}
	metas := make([]objectMeta, 0, len(group))
	for _, m := range group {
		metas = append(metas, m)
	}
	return metas, nil
}

// collectAllMetas 汇总桶内全部版本元数据（含删除标记），用于配额统计。
func (s *Store) collectAllMetas(bucket string) ([]objectMeta, error) {
	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		return nil, err
	}
	entries, err := s.fs.ReadDir(s.objectsDir(bucket))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metas []objectMeta
	if !versioning {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			meta, err := readObjectMeta(s.fs, filepath.Join(s.objectsDir(bucket), e.Name()))
			if err != nil {
				continue
			}
			metas = append(metas, *meta)
		}
		return metas, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := s.fs.ReadDir(filepath.Join(s.objectsDir(bucket), e.Name()))
		if err != nil {
			continue
		}
		for _, f := range sub {
			if !strings.HasPrefix(f.Name(), "v-") || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			meta, err := readObjectMeta(s.fs, filepath.Join(s.objectsDir(bucket), e.Name(), f.Name()))
			if err != nil {
				continue
			}
			metas = append(metas, *meta)
		}
	}
	return metas, nil
}

// bucketUsage 统计桶内非删除对象的字节总量。
func (s *Store) bucketUsage(bucket string) (int64, error) {
	metas, err := s.collectAllMetas(bucket)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, m := range metas {
		if !m.Deleted {
			total += m.Size
		}
	}
	return total, nil
}

// checkQuota 检查新增字节后是否超配额；超限返回 filex_quota_exceeded。
func (s *Store) checkQuota(bucket string) error {
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(bucket))
	if err != nil {
		return nil
	}
	if meta.Quota <= 0 {
		return nil
	}
	usage, err := s.bucketUsage(bucket)
	if err != nil {
		return nil
	}
	if usage > meta.Quota {
		return newCode(CodeQuotaExceeded, "桶配额超限")
	}
	return nil
}

// removeVersionFiles 永久删除指定版本的数据与元数据。
func (s *Store) removeVersionFiles(bucket, key, versionID string) error {
	metaPath := s.versionMetaPath(bucket, key, versionID)
	dataPath := s.versionDataPath(bucket, key, versionID)
	if err := s.fs.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.fs.Remove(dataPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	// 目录为空则回收
	entries, err := s.fs.ReadDir(s.versionDir(bucket, key))
	if err == nil && len(entries) == 0 {
		_ = s.fs.RemoveAll(s.versionDir(bucket, key))
	}
	return nil
}

// GetVersion 读取指定版本（删除标记返回 filex_object_not_found）。
func (s *Store) GetVersion(ctx context.Context, bucket, key, versionID string, opts GetOptions) (*Object, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	if err := validateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return nil, err
	}
	if versionID == "" {
		return nil, newCode(CodeInvalidArgument, "版本 ID 不能为空")
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return nil, err
	}
	unlock := s.locks.rlock(bucket, key)
	defer unlock()
	meta, err := readObjectMeta(s.fs, s.versionMetaPath(bucket, key, versionID))
	if os.IsNotExist(err) {
		return nil, newCode(CodeVersionNotFound, "版本不存在")
	}
	if err != nil {
		return nil, err
	}
	if meta.Deleted {
		return nil, newCode(CodeObjectNotFound, "删除标记不可读取")
	}
	return s.openObject(bucket, key, meta, s.versionDataPath(bucket, key, versionID), opts)
}

// HeadVersion 查询指定版本元数据。
func (s *Store) HeadVersion(ctx context.Context, bucket, key, versionID string) (ObjectInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateBucketName(bucket); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return ObjectInfo{}, err
	}
	if versionID == "" {
		return ObjectInfo{}, newCode(CodeInvalidArgument, "版本 ID 不能为空")
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return ObjectInfo{}, err
	}
	unlock := s.locks.rlock(bucket, key)
	defer unlock()
	meta, err := readObjectMeta(s.fs, s.versionMetaPath(bucket, key, versionID))
	if os.IsNotExist(err) {
		return ObjectInfo{}, newCode(CodeVersionNotFound, "版本不存在")
	}
	if err != nil {
		return ObjectInfo{}, err
	}
	if meta.Deleted {
		return ObjectInfo{}, newCode(CodeObjectNotFound, "删除标记不可查询")
	}
	return objectInfoFromMeta(bucket, *meta), nil
}

// DeleteVersion 永久删除指定版本（含删除标记）。
func (s *Store) DeleteVersion(ctx context.Context, bucket, key, versionID string) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return err
	}
	if versionID == "" {
		return newCode(CodeInvalidArgument, "版本 ID 不能为空")
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return err
	}
	unlock := s.locks.lock(bucket, key)
	defer unlock()
	if _, err := readObjectMeta(s.fs, s.versionMetaPath(bucket, key, versionID)); os.IsNotExist(err) {
		return newCode(CodeVersionNotFound, "版本不存在")
	} else if err != nil {
		return err
	}
	if err := s.removeVersionFiles(bucket, key, versionID); err != nil {
		return wrapCode(err, CodeStorageFailed, "删除版本失败")
	}
	s.metrics.Add(bucket, "delete_version", 0)
	s.logInfo("永久删除版本",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.String("version_id", versionID),
	)
	return nil
}

// RestoreVersion 将历史版本复制为新的当前版本。
func (s *Store) RestoreVersion(ctx context.Context, bucket, key, versionID string) (ObjectInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	obj, err := s.GetVersion(ctx, bucket, key, versionID, GetOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	defer obj.Close()
	return s.Put(ctx, bucket, key, obj, PutOptions{
		ContentType: obj.Info.ContentType,
		Metadata:    obj.Info.Metadata,
	})
}

// ListVersions 枚举指定键的全部版本（新→旧），含删除标记。
func (s *Store) ListVersions(ctx context.Context, bucket, key string) ([]ObjectInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	if err := validateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return nil, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return nil, err
	}
	unlock := s.locks.rlock(bucket, key)
	defer unlock()
	metas, err := s.readVersionMetas(bucket, key)
	if err != nil {
		return nil, err
	}
	sortMetasNewestFirst(metas)
	out := make([]ObjectInfo, 0, len(metas))
	for _, m := range metas {
		out = append(out, objectInfoFromMeta(bucket, m))
	}
	s.logInfo("枚举对象版本",
		logx.String("bucket", bucket),
		logx.String("key", key),
	)
	return out, nil
}
