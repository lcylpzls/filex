package filex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/logx"
)

// Store 是本地盘对象存储引擎。
type Store struct {
	cfg        Config
	bucketsDir string
	bucketMu   sync.RWMutex
	locks      stripedLocks
	fs         fsOps
	log        Logger
	metrics    Metrics
	traceHook  TraceHook
	eventHook  EventHook
	closed     atomic.Bool
}

// New 创建 Store。
func New(cfg Config) (*Store, error) {
	if cfg.DataDir == "" {
		return nil, newCode(CodeInvalidConfig, "数据目录不能为空")
	}
	if cfg.MaxObjectSize < 0 {
		return nil, newCode(CodeInvalidConfig, "对象大小上限不能为负数")
	}
	if cfg.MaxKeyBytes < 0 {
		return nil, newCode(CodeInvalidConfig, "键长度上限不能为负数")
	}
	if cfg.MaxParts < 0 {
		return nil, newCode(CodeInvalidConfig, "部件数量上限不能为负数")
	}
	if len(cfg.EncryptionKey) > 0 && len(cfg.EncryptionKey) != 32 {
		return nil, newCode(CodeInvalidConfig, "加密主密钥必须是 32 字节")
	}
	if cfg.MaxObjectSize == 0 {
		cfg.MaxObjectSize = defaultMaxObjectSize
	}
	if cfg.MaxKeyBytes == 0 {
		cfg.MaxKeyBytes = defaultMaxKeyBytes
	}
	if cfg.MaxParts == 0 {
		cfg.MaxParts = maxUploadParts
	}
	if cfg.UploadTTL <= 0 {
		cfg.UploadTTL = defaultUploadTTL
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, wrapCode(err, CodeInvalidConfig, "数据目录解析失败")
	}
	bucketsDir := filepath.Join(abs, "buckets")
	if err := os.MkdirAll(bucketsDir, 0o755); err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "创建数据目录失败")
	}
	s := &Store{
		cfg:        cfg,
		bucketsDir: bucketsDir,
		fs:         defaultFSOps,
		log:        cfg.Logger,
		metrics:    cfg.Metrics,
		traceHook:  cfg.TraceHook,
		eventHook:  cfg.EventHook,
	}
	if s.metrics == nil {
		s.metrics = nopMetrics{}
	}
	return s, nil
}

// startTrace 开始存储操作链路（无钩子时 no-op）。
func (s *Store) startTrace(ctx context.Context, op, bucket, key string) (context.Context, func(error)) {
	if s.traceHook == nil && s.eventHook == nil {
		return ctx, func(error) {}
	}
	attrs := []TraceAttr{
		{Key: "filex.operation", Value: op},
		{Key: "filex.bucket", Value: bucket},
	}
	if key != "" {
		attrs = append(attrs, TraceAttr{Key: "filex.key", Value: key})
	}
	traceEnd := func(error) {}
	if s.traceHook != nil {
		ctx, traceEnd = s.traceHook.Start(ctx, "filex."+op, attrs...)
	}
	return ctx, func(err error) {
		traceEnd(err)
		if s.eventHook != nil && !isInternal(ctx) {
			s.eventHook.OnObjectEvent(ctx, ObjectEvent{
				Bucket: bucket,
				Key:    key,
				Action: op,
				Err:    err,
			})
		}
	}
}

// internalCtxKey 标记内部操作上下文（Copy/Move 内部原语不发事件）。
type internalCtxKey struct{}

// withInternal 标记 ctx 为内部操作。
func withInternal(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalCtxKey{}, true)
}

// isInternal 判断 ctx 是否为内部操作。
func isInternal(ctx context.Context) bool {
	v, _ := ctx.Value(internalCtxKey{}).(bool)
	return v
}

// Close 关闭 Store；关闭后所有操作返回 filex_closed。
func (s *Store) Close() error {
	s.closed.Store(true)
	return nil
}

func (s *Store) ensureOpen() error {
	if s.closed.Load() {
		return newCode(CodeClosed, "存储已关闭")
	}
	return nil
}

