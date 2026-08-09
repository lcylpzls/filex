package proto

import (
	"time"

	"github.com/lcylpzls/filex"
)

// BucketInfoJSON 是桶信息的线格式。
type BucketInfoJSON struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ObjectInfoJSON 是对象信息的线格式。
type ObjectInfoJSON struct {
	Bucket      string            `json:"bucket"`
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	ETag        string            `json:"etag"`
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata,omitempty"`
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

// ToBucketJSON 转换 filex.BucketInfo 为线格式。
func ToBucketJSON(b filex.BucketInfo) BucketInfoJSON {
	return BucketInfoJSON{Name: b.Name, CreatedAt: b.CreatedAt}
}

// ToFilex 转换线格式为 filex.BucketInfo。
func (b BucketInfoJSON) ToFilex() filex.BucketInfo {
	return filex.BucketInfo{Name: b.Name, CreatedAt: b.CreatedAt}
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
