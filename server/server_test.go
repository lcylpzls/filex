package server

import (
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
	"github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/filex/proto"
	"github.com/lcylpzls/logx"
)

type fakeLogger struct {
	infos []string
}

func (f *fakeLogger) Info(msg string, _ logx.FieldGroup)  { f.infos = append(f.infos, msg) }
func (f *fakeLogger) Warn(msg string, _ logx.FieldGroup)  {}
func (f *fakeLogger) Error(msg string, _ logx.FieldGroup) {}

func newTestServer(t *testing.T) (*httptest.Server, *filex.Store, *fakeLogger) {
	t.Helper()
	store, err := filex.New(filex.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	logger := &fakeLogger{}
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: store, Logger: logger}))
	t.Cleanup(ts.Close)
	return ts, store, logger
}

func TestServerLifecycle(t *testing.T) {
	ts, _, logger := newTestServer(t)
	ctx := context.Background()
	c, err := client.New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	info, err := c.CreateBucket(ctx, "my-bucket")
	if err != nil {
		t.Fatalf("创建桶失败：%v", err)
	}
	if info.Name != "my-bucket" {
		t.Fatalf("桶名不符：%s", info.Name)
	}
	buckets, err := c.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 || buckets[0].Name != "my-bucket" {
		t.Fatalf("桶列表不符：%+v, %v", buckets, err)
	}
	if _, err := c.HeadBucket(ctx, "my-bucket"); err != nil {
		t.Fatalf("HeadBucket 失败：%v", err)
	}

	content := "你好，协议"
	objInfo, err := c.Put(ctx, "my-bucket", "dir/file.txt", strings.NewReader(content),
		filex.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"owner": "me"}})
	if err != nil {
		t.Fatalf("Put 失败：%v", err)
	}
	if objInfo.Size != int64(len(content)) {
		t.Fatalf("对象大小不符：%d", objInfo.Size)
	}

	head, err := c.Head(ctx, "my-bucket", "dir/file.txt")
	if err != nil {
		t.Fatalf("Head 失败：%v", err)
	}
	if head.ETag != objInfo.ETag || head.Metadata["owner"] != "me" {
		t.Fatalf("Head 元数据不符：%+v", head)
	}

	obj, err := c.Get(ctx, "my-bucket", "dir/file.txt", filex.GetOptions{Verify: true})
	if err != nil {
		t.Fatalf("Get 失败：%v", err)
	}
	data, err := io.ReadAll(obj)
	_ = obj.Close()
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if string(data) != content {
		t.Fatalf("内容不符：%s", data)
	}

	_, _ = c.Put(ctx, "my-bucket", "ascii.txt", strings.NewReader("hello"), filex.PutOptions{})
	rng := filex.ByteRange{Start: 1, End: 3}
	obj2, err := c.Get(ctx, "my-bucket", "ascii.txt", filex.GetOptions{Range: &rng})
	if err != nil {
		t.Fatalf("范围读取失败：%v", err)
	}
	data2, _ := io.ReadAll(obj2)
	_ = obj2.Close()
	if string(data2) != "ell" {
		t.Fatalf("范围内容不符：%q", data2)
	}

	list, err := c.List(ctx, "my-bucket", filex.ListOptions{Prefix: "dir/"})
	if err != nil || len(list.Objects) != 1 || list.Objects[0].Key != "dir/file.txt" {
		t.Fatalf("列表不符：%+v, %v", list, err)
	}

	if err := c.Delete(ctx, "my-bucket", "dir/file.txt"); err != nil {
		t.Fatalf("Delete 失败：%v", err)
	}
	if err := c.Delete(ctx, "my-bucket", "ascii.txt"); err != nil {
		t.Fatalf("Delete ascii 失败：%v", err)
	}
	if err := c.DeleteBucket(ctx, "my-bucket"); err != nil {
		t.Fatalf("DeleteBucket 失败：%v", err)
	}
	if len(logger.infos) == 0 {
		t.Fatal("应记录协议日志")
	}
}

