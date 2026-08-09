package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/proto"
	"github.com/lcylpzls/filex/server"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newClientServer(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	store, err := filex.New(filex.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ts := httptest.NewServer(server.NewHandler(server.HandlerConfig{Store: store}))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c, ts
}

func TestClientLifecycle(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()

	if _, err := c.CreateBucket(ctx, "abc"); err != nil {
		t.Fatalf("创建桶失败：%v", err)
	}
	if _, err := c.CreateBucket(ctx, "abc"); err == nil {
		t.Fatal("重复创建桶应报错")
	} else if !errx.Is(err, filex.CodeBucketExists) {
		t.Fatalf("错误码不符：%v", err)
	}
	buckets, err := c.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 {
		t.Fatalf("桶列表不符：%+v, %v", buckets, err)
	}
	if _, err := c.HeadBucket(ctx, "abc"); err != nil {
		t.Fatalf("HeadBucket 失败：%v", err)
	}

	content := "hello filex"
	info, err := c.Put(ctx, "abc", "dir/a.txt", strings.NewReader(content),
		filex.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatalf("Put 失败：%v", err)
	}
	if info.ETag != sha256Hex(content) {
		t.Fatalf("ETag 不符：%s", info.ETag)
	}

	head, err := c.Head(ctx, "abc", "dir/a.txt")
	if err != nil {
		t.Fatalf("Head 失败：%v", err)
	}
	if head.ContentType != "text/plain" || head.Metadata["k"] != "v" {
		t.Fatalf("Head 元数据不符：%+v", head)
	}

	obj, err := c.Get(ctx, "abc", "dir/a.txt", filex.GetOptions{Verify: true})
	if err != nil {
		t.Fatalf("Get 失败：%v", err)
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil || string(data) != content {
		t.Fatalf("读取内容不符：%q, %v", data, err)
	}

	list, err := c.List(ctx, "abc", filex.ListOptions{Prefix: "dir/", Delimiter: "/"})
	if err != nil || len(list.Objects) != 1 || list.Objects[0].Key != "dir/a.txt" {
		t.Fatalf("列表不符：%+v, %v", list, err)
	}

	_, _ = c.Put(ctx, "abc", "b.txt", strings.NewReader("x"), filex.PutOptions{})
	paged, err := c.List(ctx, "abc", filex.ListOptions{Limit: 1, Marker: ""})
	if err != nil || len(paged.Objects) != 1 {
		t.Fatalf("分页列表不符：%+v, %v", paged, err)
	}
	marked, err := c.List(ctx, "abc", filex.ListOptions{Marker: "a"})
	if err != nil || len(marked.Objects) == 0 {
		t.Fatalf("marker 列表不符：%+v, %v", marked, err)
	}

	rng := filex.ByteRange{Start: 1, End: 3}
	ranged, err := c.Get(ctx, "abc", "dir/a.txt", filex.GetOptions{Range: &rng})
	if err != nil {
		t.Fatalf("范围读取失败：%v", err)
	}
	rangeData, _ := io.ReadAll(ranged)
	_ = ranged.Close()
	if string(rangeData) != "ell" {
		t.Fatalf("范围内容不符：%q", rangeData)
	}

	if _, err := c.Get(ctx, "abc", "dir/a.txt", filex.GetOptions{IfMatch: `"nope"`}); err == nil {
		t.Fatal("If-Match 不命中应报错")
	}

	if err := c.Delete(ctx, "abc", "dir/a.txt"); err != nil {
		t.Fatalf("Delete 失败：%v", err)
	}
	if err := c.Delete(ctx, "abc", "b.txt"); err != nil {
		t.Fatalf("Delete b 失败：%v", err)
	}
	if _, err := c.Get(ctx, "abc", "dir/a.txt", filex.GetOptions{}); err == nil {
		t.Fatal("已删除对象应报错")
	} else if !errx.Is(err, filex.CodeObjectNotFound) {
		t.Fatalf("错误码不符：%v", err)
	}
	if err := c.DeleteBucket(ctx, "abc"); err != nil {
		t.Fatalf("DeleteBucket 失败：%v", err)
	}
}

func TestClientErrors(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()

	if _, err := c.Put(ctx, "missing", "k", strings.NewReader("v"), filex.PutOptions{}); err == nil {
		t.Fatal("缺失桶应报错")
	} else if !errx.Is(err, filex.CodeBucketNotFound) {
		t.Fatalf("错误码不符：%v", err)
	}
	if _, err := c.Head(ctx, "missing", "k"); err == nil {
		t.Fatal("缺失桶 Head 应报错")
	}
	if _, err := c.Get(ctx, "missing", "k", filex.GetOptions{}); err == nil {
		t.Fatal("缺失桶 Get 应报错")
	}
	if err := c.Delete(ctx, "missing", "k"); err == nil {
		t.Fatal("缺失桶 Delete 应报错")
	}
	if _, err := c.List(ctx, "missing", filex.ListOptions{}); err == nil {
		t.Fatal("缺失桶 List 应报错")
	}

	_, _ = c.CreateBucket(ctx, "abc")
	if _, err := c.Put(ctx, "abc", "k", strings.NewReader("v"), filex.PutOptions{
		ExpectedSHA256: strings.Repeat("0", 64),
	}); err == nil {
		t.Fatal("校验失败应报错")
	} else if !errx.Is(err, filex.CodeChecksumMismatch) {
		t.Fatalf("错误码不符：%v", err)
	}
	if _, err := c.Put(ctx, "abc", strings.Repeat("k", 1025), strings.NewReader("x"), filex.PutOptions{}); err == nil {
		t.Fatal("超长键应报错（协议层）")
	}
}

func TestClientDoAndHeaderBranches(t *testing.T) {
	c, err := New("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.do(context.Background(), ":", "/", nil, nil); err == nil {
		t.Fatal("非法方法经 do 应报错")
	}

	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("传输失败")
	})
	c.httpClient = &http.Client{Transport: rt}
	if _, err := c.Head(context.Background(), "abc", "k"); err == nil {
		t.Fatal("Head 传输失败应报错")
	}
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("Health 传输失败应报错")
	}
	if err := c.DeleteBucket(context.Background(), "abc"); err == nil {
		t.Fatal("doJSON 传输失败应报错")
	}

	rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{proto.HeaderSize: {"bad"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	c.httpClient = &http.Client{Transport: rt2}
	if _, err := c.Get(context.Background(), "abc", "k", filex.GetOptions{}); err == nil {
		t.Fatal("非法响应头应报错")
	}

	rt3 := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("传输失败")
	})
	c.httpClient = &http.Client{Transport: rt3}
	if _, err := c.GetVersion(context.Background(), "abc", "k", "v1", filex.GetOptions{}); err == nil {
		t.Fatal("GetVersion 传输失败应报错")
	}
	if _, err := c.HeadVersion(context.Background(), "abc", "k", "v1"); err == nil {
		t.Fatal("HeadVersion 传输失败应报错")
	}

	rt4 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{proto.HeaderSize: {"bad"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	c.httpClient = &http.Client{Transport: rt4}
	if _, err := c.GetVersion(context.Background(), "abc", "k", "v1", filex.GetOptions{}); err == nil {
		t.Fatal("GetVersion 非法响应头应报错")
	}
}

func TestClientOptionsAndNew(t *testing.T) {
	if _, err := New("://bad"); err == nil {
		t.Fatal("非法 URL 应报错")
	}
	if _, err := New("ftp://host"); err == nil {
		t.Fatal("非法协议应报错")
	}
	if _, err := New("http://"); err == nil {
		t.Fatal("缺少主机应报错")
	}

	var gotAuth string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	c, err := New("https://example.com", WithToken("tok"), WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.DeleteBucket(context.Background(), "abc")
	if gotAuth != "Bearer tok" {
		t.Fatalf("Authorization 头不符：%q", gotAuth)
	}

	var gotTS, gotSig string
	rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotTS = req.Header.Get("X-Filex-Timestamp")
		gotSig = req.Header.Get("X-Filex-Signature")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	c2, err := New("https://example.com", WithHMAC([]byte("secret")))
	if err != nil {
		t.Fatal(err)
	}
	c2.httpClient = &http.Client{Transport: rt2}
	_ = c2.DeleteBucket(context.Background(), "abc")
	if gotTS == "" || len(gotSig) != 64 {
		t.Fatalf("HMAC 头不符：ts=%q sig=%q", gotTS, gotSig)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestClientNewRequestAndDoErrors(t *testing.T) {
	c, err := New("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.newRequest(context.Background(), ":", "/", nil, nil); err == nil {
		t.Fatal("非法方法应报错")
	}

	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("传输失败")
	})
	c.httpClient = &http.Client{Transport: rt}
	if _, err := c.Get(context.Background(), "abc", "k", filex.GetOptions{}); err == nil {
		t.Fatal("传输失败应报错")
	}

	rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":"filex_storage_failed","kind":"unavailable","message":"磁盘故障"}`)),
			Request:    req,
		}, nil
	})
	c.httpClient = &http.Client{Transport: rt2}
	if err := c.DeleteBucket(context.Background(), "abc"); err == nil {
		t.Fatal("错误响应应映射")
	} else if !errx.Is(err, filex.CodeStorageFailed) {
		t.Fatalf("错误码不符：%v", err)
	}

	rt3 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{")),
			Request:    req,
		}, nil
	})
	c.httpClient = &http.Client{Transport: rt3}
	if _, err := c.ListBuckets(context.Background()); err == nil {
		t.Fatal("损坏 JSON 应报错")
	}
}

