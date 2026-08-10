package filex

import (
	"context"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lcylpzls/cryptox"
	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/logx"
)

// Put 写入对象。同一键并发写时以后完成者生效（原子 rename 保证完整）。
func (s *Store) Put(ctx context.Context, bucket, key string, r io.Reader, opts PutOptions) (info ObjectInfo, err error) {
	ctx, end := s.startTrace(ctx, "put", bucket, key)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	if r == nil {
		return ObjectInfo{}, newCode(CodeInvalidArgument, "内容读取器不能为空")
	}
	if err := validateBucketName(bucket); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateMetadata(opts.Metadata); err != nil {
		return ObjectInfo{}, err
	}
	if opts.ExpectedSHA256 != "" && !isSHA256Hex(opts.ExpectedSHA256) {
		return ObjectInfo{}, newCode(CodeInvalidArgument, "期望 SHA256 必须是 64 位十六进制")
	}

	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		s.metrics.IncError(bucket, errxCode(err))
		return ObjectInfo{}, err
	}
	unlock := s.locks.lock(bucket, key)
	defer unlock()

	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "读取桶版本配置失败")
	}
	versionID := ""
	if versioning {
		versionID = newVersionID()
	}
	dataPath, metaPath := s.objectPaths(bucket, key, versionID)
	if versionID == "" {
		if oldMeta, err := readObjectMeta(s.fs, metaPath); err == nil && oldMeta.Key != key {
			return ObjectInfo{}, newCode(CodeStorageFailed, "键哈希冲突，拒绝写入")
		}
	}
	cipherCtx := newObjectCipher(s.cfg.EncryptionKey)

	dir := filepath.Dir(dataPath)
	if err := s.fs.MkdirAll(dir, 0o755); err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "创建对象目录失败")
	}
	f, err := s.fs.CreateTemp(dir, ".tmp-*.part")
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "创建临时文件失败")
	}
	tmp := f.Name()
	cleanup := func() { _ = f.Close(); _ = s.fs.Remove(tmp) }
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()

	var dst io.Writer = f
	var encryptFinish func() error
	hasher := cryptox.NewSHA256()
	limited := io.LimitReader(newContextReader(ctx, r), s.cfg.MaxObjectSize+1)
	if cipherCtx != nil {
		var pw io.Writer
		pw, encryptFinish = encryptPipe(s.cfg.EncryptionKey, f)
		dst = io.MultiWriter(pw, hasher)
	} else {
		dst = io.MultiWriter(f, hasher)
	}
	size, err := s.fs.WriteToFile(dst, limited)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, storageErr(err, "写入对象内容失败")
	}
	if encryptFinish != nil {
		// 加密输出错误已通过写入端传播到 WriteToFile，这里仅等待完成。
		_ = encryptFinish()
	}
	if size > s.cfg.MaxObjectSize {
		return ObjectInfo{}, newCodef(CodeObjectTooLarge, "对象超过 %d 字节上限", s.cfg.MaxObjectSize)
	}
	if !s.cfg.DisableSync {
		if err := s.fs.SyncFile(f); err != nil {
			s.metrics.IncError(bucket, string(CodeStorageFailed))
			return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "同步对象内容失败")
		}
	}
	if err := s.fs.CloseFile(f); err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "关闭临时文件失败")
	}
	computed := hex.EncodeToString(hasher.Sum(nil))
	if opts.ExpectedSHA256 != "" && !strings.EqualFold(computed, opts.ExpectedSHA256) {
		s.metrics.IncError(bucket, string(CodeChecksumMismatch))
		return ObjectInfo{}, newCode(CodeChecksumMismatch, "内容 SHA256 与期望值不符")
	}
	if err := s.fs.Rename(tmp, dataPath); err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "提交对象内容失败")
	}
	committed = true

	now := time.Now().UTC()
	meta := objectMeta{
		Key:         key,
		Size:        size,
		SHA256:      computed,
		ContentType: opts.ContentType,
		Metadata:    cloneMap(opts.Metadata),
		VersionID:   versionID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if cipherCtx != nil {
		meta.Encryption = &encryptionMeta{Algorithm: encryptionAlgorithm}
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if err := s.writeJSONAtomic(metaPath, meta); err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "写入对象元数据失败")
	}
	if err := s.checkQuota(bucket); err != nil {
		if versionID == "" {
			_ = s.fs.Remove(metaPath)
			_ = s.fs.Remove(dataPath)
		} else {
			_ = s.removeVersionFiles(bucket, key, versionID)
		}
		s.metrics.IncError(bucket, string(CodeQuotaExceeded))
		return ObjectInfo{}, err
	}
	s.syncDir(dir)
	s.metrics.Add(bucket, "put", size)
	s.logInfo("写入对象",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.String("etag", computed),
		logx.String("version_id", versionID),
	)
	return objectInfoFromMeta(bucket, meta), nil
}