// Health 检查存储引擎可用性。
func (s *Store) Health(ctx context.Context) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if _, err := s.fs.ReadDir(s.bucketsDir); err != nil {
		return wrapCode(err, CodeStorageFailed, "存储不可用")
	}
	return nil
}

// BucketUsage 返回桶内非删除对象的字节总量（含全部版本）。
func (s *Store) BucketUsage(ctx context.Context, bucket string) (int64, error) {
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	if err := validateBucketName(bucket); err != nil {
		return 0, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return 0, err
	}
	return s.bucketUsage(bucket)
}

// CreateBucket 创建桶。
func (s *Store) CreateBucket(ctx context.Context, name string) (BucketInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return BucketInfo{}, err
	}
	if err := validateBucketName(name); err != nil {
		return BucketInfo{}, err
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()

	metaPath := s.bucketMetaPath(name)
	if _, err := readBucketMeta(s.fs, metaPath); err == nil {
		return BucketInfo{}, newCode(CodeBucketExists, "桶已存在")
	} else if !os.IsNotExist(err) {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "检查桶失败")
	}

	objectsDir := filepath.Join(s.bucketDir(name), "objects")
	if err := s.fs.MkdirAll(objectsDir, 0o755); err != nil {
		s.metrics.IncError(name, string(CodeStorageFailed))
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "创建桶目录失败")
	}
	now := time.Now().UTC()
	meta := bucketMeta{Name: name, CreatedAt: now, UpdatedAt: now}
	if err := s.writeJSONAtomic(metaPath, meta); err != nil {
		s.metrics.IncError(name, string(CodeStorageFailed))
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "写入桶元数据失败")
	}
	s.metrics.Add(name, "create_bucket", 0)
	s.logInfo("创建桶", logx.String("bucket", name))
	return BucketInfo{
		Name:       name,
		Versioning: meta.Versioning,
		Quota:      meta.Quota,
		CreatedAt:  meta.CreatedAt,
		UpdatedAt:  meta.UpdatedAt,
	}, nil
}

// SetBucketVersioning 开关桶版本化。
func (s *Store) SetBucketVersioning(ctx context.Context, name string, enabled bool) (BucketInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return BucketInfo{}, err
	}
	if err := validateBucketName(name); err != nil {
		return BucketInfo{}, err
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(name))
	if os.IsNotExist(err) {
		return BucketInfo{}, newCode(CodeBucketNotFound, "桶不存在")
	}
	if err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "读取桶元数据失败")
	}
	meta.Versioning = enabled
	meta.UpdatedAt = time.Now().UTC()
	if err := s.writeJSONAtomic(s.bucketMetaPath(name), meta); err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "写入桶元数据失败")
	}
	s.logInfo("设置桶版本化",
		logx.String("bucket", name),
		logx.Bool("versioning", enabled),
	)
	return bucketInfoFromMeta(*meta), nil
}

// SetBucketQuota 设置桶配额（0 表示不限）。
func (s *Store) SetBucketQuota(ctx context.Context, name string, quota int64) (BucketInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return BucketInfo{}, err
	}
	if err := validateBucketName(name); err != nil {
		return BucketInfo{}, err
	}
	if quota < 0 {
		return BucketInfo{}, newCode(CodeInvalidArgument, "配额不能为负数")
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(name))
	if os.IsNotExist(err) {
		return BucketInfo{}, newCode(CodeBucketNotFound, "桶不存在")
	}
	if err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "读取桶元数据失败")
	}
	meta.Quota = quota
	meta.UpdatedAt = time.Now().UTC()
	if err := s.writeJSONAtomic(s.bucketMetaPath(name), meta); err != nil {
		return BucketInfo{}, wrapCode(err, CodeStorageFailed, "写入桶元数据失败")
	}
	s.logInfo("设置桶配额",
		logx.String("bucket", name),
		logx.Int64("quota", quota),
	)
	return bucketInfoFromMeta(*meta), nil
}

