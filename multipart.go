package filex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/logx"
)

// maxUploadParts 是单次分片上传的部件数量上限。
const maxUploadParts = 10000

// uploadRand 可注入，便于测试随机数失败分支。
var uploadRand io.Reader = rand.Reader

// UploadInfo 是分片上传会话信息。
type UploadInfo struct {
	UploadID  string
	Bucket    string
	Key       string
	CreatedAt time.Time
}

// PartInfo 是已上传部件信息。
type PartInfo struct {
	PartNumber int
	Size       int64
	SHA256     string
	UpdatedAt  time.Time
}

// uploadMeta 是上传会话元数据文件格式。
type uploadMeta struct {
	UploadID    string            `json:"upload_id"`
	Bucket      string            `json:"bucket"`
	Key         string            `json:"key"`
	ContentType string            `json:"content_type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// partMeta 是部件元数据文件格式。
type partMeta struct {
	PartNumber int       `json:"part_number"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (m uploadMeta) valid() error {
	if m.UploadID == "" || m.Bucket == "" || m.Key == "" {
		return newCode(CodeUploadInvalid, "上传会话元数据缺少关键字段")
	}
	return nil
}

func (m partMeta) valid() error {
	if m.PartNumber < 1 || m.Size < 0 || !isSHA256Hex(m.SHA256) {
		return newCode(CodeUploadInvalid, "部件元数据非法")
	}
	return nil
}

func decodeUploadMeta(data []byte) (*uploadMeta, error) {
	var m uploadMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errx.NewCode(CodeUploadInvalid, "上传会话元数据 JSON 损坏")
	}
	if err := m.valid(); err != nil {
		return nil, err
	}
	return &m, nil
}

func decodePartMeta(data []byte) (*partMeta, error) {
	var m partMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errx.NewCode(CodeUploadInvalid, "部件元数据 JSON 损坏")
	}
	if err := m.valid(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) uploadDir(bucket, uploadID string) string {
	return filepath.Join(s.objectsDir(bucket), ".uploads", uploadID)
}

func (s *Store) uploadMetaPath(bucket, uploadID string) string {
	return filepath.Join(s.uploadDir(bucket, uploadID), "upload.json")
}

func (s *Store) partDataPath(bucket, uploadID string, n int) string {
	return filepath.Join(s.uploadDir(bucket, uploadID), fmt.Sprintf("part-%d.part", n))
}

func (s *Store) partMetaPath(bucket, uploadID string, n int) string {
	return filepath.Join(s.uploadDir(bucket, uploadID), fmt.Sprintf("part-%d.json", n))
}