// Get 读取对象；opts.Verify 开启时 EOF 复验 SHA256。
func (s *Store) Get(ctx context.Context, bucket, key string, opts GetOptions) (obj *Object, err error) {
	_, end := s.startTrace(ctx, "get", bucket, key)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	if err := validateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return nil, err
	}
	if opts.Verify && opts.Range != nil {
		return nil, newCode(CodeInvalidArgument, "范围读取与完整校验不能同时使用")
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		s.metrics.IncError(bucket, errxCode(err))
		return nil, err
	}
	unlock := s.locks.rlock(bucket, key)
	defer unlock()

	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return nil, wrapCode(err, CodeStorageFailed, "读取桶版本配置失败")
	}
	var meta *objectMeta
	var dataPath string
	if versioning {
		meta, err = s.readCurrentMeta(bucket, key, true)
		if os.IsNotExist(err) {
			s.metrics.IncError(bucket, string(CodeObjectNotFound))
			return nil, newCode(CodeObjectNotFound, "对象不存在")
		}
		if err != nil {
			s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
			return nil, wrapCode(err, CodeMetadataCorrupt, "读取版本元数据失败")
		}
		dataPath, _ = s.objectPaths(bucket, key, meta.VersionID)
	} else {
		meta, err = s.readCurrentMeta(bucket, key, false)
		if os.IsNotExist(err) {
			s.metrics.IncError(bucket, string(CodeObjectNotFound))
			return nil, newCode(CodeObjectNotFound, "对象不存在")
		}
		if err != nil {
			s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
			return nil, wrapCode(err, CodeMetadataCorrupt, "读取对象元数据失败")
		}
		dataPath = s.objectDataPath(bucket, key)
	}
	return s.openObject(bucket, key, meta, dataPath, opts)
}

// openObject 打开已定位元数据的对象内容。
func (s *Store) openObject(bucket, key string, meta *objectMeta, dataPath string, opts GetOptions) (*Object, error) {
	if meta.Encryption != nil {
		if opts.Range != nil {
			return nil, newCode(CodeInvalidArgument, "加密对象不支持范围读取")
		}
	}
	info, err := s.fs.Stat(dataPath)
	if os.IsNotExist(err) {
		s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
		return nil, newCode(CodeMetadataCorrupt, "对象数据文件缺失")
	}
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return nil, wrapCode(err, CodeStorageFailed, "检查对象数据失败")
	}
	if meta.Encryption == nil && info.Size() != meta.Size {
		s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
		return nil, newCodef(CodeMetadataCorrupt, "对象大小不一致：期望 %d，实际 %d", meta.Size, info.Size())
	}
	f, err := s.fs.OpenFile(dataPath, os.O_RDONLY, 0)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return nil, wrapCode(err, CodeStorageFailed, "打开对象数据失败")
	}
	var rc io.ReadCloser
	if meta.Encryption != nil {
		if len(s.cfg.EncryptionKey) == 0 {
			_ = f.Close()
			return nil, newCode(CodeStorageFailed, "缺少加密主密钥")
		}
		base := &closeReader{
			Reader: decryptReader(s.cfg.EncryptionKey, f),
			closer: f,
		}
		if opts.Verify {
			rc = newVerifyingReader(base, meta.SHA256)
		} else {
			rc = base
		}
	} else if opts.Range != nil {
		if opts.Range.Start < 0 || opts.Range.End < opts.Range.Start || opts.Range.Start >= meta.Size {
			_ = f.Close()
			return nil, newCode(CodeInvalidRange, "字节范围越界")
		}
		rng := *opts.Range
		if rng.End >= meta.Size {
			rng.End = meta.Size - 1
		}
		rc = &sectionReadCloser{
			Reader: io.NewSectionReader(f, rng.Start, rng.Length()),
			closer: f,
		}
	} else if opts.Verify {
		rc = newVerifyingReader(f, meta.SHA256)
	} else {
		rc = f
	}
	s.metrics.Add(bucket, "get", meta.Size)
	s.logInfo("读取对象",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.String("etag", meta.SHA256),
	)
	return &Object{Info: objectInfoFromMeta(bucket, *meta), ReadCloser: rc}, nil
}

// closeReader 组合通用读取器与关闭器。
type closeReader struct {
	io.Reader
	closer io.Closer
}

func (c *closeReader) Close() error {
	return c.closer.Close()
}