func TestServerRawHTTP(t *testing.T) {
	ts, _, _ := newTestServer(t)
	ctx := context.Background()
	c, _ := client.New(ts.URL)
	_, _ = c.CreateBucket(ctx, "abc")
	_, _ = c.Put(ctx, "abc", "k", strings.NewReader("hello"), filex.PutOptions{ContentType: "text/plain"})

	do := func(method, path string, body io.Reader, hdr http.Header) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, ts.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		for k, vs := range hdr {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	// 请求 ID 头
	resp := do("GET", "/filex/v1/buckets/abc/objects/k", nil, nil)
	if resp.Header.Get(proto.HeaderRequestID) == "" {
		t.Fatal("响应缺少请求 ID")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET 状态码不符：%d", resp.StatusCode)
	}

	// 错误 JSON 与 404
	resp = do("GET", "/filex/v1/buckets/abc/objects/missing", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("404 状态码不符：%d", resp.StatusCode)
	}
	var errBody proto.ErrorBody
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if errBody.Code != string(filex.CodeObjectNotFound) {
		t.Fatalf("错误码不符：%s", errBody.Code)
	}

	// 非法 Range → 416
	resp = do("GET", "/filex/v1/buckets/abc/objects/k", nil, http.Header{"Range": {"bytes=99-100"}})
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("非法 Range 状态码不符：%d", resp.StatusCode)
	}

	// If-None-Match 命中 → 304
	etag := `"` + sha256Hex("hello") + `"`
	resp = do("GET", "/filex/v1/buckets/abc/objects/k", nil, http.Header{"If-None-Match": {etag}})
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match 状态码不符：%d", resp.StatusCode)
	}

	// If-Match 不命中 → 412
	resp = do("GET", "/filex/v1/buckets/abc/objects/k", nil, http.Header{"If-Match": {`"deadbeef"`}})
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("If-Match 状态码不符：%d", resp.StatusCode)
	}

	// 非法元数据头 → 400
	resp = do("PUT", "/filex/v1/buckets/abc/objects/bad", strings.NewReader("v"),
		http.Header{proto.HeaderMetadata: {"{"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法元数据状态码不符：%d", resp.StatusCode)
	}

	// 不存在的桶 → 404
	resp = do("PUT", "/filex/v1/buckets/missing/objects/k", strings.NewReader("v"), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("缺失桶状态码不符：%d", resp.StatusCode)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestServerConditionalHelpers(t *testing.T) {
	if !etagListMatch(`"abc", "def"`, "def") {
		t.Fatal("多值列表应命中")
	}
	if etagListMatch(`"abc"`, "def") {
		t.Fatal("不匹配应返回 false")
	}
	if !etagListMatch("*", "any") {
		t.Fatal("星号应命中")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("If-Match", `"nope"`)
	if checkConditional(rec, req, `"real"`) {
		t.Fatal("If-Match 不命中应返回 false")
	}
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("状态码不符：%d", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("If-None-Match", `"yes"`)
	if checkConditional(rec2, req2, "yes") {
		t.Fatal("If-None-Match 命中应返回 false")
	}
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("状态码不符：%d", rec2.Code)
	}
}

func TestParseMetadataHeader(t *testing.T) {
	m, err := parseMetadataHeader("")
	if err != nil || m != nil {
		t.Fatalf("空头应返回 nil：%v, %v", m, err)
	}
	m, err = parseMetadataHeader(`{"a":"b"}`)
	if err != nil || m["a"] != "b" {
		t.Fatalf("元数据解析失败：%v, %v", m, err)
	}
	if _, err := parseMetadataHeader("{"); err == nil {
		t.Fatal("非法元数据应报错")
	} else if !errx.Is(err, filex.CodeInvalidMetadata) {
		t.Fatalf("错误码不符：%v", err)
	}
}

func TestNewHandlerPanicsWithoutStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("缺少 Store 应 panic")
		}
	}()
	NewHandler(HandlerConfig{})
}

func TestServerPanicRecovery(t *testing.T) {
	h := &handler{cfg: HandlerConfig{}, mux: http.NewServeMux()}
	h.mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码不符：%d", rec.Code)
	}
	var body proto.ErrorBody
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Code != string(filex.CodeInternal) {
		t.Fatalf("错误码不符：%s", body.Code)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("随机数失败") }

func TestNewRequestID(t *testing.T) {
	id := newRequestID()
	if len(id) != 32 {
		t.Fatalf("请求 ID 长度不符：%s", id)
	}
	old := randReader
	randReader = failReader{}
	defer func() { randReader = old }()
	fallback := newRequestID()
	if !strings.HasPrefix(fallback, "rid-") {
		t.Fatalf("回退请求 ID 不符：%s", fallback)
	}
}

type fakeStore struct {
	createErr       error
	listBucketsErr  error
	headBucketErr   error
	deleteBucketErr error
	listErr         error
	putErr          error
	getErr          error
	headErr         error
	deleteErr       error
	headInfo        filex.ObjectInfo
}

func (f *fakeStore) CreateBucket(context.Context, string) (filex.BucketInfo, error) {
	if f.createErr != nil {
		return filex.BucketInfo{}, f.createErr
	}
	return filex.BucketInfo{Name: "abc"}, nil
}

func (f *fakeStore) DeleteBucket(context.Context, string) error { return f.deleteBucketErr }
func (f *fakeStore) HeadBucket(context.Context, string) (filex.BucketInfo, error) {
	if f.headBucketErr != nil {
		return filex.BucketInfo{}, f.headBucketErr
	}
	return filex.BucketInfo{Name: "abc"}, nil
}
func (f *fakeStore) ListBuckets(context.Context) ([]filex.BucketInfo, error) {
	if f.listBucketsErr != nil {
		return nil, f.listBucketsErr
	}
	return []filex.BucketInfo{{Name: "abc"}}, nil
}
func (f *fakeStore) Put(context.Context, string, string, io.Reader, filex.PutOptions) (filex.ObjectInfo, error) {
	if f.putErr != nil {
		return filex.ObjectInfo{}, f.putErr
	}
	return f.headInfo, nil
}
func (f *fakeStore) Get(context.Context, string, string, filex.GetOptions) (*filex.Object, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &filex.Object{Info: f.headInfo, ReadCloser: io.NopCloser(strings.NewReader("x"))}, nil
}
func (f *fakeStore) Head(context.Context, string, string) (filex.ObjectInfo, error) {
	if f.headErr != nil {
		return filex.ObjectInfo{}, f.headErr
	}
	return f.headInfo, nil
}
func (f *fakeStore) Delete(context.Context, string, string) error { return f.deleteErr }
func (f *fakeStore) List(context.Context, string, filex.ListOptions) (filex.ListResult, error) {
	if f.listErr != nil {
		return filex.ListResult{}, f.listErr
	}
	return filex.ListResult{}, nil
}

func TestServerHandlerErrorBranches(t *testing.T) {
	injected := errors.New("注入错误")
	f := &fakeStore{
		createErr:       injected,
		listBucketsErr:  injected,
		headBucketErr:   injected,
		deleteBucketErr: injected,
		listErr:         injected,
		putErr:          injected,
		headErr:         injected,
		deleteErr:       injected,
	}
	ts := httptest.NewServer(NewHandler(HandlerConfig{Store: f}))
	defer ts.Close()

	cases := []struct {
		method string
		path   string
		body   io.Reader
	}{
		{http.MethodPut, "/filex/v1/buckets/abc", nil},
		{http.MethodGet, "/filex/v1/buckets", nil},
		{http.MethodHead, "/filex/v1/buckets/abc", nil},
		{http.MethodDelete, "/filex/v1/buckets/abc", nil},
		{http.MethodGet, "/filex/v1/buckets/abc/objects", nil},
		{http.MethodPut, "/filex/v1/buckets/abc/objects/k", strings.NewReader("v")},
		{http.MethodGet, "/filex/v1/buckets/abc/objects/k", nil},
		{http.MethodHead, "/filex/v1/buckets/abc/objects/k", nil},
		{http.MethodDelete, "/filex/v1/buckets/abc/objects/k", nil},
	}
	for _, c := range cases {
		req, err := http.NewRequest(c.method, ts.URL+c.path, c.body)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s 状态码应为 500，实际 %d", c.method, c.path, resp.StatusCode)
		}
	}

	// Get 成功 Head 但 Get 失败的分支
	f2 := &fakeStore{headInfo: filex.ObjectInfo{ETag: "etag", Size: 1}, getErr: injected}
	ts2 := httptest.NewServer(NewHandler(HandlerConfig{Store: f2}))
	defer ts2.Close()
	resp, err := http.Get(ts2.URL + "/filex/v1/buckets/abc/objects/k")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Get 错误分支状态码不符：%d", resp.StatusCode)
	}
}

func TestServerInvalidInputBranches(t *testing.T) {
	ts, _, _ := newTestServer(t)
	c, err := client.New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBucket(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/filex/v1/buckets/abc/objects?limit=abc", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 limit 状态码不符：%d", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/filex/v1/buckets/abc/objects?limit=1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("合法 limit 状态码不符：%d", resp.StatusCode)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/filex/v1/buckets/abc/objects/"},
		{http.MethodGet, "/filex/v1/buckets/abc/objects/"},
		{http.MethodHead, "/filex/v1/buckets/abc/objects/"},
	} {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, strings.NewReader("v"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s %s 空键状态码不符：%d", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestStatusWriterWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	_, _ = sw.Write([]byte("x"))
	if sw.status != http.StatusOK {
		t.Fatalf("Write 后状态码应为 200：%d", sw.status)
	}
	sw.WriteHeader(http.StatusTeapot)
	if sw.status != http.StatusOK {
		t.Fatalf("已写入后 WriteHeader 不应覆盖：%d", sw.status)
	}
}