// InitiateMultipartUpload 创建分片上传会话。
func (s *Store) InitiateMultipartUpload(ctx context.Context, bucket, key string, opts PutOptions) (UploadInfo, error) {
	if err := validateBucketName(bucket); err != nil {
		return UploadInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return UploadInfo{}, err
	}
	if err := validateMetadata(opts.Metadata); err != nil {
		return UploadInfo{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return UploadInfo{}, err
	}

	uploadID := newUploadID()
	meta := uploadMeta{
		UploadID:    uploadID,
		Bucket:      bucket,
		Key:         key,
		ContentType: opts.ContentType,
		Metadata:    cloneMap(opts.Metadata),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.writeJSONAtomic(s.uploadMetaPath(bucket, uploadID), meta); err != nil {
		return UploadInfo{}, wrapCode(err, CodeStorageFailed, "写入上传会话失败")
	}
	s.metrics.Add(bucket, "initiate_upload", 0)
	s.logInfo("创建分片上传会话",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.String("upload_id", uploadID),
	)
	return UploadInfo{UploadID: uploadID, Bucket: bucket, Key: key, CreatedAt: meta.CreatedAt}, nil
}

// UploadPart 上传单个部件；同部件重复上传为幂等覆盖。
func (s *Store) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, r io.Reader) (PartInfo, error) {
	if r == nil {
		return PartInfo{}, newCode(CodeInvalidArgument, "部件内容读取器不能为空")
	}
	if partNumber < 1 || partNumber > maxUploadParts {
		return PartInfo{}, newCodef(CodeUploadInvalid, "部件号必须在 1-%d 之间", maxUploadParts)
	}
	if err := validateBucketName(bucket); err != nil {
		return PartInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return PartInfo{}, err
	}
	if uploadID == "" {
		return PartInfo{}, newCode(CodeUploadInvalid, "上传会话 ID 不能为空")
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return PartInfo{}, err
	}
	unlock := s.locks.lock(bucket, "upload:"+uploadID)
	defer unlock()

	um, err := readUploadMeta(s.fs, s.uploadMetaPath(bucket, uploadID))
	if os.IsNotExist(err) {
		return PartInfo{}, newCode(CodeUploadNotFound, "上传会话不存在")
	}
	if err != nil {
		return PartInfo{}, err
	}
	if um.Bucket != bucket || um.Key != key {
		return PartInfo{}, newCode(CodeUploadInvalid, "上传会话与桶/键不匹配")
	}

	dir := s.uploadDir(bucket, uploadID)
	f, err := s.fs.CreateTemp(dir, ".part-*.tmp")
	if err != nil {
		return PartInfo{}, wrapCode(err, CodeStorageFailed, "创建部件临时文件失败")
	}
	tmp := f.Name()
	cleanup := func() { _ = f.Close(); _ = s.fs.Remove(tmp) }
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()
	hasher := sha256.New()
	size, err := s.fs.WriteToFile(io.MultiWriter(f, hasher), io.LimitReader(r, s.cfg.MaxObjectSize+1))
	if err != nil {
		return PartInfo{}, wrapCode(err, CodeStorageFailed, "写入部件内容失败")
	}
	if size > s.cfg.MaxObjectSize {
		return PartInfo{}, newCodef(CodeObjectTooLarge, "部件超过 %d 字节上限", s.cfg.MaxObjectSize)
	}
	if !s.cfg.DisableSync {
		if err := s.fs.SyncFile(f); err != nil {
			return PartInfo{}, wrapCode(err, CodeStorageFailed, "同步部件内容失败")
		}
	}
	if err := s.fs.CloseFile(f); err != nil {
		return PartInfo{}, wrapCode(err, CodeStorageFailed, "关闭部件临时文件失败")
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if err := s.fs.Rename(tmp, s.partDataPath(bucket, uploadID, partNumber)); err != nil {
		return PartInfo{}, wrapCode(err, CodeStorageFailed, "提交部件内容失败")
	}
	committed = true

	pm := partMeta{
		PartNumber: partNumber,
		Size:       size,
		SHA256:     sum,
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.writeJSONAtomic(s.partMetaPath(bucket, uploadID, partNumber), pm); err != nil {
		return PartInfo{}, wrapCode(err, CodeStorageFailed, "写入部件元数据失败")
	}
	s.metrics.Add(bucket, "upload_part", size)
	s.logInfo("上传分片部件",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.Int("part_number", partNumber),
	)
	return PartInfo{PartNumber: partNumber, Size: size, SHA256: sum, UpdatedAt: pm.UpdatedAt}, nil
}

// CompleteMultipartUpload 合并部件并提交对象；成功后删除会话。
func (s *Store) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) (ObjectInfo, error) {
	if err := validateBucketName(bucket); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return ObjectInfo{}, err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return ObjectInfo{}, err
	}
	unlock := s.locks.lock(bucket, "upload:"+uploadID)
	defer unlock()

	versioning, err := s.bucketVersioning(bucket)
	if err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "读取桶版本配置失败")
	}
	versionID := ""
	if versioning {
		versionID = newVersionID()
	}
	dataPath, metaPath := s.objectPaths(bucket, key, versionID)

	um, err := readUploadMeta(s.fs, s.uploadMetaPath(bucket, uploadID))
	if os.IsNotExist(err) {
		return ObjectInfo{}, newCode(CodeUploadNotFound, "上传会话不存在")
	}
	if err != nil {
		return ObjectInfo{}, err
	}
	if um.Bucket != bucket || um.Key != key {
		return ObjectInfo{}, newCode(CodeUploadInvalid, "上传会话与桶/键不匹配")
	}

	dir := s.uploadDir(bucket, uploadID)
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "扫描上传会话失败")
	}
	numbers := make([]int, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "part-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "part-"), ".json"))
		if err != nil {
			continue
		}
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	if len(numbers) == 0 {
		return ObjectInfo{}, newCode(CodeUploadIncomplete, "上传会话没有任何部件")
	}
	for i, n := range numbers {
		if n != i+1 {
			return ObjectInfo{}, newCode(CodeUploadIncomplete, "部件号不连续")
		}
	}

	objDir := filepath.Dir(dataPath)
	if err := s.fs.MkdirAll(objDir, 0o755); err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "创建对象目录失败")
	}
	f, err := s.fs.CreateTemp(objDir, ".tmp-*.part")
	if err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "创建合并临时文件失败")
	}
	tmp := f.Name()
	cleanup := func() { _ = f.Close(); _ = s.fs.Remove(tmp) }
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()
	hasher := sha256.New()
	var total int64
	for _, n := range numbers {
		pm, err := readPartMeta(s.fs, s.partMetaPath(bucket, uploadID, n))
		if err != nil {
			return ObjectInfo{}, wrapCode(err, CodeUploadIncomplete, "读取部件元数据失败")
		}
		pf, err := s.fs.OpenFile(s.partDataPath(bucket, uploadID, n), os.O_RDONLY, 0)
		if os.IsNotExist(err) {
			return ObjectInfo{}, newCode(CodeUploadIncomplete, "部件数据缺失")
		}
		if err != nil {
			return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "打开部件数据失败")
		}
		n2, err := s.fs.WriteToFile(io.MultiWriter(f, hasher), pf)
		_ = pf.Close()
		if err != nil {
			return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "合并部件失败")
		}
		total += n2
		if n2 != pm.Size {
			return ObjectInfo{}, newCode(CodeUploadIncomplete, "部件大小与元数据不符")
		}
	}
	if total > s.cfg.MaxObjectSize {
		return ObjectInfo{}, newCodef(CodeObjectTooLarge, "合并对象超过 %d 字节上限", s.cfg.MaxObjectSize)
	}
	if !s.cfg.DisableSync {
		if err := s.fs.SyncFile(f); err != nil {
			return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "同步合并文件失败")
		}
	}
	if err := s.fs.CloseFile(f); err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "关闭合并文件失败")
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if err := s.fs.Rename(tmp, dataPath); err != nil {
		return ObjectInfo{}, wrapCode(err, CodeStorageFailed, "提交合并对象失败")
	}
	committed = true

	ct := um.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	om := objectMeta{
		Key:         key,
		Size:        total,
		SHA256:      sum,
		ContentType: ct,
		Metadata:    cloneMap(um.Metadata),
		VersionID:   versionID,
		CreatedAt:   um.CreatedAt,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.writeJSONAtomic(metaPath, om); err != nil {
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
	if err := s.fs.RemoveAll(dir); err != nil {
		s.logWarn("清理上传会话失败",
			logx.String("bucket", bucket),
			logx.String("upload_id", uploadID),
		)
	}
	s.metrics.Add(bucket, "complete_upload", total)
	s.logInfo("完成分片上传",
		logx.String("bucket", bucket),
		logx.String("key", key),
		logx.String("etag", sum),
		logx.String("version_id", versionID),
	)
	return objectInfoFromMeta(bucket, om), nil
}

// AbortMultipartUpload 中止并清理上传会话。
func (s *Store) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := validateBucketName(bucket); err != nil {
		return err
	}
	if err := validateKey(key, s.cfg.MaxKeyBytes); err != nil {
		return err
	}
	s.bucketMu.RLock()
	defer s.bucketMu.RUnlock()
	if _, err := s.ensureBucket(bucket); err != nil {
		return err
	}
	unlock := s.locks.lock(bucket, "upload:"+uploadID)
	defer unlock()

	if _, err := readUploadMeta(s.fs, s.uploadMetaPath(bucket, uploadID)); os.IsNotExist(err) {
		return newCode(CodeUploadNotFound, "上传会话不存在")
	} else if err != nil {
		return err
	}
	if err := s.fs.RemoveAll(s.uploadDir(bucket, uploadID)); err != nil {
		return wrapCode(err, CodeStorageFailed, "清理上传会话失败")
	}
	s.metrics.Add(bucket, "abort_upload", 0)
	s.logInfo("中止分片上传",
		logx.String("bucket", bucket),
		logx.String("key", key),
	)
	return nil
}