// Head 读取对象元数据，不打开内容。
func (s *Store) Head(ctx context.Context, bucket, key string) (info ObjectInfo, err error) {
	_, end := s.startTrace(ctx, "head", bucket, key)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateBucketName(bucket); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return ObjectInfo{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		s.metrics.IncError(bucket, errxCode(err))
		return ObjectInfo{}, err
	}
	unlock := s.locks.rlock(bucket, key)
	defer unlock()
	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "读取桶版本配置失败")
	}
	var meta *objectMeta
	if versioning {
		meta, err = s.readCurrentMeta(bucket, key, true)
		if os.IsNotExist(err) {
			s.metrics.IncError(bucket, string(CodeObjectNotFound))
			return ObjectInfo{}, newCode(CodeObjectNotFound, "对象不存在")
		}
		if err != nil {
			s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
			return ObjectInfo{}, wrapCode(err, CodeMetadataCorrupt, "读取版本元数据失败")
		}
	} else {
		meta, err = s.readCurrentMeta(bucket, key, false)
		if os.IsNotExist(err) {
			s.metrics.IncError(bucket, string(CodeObjectNotFound))
			return ObjectInfo{}, newCode(CodeObjectNotFound, "对象不存在")
		}
		if err != nil {
			s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
			return ObjectInfo{}, wrapCode(err, CodeMetadataCorrupt, "读取对象元数据失败")
		}
	}
	s.logInfo("查询对象头",
		logx.String("bucket", bucket),
		logx.String("key", key),
	)
	return objectInfoFromMeta(bucket, *meta), nil
}

// Delete 删除对象。元数据先删，数据删除失败时交由孤儿清理。
func (s *Store) Delete(ctx context.Context, bucket, key string) (err error) {
	_, end := s.startTrace(ctx, "delete", bucket, key)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		s.metrics.IncError(bucket, errxCode(err))
		return err
	}
	unlock := s.locks.lock(bucket, key)
	defer unlock()

	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return wrapCode(err, CodeStorageFailed, "读取桶版本配置失败")
	}
	if versioning {
		latest, err := s.readCurrentMeta(bucket, key, true)
		if os.IsNotExist(err) {
			s.metrics.IncError(bucket, string(CodeObjectNotFound))
			return newCode(CodeObjectNotFound, "对象不存在")
		}
		if err != nil {
			s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
			return wrapCode(err, CodeMetadataCorrupt, "读取版本元数据失败")
		}
		now := time.Now().UTC()
		marker := objectMeta{
			Key:         key,
			Size:        0,
			SHA256:      emptySHA256,
			ContentType: "application/octet-stream",
			VersionID:   newVersionID(),
			Deleted:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.writeJSONAtomic(s.versionMetaPath(bucket, key, marker.VersionID), marker); err != nil {
			s.metrics.IncError(bucket, string(CodeStorageFailed))
			return wrapCode(err, CodeStorageFailed, "写入删除标记失败")
		}
		s.metrics.Add(bucket, "delete", latest.Size)
		s.logInfo("软删除对象",
			logx.String("bucket", bucket),
			logx.String("key", key),
			logx.String("version_id", marker.VersionID),
		)
		return nil
	}

	metaPath := s.objectMetaPath(bucket, key)
	meta, err := readObjectMeta(s.fs, metaPath)
	if os.IsNotExist(err) {
		s.metrics.IncError(bucket, string(CodeObjectNotFound))
		return newCode(CodeObjectNotFound, "对象不存在")
	}
	if err != nil {
		s.metrics.IncError(bucket, string(CodeMetadataCorrupt))
		return wrapCode(err, CodeMetadataCorrupt, "读取对象元数据失败")
	}
	if err := s.fs.Remove(metaPath); err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return wrapCode(err, CodeStorageFailed, "删除对象元数据失败")
	}
	if err := s.fs.Remove(s.objectDataPath(bucket, key)); err != nil && !os.IsNotExist(err) {
		s.logWarn("对象数据删除失败，等待孤儿清理",
			logx.String("bucket", bucket),
			logx.String("key", key),
		)
	}
	s.metrics.Add(bucket, "delete", meta.Size)
	s.logInfo("删除对象",
		logx.String("bucket", bucket),
		logx.String("key", key),
	)
	return nil
}