func TestClientDecodeErrorFallback(t *testing.T) {
	c, _ := New("https://example.com")
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("oops")),
			Request:    req,
		}, nil
	})
	c.httpClient = &http.Client{Transport: rt}
	if err := c.DeleteBucket(context.Background(), "abc"); err == nil {
		t.Fatal("应返回错误")
	} else if !errx.Is(err, filex.CodeStorageFailed) {
		t.Fatalf("回退错误码不符：%v", err)
	}
}

func TestClientParseObjectHeaders(t *testing.T) {
	hdr := http.Header{
		proto.HeaderSize:     {"5"},
		"Content-Type":       {"text/plain"},
		proto.HeaderMetadata: {`{"a":"b"}`},
	}
	hdr.Set(proto.HeaderSHA256, "etag")
	hdr.Set(proto.HeaderCreatedAt, "2026-08-10T00:00:00Z")
	hdr.Set("Last-Modified", "Mon, 10 Aug 2026 00:00:00 GMT")
	info, err := parseObjectHeaders("abc", "k", hdr)
	if err != nil {
		t.Fatalf("解析响应头失败：%v", err)
	}
	if info.Size != 5 || info.ETag != "etag" || info.Metadata["a"] != "b" {
		t.Fatalf("对象信息不符：%+v", info)
	}
	if info.CreatedAt.IsZero() || info.UpdatedAt.IsZero() {
		t.Fatalf("时间应为零值之外：%+v", info)
	}

	bad := []http.Header{
		{proto.HeaderSize: {"abc"}},
		{proto.HeaderMetadata: {"{"}},
		{proto.HeaderCreatedAt: {"bad"}},
		{"Last-Modified": {"bad"}},
	}
	for _, h := range bad {
		if _, err := parseObjectHeaders("abc", "k", h); err == nil {
			t.Fatalf("非法响应头应报错：%+v", h)
		}
	}
}