// DeleteBucket 删除空桶；存在对象时返回 filex_bucket_not_empty。
func (s *Store) DeleteBucket(ctx context.Context, name string) error {
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if err := validateBucketName(name); err != nil {
		return err
	}
	s.bucketMu.Lock()
	defer s.bucketMu.Unlock()

	if _, err := readBucketMeta(s.fs, s.bucketMetaPath(name)); os.IsNotExist(err) {
		return newCode(CodeBucketNotFound, "桶不存在")
	} else if err != nil {
		s.metrics.IncError(name, string(CodeMetadataCorrupt))
		return wrapCode(err, CodeMetadataCorrupt, "读取桶元数据失败")
	}

	metas, err := s.collectCurrentMetas(name)
	if err != nil {
		s.metrics.IncError(name, string(CodeStorageFailed))
		return wrapCode(err, CodeStorageFailed, "扫描桶内容失败")
	}
	if len(metas) > 0 {
		return newCode(CodeBucketNotEmpty, "桶非空")
	}
	if s.hasActiveUploads(name) {
		return newCode(CodeBucketNotEmpty, "桶存在活动分片上传")
	}
	if err := s.fs.RemoveAll(s.bucketDir(name)); err != nil {
		s.metrics.IncError(name, string(CodeStorageFailed))
		return wrapCode(err, CodeStorageFailed, "删除桶目录失败")
	}
	s.metrics.Add(name, "delete_bucket", 0)
	s.logInfo("删除桶", logx.String("bucket", name))
	return nil
}

// HeadBucket 读取桶元数据。
func (s *Store) HeadBucket(ctx context.Context, name string) (BucketInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return BucketInfo{}, err
	}
	if err := validateBucketName(name); err != nil {
		return BucketInfo{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	meta, err := s.ensureBucket(name)
	if err != nil {
		s.metrics.IncError(name, errxCode(err))
		return BucketInfo{}, err
	}
	return bucketInfoFromMeta(*meta), nil
}

// ListBuckets 返回全部桶，按名称排序。
func (s *Store) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	entries, err := s.fs.ReadDir(s.bucketsDir)
	if err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "扫描桶目录失败")
	}
	result := make([]BucketInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := readBucketMeta(s.fs, s.bucketMetaPath(e.Name()))
		if err != nil {
			s.logWarn("跳过损坏的桶元数据", logx.String("bucket", e.Name()))
			continue
		}
		result = append(result, bucketInfoFromMeta(*meta))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func bucketInfoFromMeta(m bucketMeta) BucketInfo {
	lc := LifecycleOptions{}
	if m.Lifecycle != nil {
		lc = LifecycleOptions{ExpireDays: m.Lifecycle.ExpireDays, MaxVersions: m.Lifecycle.MaxVersions}
	}
	return BucketInfo{
		Name:       m.Name,
		Versioning: m.Versioning,
		Quota:      m.Quota,
		Lifecycle:  lc,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

// bucketDir 返回桶目录。
func (s *Store) bucketDir(name string) string {
	return filepath.Join(s.bucketsDir, name)
}

// bucketMetaPath 返回桶元数据路径。
func (s *Store) bucketMetaPath(name string) string {
	return filepath.Join(s.bucketDir(name), "meta.json")
}

// objectsDir 返回对象目录。
func (s *Store) objectsDir(name string) string {
	return filepath.Join(s.bucketDir(name), "objects")
}

// objectDataPath 返回对象数据文件路径。
func (s *Store) objectDataPath(bucket, key string) string {
	return filepath.Join(s.objectsDir(bucket), hashKey(key)+".data")
}

// objectMetaPath 返回对象元数据文件路径。
func (s *Store) objectMetaPath(bucket, key string) string {
	return filepath.Join(s.objectsDir(bucket), hashKey(key)+".json")
}

// hashKey 计算键的 SHA256 十六进制，作为稳定的文件系统安全目录名。
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *Store) logInfo(msg string, fields ...logx.Field) {
	if s.log != nil {
		s.log.Info(msg, logx.Fields(fields...))
	}
}

func (s *Store) logWarn(msg string, fields ...logx.Field) {
	if s.log != nil {
		s.log.Warn(msg, logx.Fields(fields...))
	}
}

// errxCode 提取错误码字符串，供指标打点。
func errxCode(err error) string {
	if err == nil {
		return ""
	}
	if code, ok := errx.CodeOf(err); ok {
		return string(code)
	}
	return "unknown"
}