// List 枚举对象，支持 prefix / marker / limit / delimiter。
func (s *Store) List(ctx context.Context, bucket string, opts ListOptions) (result ListResult, err error) {
	ctx, end := s.startTrace(ctx, "list", bucket, "")
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return ListResult{}, err
	}
	if err := validateBucketName(bucket); err != nil {
		return ListResult{}, err
	}
	limit := opts.Limit
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit < 0 || limit > maxListLimit {
		return ListResult{}, newCodef(CodeInvalidArgument, "limit 必须在 1-%d 之间", maxListLimit)
	}
	if err := ctx.Err(); err != nil {
		return ListResult{}, wrapCtxErr(err)
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		s.metrics.IncError(bucket, errxCode(err))
		return ListResult{}, err
	}

	metas, err := s.collectCurrentMetas(bucket)
	if err != nil {
		s.metrics.IncError(bucket, string(CodeStorageFailed))
		return ListResult{}, wrapCode(err, CodeStorageFailed, "扫描对象目录失败")
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Key < metas[j].Key })

	result = ListResult{}
	seen := map[string]struct{}{}
	count := 0
	lastKey := ""
	for _, m := range metas {
		if m.Key <= opts.Marker {
			continue
		}
		if !strings.HasPrefix(m.Key, opts.Prefix) {
			continue
		}
		lastKey = m.Key
		if opts.Delimiter != "" {
			rest := m.Key[len(opts.Prefix):]
			if idx := strings.Index(rest, opts.Delimiter); idx >= 0 {
				cp := opts.Prefix + rest[:idx+len(opts.Delimiter)]
				if _, ok := seen[cp]; ok {
					continue
				}
				seen[cp] = struct{}{}
				result.CommonPrefixes = append(result.CommonPrefixes, cp)
				count++
				if count >= limit {
					result.IsTruncated = true
					result.NextMarker = lastKey
					return result, nil
				}
				continue
			}
		}
		result.Objects = append(result.Objects, objectInfoFromMeta(bucket, m))
		count++
		if count >= limit {
			result.IsTruncated = true
			result.NextMarker = lastKey
			return result, nil
		}
	}
	s.logInfo("枚举对象",
		logx.String("bucket", bucket),
		logx.String("prefix", opts.Prefix),
	)
	return result, nil
}

// Copy 复制对象（保留源内容类型与元数据）。
func (s *Store) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (info ObjectInfo, err error) {
	ctx, end := s.startTrace(ctx, "copy", srcBucket, srcKey)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	obj, err := s.Get(withInternal(ctx), srcBucket, srcKey, GetOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	defer obj.Close()
	return s.Put(withInternal(ctx), dstBucket, dstKey, obj, PutOptions{
		ContentType: obj.Info.ContentType,
		Metadata:    obj.Info.Metadata,
	})
}

// Move 移动对象（复制成功后删除源）。
func (s *Store) Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (info ObjectInfo, err error) {
	ctx, end := s.startTrace(ctx, "move", srcBucket, srcKey)
	defer func() { end(err) }()
	if err := s.ensureOpen(); err != nil {
		return ObjectInfo{}, err
	}
	info, err = s.Copy(withInternal(ctx), srcBucket, srcKey, dstBucket, dstKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := s.Delete(withInternal(ctx), srcBucket, srcKey); err != nil {
		return ObjectInfo{}, err
	}
	return info, nil
}

// objectInfoFromMeta 将对象元数据转换为公开快照。
func objectInfoFromMeta(bucket string, m objectMeta) ObjectInfo {
	ct := m.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	return ObjectInfo{
		Bucket:      bucket,
		Key:         m.Key,
		Size:        m.Size,
		ETag:        m.SHA256,
		ContentType: ct,
		Metadata:    cloneMap(m.Metadata),
		VersionID:   m.VersionID,
		Deleted:     m.Deleted,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func cloneMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// verifyingReader 在 EOF 时校验内容 SHA256。
type verifyingReader struct {
	r        io.Reader
	h        hash.Hash
	expected string
}

func newVerifyingReader(r io.Reader, expected string) *verifyingReader {
	return &verifyingReader{r: r, h: cryptox.NewSHA256(), expected: expected}
}

func (v *verifyingReader) Read(p []byte) (int, error) {
	n, err := v.r.Read(p)
	if n > 0 {
		_, _ = v.h.Write(p[:n])
	}
	if err == io.EOF {
		got := hex.EncodeToString(v.h.Sum(nil))
		if !strings.EqualFold(got, v.expected) {
			return n, errx.NewCode(CodeChecksumMismatch, "读取内容 SHA256 与元数据不符")
		}
	}
	return n, err
}

func (v *verifyingReader) Close() error {
	if c, ok := v.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// sectionReadCloser 组合范围读取器与底层文件关闭。
type sectionReadCloser struct {
	io.Reader
	closer io.Closer
}

func (s *sectionReadCloser) Close() error {
	return s.closer.Close()
}