// ListParts 返回已上传部件列表（按部件号排序）。
func (s *Store) ListParts(ctx context.Context, bucket, key, uploadID string) ([]PartInfo, error) {
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
	unlock := s.locks.rlock(bucket, "upload:"+uploadID)
	defer unlock()

	if _, err := readUploadMeta(s.fs, s.uploadMetaPath(bucket, uploadID)); os.IsNotExist(err) {
		return nil, newCode(CodeUploadNotFound, "上传会话不存在")
	} else if err != nil {
		return nil, err
	}
	entries, err := s.fs.ReadDir(s.uploadDir(bucket, uploadID))
	if err != nil {
		return nil, wrapCode(err, CodeStorageFailed, "扫描部件目录失败")
	}
	parts := make([]PartInfo, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "part-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		pm, err := readPartMeta(s.fs, filepath.Join(s.uploadDir(bucket, uploadID), name))
		if err != nil {
			continue
		}
		parts = append(parts, PartInfo{
			PartNumber: pm.PartNumber,
			Size:       pm.Size,
			SHA256:     pm.SHA256,
			UpdatedAt:  pm.UpdatedAt,
		})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	s.logInfo("枚举上传部件",
		logx.String("bucket", bucket),
		logx.String("key", key),
	)
	return parts, nil
}

func readUploadMeta(fs fsOps, path string) (*uploadMeta, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeUploadMeta(data)
}

func readPartMeta(fs fsOps, path string) (*partMeta, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodePartMeta(data)
}

func newUploadID() string {
	var b [12]byte
	if _, err := io.ReadFull(uploadRand, b[:]); err != nil {
		return fmt.Sprintf("u-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
