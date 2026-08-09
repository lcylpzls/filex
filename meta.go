package filex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/lcylpzls/errx"
)

// bucketMeta 是桶元数据文件格式。
type bucketMeta struct {
	Name       string         `json:"name"`
	Versioning bool           `json:"versioning,omitempty"`
	Quota      int64          `json:"quota,omitempty"`
	Lifecycle  *lifecycleMeta `json:"lifecycle,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// objectMeta 是对象元数据文件格式。
type objectMeta struct {
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	SHA256      string            `json:"sha256"`
	ContentType string            `json:"content_type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	VersionID   string            `json:"version_id,omitempty"`
	Deleted     bool              `json:"deleted,omitempty"`
	Encryption  *encryptionMeta   `json:"encryption,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

func (m objectMeta) valid() error {
	if m.Key == "" {
		return newCode(CodeMetadataCorrupt, "对象元数据缺少键名")
	}
	if m.Size < 0 {
		return newCodef(CodeMetadataCorrupt, "对象元数据大小非法：%d", m.Size)
	}
	if !isSHA256Hex(m.SHA256) {
		return newCode(CodeMetadataCorrupt, "对象元数据 SHA256 非法")
	}
	return nil
}

func readBucketMeta(fs fsOps, path string) (*bucketMeta, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeBucketMeta(data)
}

// decodeBucketMeta 从字节解码并校验桶元数据。
func decodeBucketMeta(data []byte) (*bucketMeta, error) {
	var m bucketMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errx.NewCode(CodeMetadataCorrupt, "桶元数据 JSON 损坏")
	}
	if m.Name == "" {
		return nil, newCode(CodeMetadataCorrupt, "桶元数据缺少名称")
	}
	return &m, nil
}

func readObjectMeta(fs fsOps, path string) (*objectMeta, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeObjectMeta(data)
}

// decodeObjectMeta 从字节解码并校验对象元数据。
func decodeObjectMeta(data []byte) (*objectMeta, error) {
	var m objectMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errx.NewCode(CodeMetadataCorrupt, "对象元数据 JSON 损坏")
	}
	if err := m.valid(); err != nil {
		return nil, err
	}
	return &m, nil
}

// writeJSONAtomic 以「临时文件 + fsync + rename」原子落盘 JSON。
func (s *Store) writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := s.fs.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := s.fs.CreateTemp(dir, ".meta-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	committed := false
	defer func() {
		if !committed {
			_ = f.Close()
			_ = s.fs.Remove(tmp)
		}
	}()
	if _, err := s.fs.WriteToFile(f, bytes.NewReader(data)); err != nil {
		return err
	}
	if !s.cfg.DisableSync {
		if err := s.fs.SyncFile(f); err != nil {
			return err
		}
	}
	if err := s.fs.CloseFile(f); err != nil {
		return err
	}
	if err := s.fs.Rename(tmp, path); err != nil {
		return err
	}
	committed = true
	s.syncDir(dir)
	return nil
}

func (s *Store) syncDir(dir string) {
	_ = s.fs.SyncPath(dir)
}

// ensureBucket 检查桶存在，返回桶元数据。
func (s *Store) ensureBucket(bucket string) (*bucketMeta, error) {
	meta, err := readBucketMeta(s.fs, s.bucketMetaPath(bucket))
	if os.IsNotExist(err) {
		return nil, newCode(CodeBucketNotFound, "桶不存在")
	}
	if err != nil {
		if errx.Is(err, CodeMetadataCorrupt) {
			return nil, err
		}
		return nil, wrapCode(err, CodeStorageFailed, "读取桶元数据失败")
	}
	return meta, nil
}
