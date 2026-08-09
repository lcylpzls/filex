package proto

import (
	"time"

	"github.com/lcylpzls/filex"
)

// BucketInfoJSON 是桶信息的线格式。
type BucketInfoJSON struct {
	Name       string    `json:"name"`
	Versioning bool      `json:"versioning,omitempty"`
	Quota      int64     `json:"quota,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ObjectInfoJSON 是对象信息的线格式。
type ObjectInfoJSON struct {
	Bucket      string            `json:"bucket"`
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	ETag        string            `json:"etag"`
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	VersionID   string            `json:"version_id,omitempty"`
	Deleted     bool              `json:"deleted,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ListResultJSON 是对象列表的线格式。
type ListResultJSON struct {
	Objects        []ObjectInfoJSON `json:"objects"`
	CommonPrefixes []string         `json:"common_prefixes,omitempty"`
	NextMarker     string           `json:"next_marker,omitempty"`
	IsTruncated    bool             `json:"is_truncated"`
}

// ErrorBody 是统一错误响应体。
type ErrorBody struct {
	Code      string `json:"code"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// UploadInfoJSON 是分片上传会话的线格式。
type UploadInfoJSON struct {
	UploadID  string    `json:"upload_id"`
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
}

// PartInfoJSON 是部件的线格式。
type PartInfoJSON struct {
	PartNumber int       `json:"part_number"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PartListJSON 是部件列表的线格式。
type PartListJSON struct {
	Parts []PartInfoJSON `json:"parts"`
}

// ObjectListJSON 是对象版本列表的线格式。
type ObjectListJSON struct {
	Objects []ObjectInfoJSON `json:"objects"`
}

// ToBucketJSON 转换 filex.BucketInfo 为线格式。
func ToBucketJSON(b filex.BucketInfo) BucketInfoJSON {
	return BucketInfoJSON{
		Name:       b.Name,
		Versioning: b.Versioning,
		Quota:      b.Quota,
		CreatedAt:  b.CreatedAt,
		UpdatedAt:  b.UpdatedAt,
	}
}

// ToFilex 转换线格式为 filex.BucketInfo。
func (b BucketInfoJSON) ToFilex() filex.BucketInfo {
	return filex.BucketInfo{
		Name:       b.Name,
		Versioning: b.Versioning,
		Quota:      b.Quota,
		CreatedAt:  b.CreatedAt,
		UpdatedAt:  b.UpdatedAt,
	}
}

// ToObjectJSON 转换 filex.ObjectInfo 为线格式。
func ToObjectJSON(o filex.ObjectInfo) ObjectInfoJSON {
	return ObjectInfoJSON{
		Bucket:      o.Bucket,
		Key:         o.Key,
		Size:        o.Size,
		ETag:        o.ETag,
		ContentType: o.ContentType,
		Metadata:    cloneMap(o.Metadata),
		VersionID:   o.VersionID,
		Deleted:     o.Deleted,
		CreatedAt:   o.CreatedAt,
		UpdatedAt:   o.UpdatedAt,
	}
}

// ToFilex 转换线格式为 filex.ObjectInfo。
func (o ObjectInfoJSON) ToFilex() filex.ObjectInfo {
	return filex.ObjectInfo{
		Bucket:      o.Bucket,
		Key:         o.Key,
		Size:        o.Size,
		ETag:        o.ETag,
		ContentType: o.ContentType,
		Metadata:    cloneMap(o.Metadata),
		VersionID:   o.VersionID,
		Deleted:     o.Deleted,
		CreatedAt:   o.CreatedAt,
		UpdatedAt:   o.UpdatedAt,
	}
}

// ToListJSON 转换 filex.ListResult 为线格式。
func ToListJSON(l filex.ListResult) ListResultJSON {
	out := ListResultJSON{
		Objects:        make([]ObjectInfoJSON, 0, len(l.Objects)),
		CommonPrefixes: append([]string(nil), l.CommonPrefixes...),
		NextMarker:     l.NextMarker,
		IsTruncated:    l.IsTruncated,
	}
	for _, o := range l.Objects {
		out.Objects = append(out.Objects, ToObjectJSON(o))
	}
	return out
}

// ToFilex 转换线格式为 filex.ListResult。
func (l ListResultJSON) ToFilex() filex.ListResult {
	out := filex.ListResult{
		Objects:        make([]filex.ObjectInfo, 0, len(l.Objects)),
		CommonPrefixes: append([]string(nil), l.CommonPrefixes...),
		NextMarker:     l.NextMarker,
		IsTruncated:    l.IsTruncated,
	}
	for _, o := range l.Objects {
		out.Objects = append(out.Objects, o.ToFilex())
	}
	return out
}

// ToUploadJSON 转换 filex.UploadInfo 为线格式。
func ToUploadJSON(u filex.UploadInfo) UploadInfoJSON {
	return UploadInfoJSON{UploadID: u.UploadID, Bucket: u.Bucket, Key: u.Key, CreatedAt: u.CreatedAt}
}

// ToFilex 转换线格式为 filex.UploadInfo。
func (u UploadInfoJSON) ToFilex() filex.UploadInfo {
	return filex.UploadInfo{UploadID: u.UploadID, Bucket: u.Bucket, Key: u.Key, CreatedAt: u.CreatedAt}
}

// ToPartJSON 转换 filex.PartInfo 为线格式。
func ToPartJSON(p filex.PartInfo) PartInfoJSON {
	return PartInfoJSON{PartNumber: p.PartNumber, Size: p.Size, SHA256: p.SHA256, UpdatedAt: p.UpdatedAt}
}

// ToFilex 转换线格式为 filex.PartInfo。
func (p PartInfoJSON) ToFilex() filex.PartInfo {
	return filex.PartInfo{PartNumber: p.PartNumber, Size: p.Size, SHA256: p.SHA256, UpdatedAt: p.UpdatedAt}
}

// ToPartListJSON 转换部件切片为线格式。
func ToPartListJSON(parts []filex.PartInfo) PartListJSON {
	out := PartListJSON{Parts: make([]PartInfoJSON, 0, len(parts))}
	for _, p := range parts {
		out.Parts = append(out.Parts, ToPartJSON(p))
	}
	return out
}

// ToFilex 转换线格式为部件切片。
func (l PartListJSON) ToFilex() []filex.PartInfo {
	out := make([]filex.PartInfo, 0, len(l.Parts))
	for _, p := range l.Parts {
		out = append(out, p.ToFilex())
	}
	return out
}

// ToObjectListJSON 转换对象切片为线格式。
func ToObjectListJSON(objs []filex.ObjectInfo) ObjectListJSON {
	out := ObjectListJSON{Objects: make([]ObjectInfoJSON, 0, len(objs))}
	for _, o := range objs {
		out.Objects = append(out.Objects, ToObjectJSON(o))
	}
	return out
}

// ToFilex 转换线格式为对象切片。
func (l ObjectListJSON) ToFilex() []filex.ObjectInfo {
	out := make([]filex.ObjectInfo, 0, len(l.Objects))
	for _, o := range l.Objects {
		out = append(out, o.ToFilex())
	}
	return out
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