func TestClientEscapeKey(t *testing.T) {
	c, _ := New("https://example.com")
	path := c.objectPath("abc", "dir/a b.txt")
	if !strings.Contains(path, "%2F") && !strings.Contains(path, "/dir/a") {
		t.Fatalf("键路径不符：%s", path)
	}
	if strings.Contains(path, " ") {
		t.Fatalf("路径不应包含空格：%s", path)
	}
}

func TestClientGetConditional(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")
	info, err := c.Put(ctx, "abc", "k", strings.NewReader("v"), filex.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obj, err := c.Get(ctx, "abc", "k", filex.GetOptions{IfNoneMatch: `"` + info.ETag + `"`})
	if err == nil {
		_ = obj.Close()
		t.Fatal("If-None-Match 命中应返回错误")
	}
}

func TestClientMultipartLifecycle(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")

	up, err := c.InitiateMultipartUpload(ctx, "abc", "big",
		filex.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatalf("Initiate 失败：%v", err)
	}
	if _, err := c.InitiateMultipartUpload(ctx, "missing", "big", filex.PutOptions{}); err == nil {
		t.Fatal("缺失桶 Initiate 应报错")
	}
	if _, err := c.UploadPart(ctx, "abc", "big", up.UploadID, 1, strings.NewReader("aaa")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UploadPart(ctx, "abc", "big", up.UploadID, 2, strings.NewReader("bbb")); err != nil {
		t.Fatal(err)
	}
	parts, err := c.ListParts(ctx, "abc", "big", up.UploadID)
	if err != nil || len(parts) != 2 || parts[1].PartNumber != 2 {
		t.Fatalf("ListParts 不符：%+v, %v", parts, err)
	}
	info, err := c.CompleteMultipartUpload(ctx, "abc", "big", up.UploadID)
	if err != nil {
		t.Fatalf("Complete 失败：%v", err)
	}
	if info.ETag != sha256Hex("aaabbb") {
		t.Fatalf("ETag 不符：%s", info.ETag)
	}

	// 中止
	up2, _ := c.InitiateMultipartUpload(ctx, "abc", "gone", filex.PutOptions{})
	_, _ = c.UploadPart(ctx, "abc", "gone", up2.UploadID, 1, strings.NewReader("x"))
	if err := c.AbortMultipartUpload(ctx, "abc", "gone", up2.UploadID); err != nil {
		t.Fatalf("Abort 失败：%v", err)
	}
	if _, err := c.ListParts(ctx, "abc", "gone", up2.UploadID); err == nil {
		t.Fatal("中止后部件应不可见")
	}
}

func TestClientVersioningAndCopy(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")
	_, _ = c.CreateBucket(ctx, "dst")

	if _, err := c.SetBucketVersioning(ctx, "abc", true); err != nil {
		t.Fatalf("SetBucketVersioning 失败：%v", err)
	}
	if _, err := c.SetBucketQuota(ctx, "abc", 100); err != nil {
		t.Fatalf("SetBucketQuota 失败：%v", err)
	}
	v1, _ := c.Put(ctx, "abc", "k", strings.NewReader("aaa"), filex.PutOptions{})
	_, _ = c.Put(ctx, "abc", "k", strings.NewReader("bbb"), filex.PutOptions{})

	versions, err := c.ListVersions(ctx, "abc", "k")
	if err != nil || len(versions) != 2 {
		t.Fatalf("ListVersions 不符：%+v, %v", versions, err)
	}
	obj, err := c.GetVersion(ctx, "abc", "k", v1.VersionID, filex.GetOptions{})
	if err != nil {
		t.Fatalf("GetVersion 失败：%v", err)
	}
	data, _ := io.ReadAll(obj)
	_ = obj.Close()
	if string(data) != "aaa" {
		t.Fatalf("历史版本内容不符：%s", data)
	}
	if _, err := c.HeadVersion(ctx, "abc", "k", v1.VersionID); err != nil {
		t.Fatalf("HeadVersion 失败：%v", err)
	}
	_ = c.Delete(ctx, "abc", "k")
	if _, err := c.RestoreVersion(ctx, "abc", "k", v1.VersionID); err != nil {
		t.Fatalf("RestoreVersion 失败：%v", err)
	}
	versions, _ = c.ListVersions(ctx, "abc", "k")
	if err := c.DeleteVersion(ctx, "abc", "k", versions[1].VersionID); err != nil {
		t.Fatalf("DeleteVersion 失败：%v", err)
	}
	if _, err := c.Copy(ctx, "abc", "k", "dst", "c.txt"); err != nil {
		t.Fatalf("Copy 失败：%v", err)
	}
	if _, err := c.Move(ctx, "abc", "k", "dst", "m.txt"); err != nil {
		t.Fatalf("Move 失败：%v", err)
	}

	// 错误路径
	if _, err := c.GetVersion(ctx, "abc", "k", "missing", filex.GetOptions{}); err == nil {
		t.Fatal("缺失版本 GetVersion 应报错")
	}
	if _, err := c.HeadVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本 HeadVersion 应报错")
	}
	if err := c.DeleteVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本 DeleteVersion 应报错")
	}
	if _, err := c.RestoreVersion(ctx, "abc", "k", "missing"); err == nil {
		t.Fatal("缺失版本 RestoreVersion 应报错")
	}
	if _, err := c.ListVersions(ctx, "missing", "k"); err == nil {
		t.Fatal("缺失桶 ListVersions 应报错")
	}
	if _, err := c.Copy(ctx, "abc", "missing", "dst", "x"); err == nil {
		t.Fatal("缺失源 Copy 应报错")
	}
	if _, err := c.Move(ctx, "abc", "missing", "dst", "x"); err == nil {
		t.Fatal("缺失源 Move 应报错")
	}
	if _, err := c.SetBucketVersioning(ctx, "missing", true); err == nil {
		t.Fatal("缺失桶 SetBucketVersioning 应报错")
	}
	if _, err := c.SetBucketQuota(ctx, "missing", 1); err == nil {
		t.Fatal("缺失桶 SetBucketQuota 应报错")
	}
}

