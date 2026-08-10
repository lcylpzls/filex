package proto

import (
	"encoding/json"
	testx "github.com/lcylpzls/testx"
	"testing"
	"time"

	"github.com/lcylpzls/filex"
)

func TestBucketWireRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	info := filex.BucketInfo{Name: "abc", CreatedAt: now}
	w := ToBucketJSON(info)
	back := w.ToFilex()
	if back.Name != info.Name || !back.CreatedAt.Equal(now) {
		t.Fatalf("桶信息往返不符：%+v", back)
	}
}

func TestObjectWireRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	info := filex.ObjectInfo{
		Bucket:      "abc",
		Key:         "dir/key",
		Size:        5,
		ETag:        "etag",
		ContentType: "text/plain",
		Metadata:    map[string]string{"a": "b"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	w := ToObjectJSON(info)
	back := w.ToFilex()
	if back.Key != info.Key || back.Metadata["a"] != "b" || !back.CreatedAt.Equal(now) {
		t.Fatalf("对象信息往返不符：%+v", back)
	}
	if back.Metadata["a"] != "b" {
		t.Fatal("元数据应复制")
	}
	back.Metadata["a"] = "x"
	if info.Metadata["a"] != "b" {
		t.Fatal("元数据修改不应影响原对象")
	}
}

func TestListWireRoundTrip(t *testing.T) {
	l := filex.ListResult{
		Objects: []filex.ObjectInfo{
			{Bucket: "abc", Key: "a", ETag: "e1"},
			{Bucket: "abc", Key: "b", ETag: "e2"},
		},
		CommonPrefixes: []string{"dir/"},
		NextMarker:     "b",
		IsTruncated:    true,
	}
	back := ToListJSON(l).ToFilex()
	if len(back.Objects) != 2 || back.Objects[0].Key != "a" || back.Objects[1].ETag != "e2" {
		t.Fatalf("列表往返不符：%+v", back)
	}
	if len(back.CommonPrefixes) != 1 || back.CommonPrefixes[0] != "dir/" {
		t.Fatalf("公共前缀不符：%+v", back.CommonPrefixes)
	}
	if !back.IsTruncated || back.NextMarker != "b" {
		t.Fatalf("分页状态不符：%+v", back)
	}
}

func TestErrorBodyJSON(t *testing.T) {
	body := ErrorBody{Code: "filex_bucket_not_found", Kind: "not_found", Message: "桶不存在", RequestID: "r1"}
	data, err := json.Marshal(body)
	testx.RequireNoError(t, err)

	var back ErrorBody
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Code != body.Code || back.RequestID != "r1" {
		t.Fatalf("错误体往返不符：%+v", back)
	}
}

func TestUploadWireRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	u := filex.UploadInfo{UploadID: "u1", Bucket: "abc", Key: "k", CreatedAt: now}
	back := ToUploadJSON(u).ToFilex()
	if back.UploadID != "u1" || back.Key != "k" || !back.CreatedAt.Equal(now) {
		t.Fatalf("上传会话往返不符：%+v", back)
	}
}

func TestPartWireRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	p := filex.PartInfo{PartNumber: 2, Size: 10, SHA256: "sha", UpdatedAt: now}
	back := ToPartJSON(p).ToFilex()
	if back.PartNumber != 2 || back.Size != 10 || back.SHA256 != "sha" || !back.UpdatedAt.Equal(now) {
		t.Fatalf("部件往返不符：%+v", back)
	}
}

func TestPartListWireRoundTrip(t *testing.T) {
	parts := []filex.PartInfo{{PartNumber: 1, Size: 3}, {PartNumber: 2, Size: 4}}
	back := ToPartListJSON(parts).ToFilex()
	if len(back) != 2 || back[0].PartNumber != 1 || back[1].Size != 4 {
		t.Fatalf("部件列表往返不符：%+v", back)
	}
}

func TestObjectListWireRoundTrip(t *testing.T) {
	objs := []filex.ObjectInfo{
		{Bucket: "abc", Key: "k", ETag: "e1", VersionID: "v1"},
		{Bucket: "abc", Key: "k", ETag: "e2", VersionID: "v2", Deleted: true},
	}
	back := ToObjectListJSON(objs).ToFilex()
	if len(back) != 2 || back[0].VersionID != "v1" || !back[1].Deleted {
		t.Fatalf("对象列表往返不符：%+v", back)
	}
}