func TestClientPutMultipart(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")

	info, err := c.PutMultipart(ctx, "abc", "big", strings.NewReader("abcdef"), filex.PutOptions{}, 2, 2)
	if err != nil {
		t.Fatalf("PutMultipart 失败：%v", err)
	}
	if info.ETag != sha256Hex("abcdef") {
		t.Fatalf("ETag 不符：%s", info.ETag)
	}
	info2, err := c.PutMultipart(ctx, "abc", "big2", strings.NewReader("xy"), filex.PutOptions{}, 0, 0)
	if err != nil || info2.ETag != sha256Hex("xy") {
		t.Fatalf("默认参数分片上传失败：%+v, %v", info2, err)
	}
	if _, err := c.PutMultipart(ctx, "missing", "k", strings.NewReader("x"), filex.PutOptions{}, 0, 0); err == nil {
		t.Fatal("缺失桶 PutMultipart 应报错")
	}

	// 空流：无部件，应失败并自动中止
	if _, err := c.PutMultipart(ctx, "abc", "empty", strings.NewReader(""), filex.PutOptions{}, 2, 2); err == nil {
		t.Fatal("空流分片上传应报错")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("读取失败") }

func TestClientPutMultipartErrors(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")

	if _, err := c.PutMultipart(ctx, "abc", "k", errReader{}, filex.PutOptions{}, 2, 1); err == nil {
		t.Fatal("读取失败应报错")
	}
	// 部件上传失败（worker 错误）
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		switch q.Get("upload") {
		case "initiate":
			return attachReq(jsonResponse(http.StatusOK, proto.ToUploadJSON(filex.UploadInfo{UploadID: "u1"})), req)
		case "abort":
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		case "part":
			return nil, errors.New("部件传输失败")
		}
		return attachReq(jsonResponse(http.StatusOK, proto.ToObjectJSON(filex.ObjectInfo{ETag: "e"})), req)
	})
	c2, _ := New("https://example.com", WithHTTPClient(&http.Client{Transport: rt}))
	if _, err := c2.PutMultipart(ctx, "abc", "k3", strings.NewReader("a"), filex.PutOptions{}, 1, 1); err == nil {
		t.Fatal("部件失败应报错")
	}

	// 部件失败且发生在喂入过程中（多部件场景）
	c4, _ := New("https://example.com", WithHTTPClient(&http.Client{Transport: rt}))
	if _, err := c4.PutMultipart(ctx, "abc", "k5",
		strings.NewReader(strings.Repeat("a", 100)), filex.PutOptions{}, 1, 1); err == nil {
		t.Fatal("喂入过程部件失败应报错")
	}

	// 完成失败
	rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		switch q.Get("upload") {
		case "initiate":
			return attachReq(jsonResponse(http.StatusOK, proto.ToUploadJSON(filex.UploadInfo{UploadID: "u1"})), req)
		case "part":
			return attachReq(jsonResponse(http.StatusOK, proto.ToPartJSON(filex.PartInfo{PartNumber: 1})), req)
		case "complete":
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"code":"filex_storage_failed","message":"合并失败"}`)), Request: req}, nil
		case "abort":
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return attachReq(jsonResponse(http.StatusOK, proto.ToObjectJSON(filex.ObjectInfo{ETag: "e"})), req)
	})
	c3, _ := New("https://example.com", WithHTTPClient(&http.Client{Transport: rt2}))
	if _, err := c3.PutMultipart(ctx, "abc", "k4", strings.NewReader("a"), filex.PutOptions{}, 1, 1); err == nil {
		t.Fatal("完成失败应报错")
	}
}

func jsonResponse(status int, v any) *http.Response {
	data, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

func attachReq(resp *http.Response, req *http.Request) (*http.Response, error) {
	resp.Request = req
	return resp, nil
}

func TestClientWireJSON(t *testing.T) {
	var body proto.ErrorBody
	if err := json.Unmarshal([]byte(`{"code":"filex_internal","kind":"internal","message":"x","requestId":"r"}`), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID != "r" {
		t.Fatalf("线格式解析不符：%+v", body)
	}
}
