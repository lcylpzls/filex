// Package server 实现 filex 自研对象存储协议的 HTTP 处理器，
// 可直接挂载到 net/http 或 webx。
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/proto"
	"github.com/lcylpzls/logx"
)

// Store 是协议处理器所需的存储引擎接口，*filex.Store 天然满足。
type Store interface {
	CreateBucket(ctx context.Context, name string) (filex.BucketInfo, error)
	DeleteBucket(ctx context.Context, name string) error
	HeadBucket(ctx context.Context, name string) (filex.BucketInfo, error)
	ListBuckets(ctx context.Context) ([]filex.BucketInfo, error)
	Put(ctx context.Context, bucket, key string, r io.Reader, opts filex.PutOptions) (filex.ObjectInfo, error)
	Get(ctx context.Context, bucket, key string, opts filex.GetOptions) (*filex.Object, error)
	Head(ctx context.Context, bucket, key string) (filex.ObjectInfo, error)
	Delete(ctx context.Context, bucket, key string) error
	List(ctx context.Context, bucket string, opts filex.ListOptions) (filex.ListResult, error)
}

// HandlerConfig 是协议处理器的配置。
type HandlerConfig struct {
	// Store 是底层存储引擎（必填），*filex.Store 可直接使用。
	Store Store
	// Logger 是可选结构化日志。
	Logger filex.Logger
}

// randReader 可注入，便于测试随机数失败分支。
var randReader io.Reader = rand.Reader

type handler struct {
	cfg HandlerConfig
	mux *http.ServeMux
}

// NewHandler 创建协议处理器。
func NewHandler(cfg HandlerConfig) http.Handler {
	if cfg.Store == nil {
		panic("filex/server: Store 不能为空")
	}
	h := &handler{cfg: cfg, mux: http.NewServeMux()}
	h.mux.HandleFunc("PUT "+proto.BasePath+"/buckets/{bucket}", h.handleCreateBucket)
	h.mux.HandleFunc("GET "+proto.BasePath+"/buckets", h.handleListBuckets)
	h.mux.HandleFunc("HEAD "+proto.BasePath+"/buckets/{bucket}", h.handleHeadBucket)
	h.mux.HandleFunc("DELETE "+proto.BasePath+"/buckets/{bucket}", h.handleDeleteBucket)
	h.mux.HandleFunc("GET "+proto.BasePath+"/buckets/{bucket}/objects", h.handleListObjects)
	h.mux.HandleFunc("PUT "+proto.BasePath+"/buckets/{bucket}/objects/{key...}", h.handlePutObject)
	h.mux.HandleFunc("GET "+proto.BasePath+"/buckets/{bucket}/objects/{key...}", h.handleGetObject)
	h.mux.HandleFunc("HEAD "+proto.BasePath+"/buckets/{bucket}/objects/{key...}", h.handleHeadObject)
	h.mux.HandleFunc("DELETE "+proto.BasePath+"/buckets/{bucket}/objects/{key...}", h.handleDeleteObject)
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rid := r.Header.Get(proto.HeaderRequestID)
	if rid == "" {
		rid = newRequestID()
	}
	w.Header().Set(proto.HeaderRequestID, rid)
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			h.writeError(sw, rid, errx.NewCode(filex.CodeInternal, fmt.Sprintf("服务器内部错误：%v", rec)))
		}
		if h.cfg.Logger != nil {
			h.cfg.Logger.Info("协议请求", logx.Fields(
				logx.String("request_id", rid),
				logx.String("method", r.Method),
				logx.String("path", r.URL.Path),
				logx.Int("status", sw.status),
				logx.Int64("duration_ms", time.Since(start).Milliseconds()),
			))
		}
	}()
	h.mux.ServeHTTP(sw, r)
}

// statusWriter 记录实际响应状态码，供日志使用。
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(p)
}

func (h *handler) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	info, err := h.cfg.Store.CreateBucket(r.Context(), r.PathValue("bucket"))
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	writeJSON(w, http.StatusCreated, proto.ToBucketJSON(info))
}

type bucketListJSON struct {
	Buckets []proto.BucketInfoJSON `json:"buckets"`
}

func (h *handler) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.cfg.Store.ListBuckets(r.Context())
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	out := bucketListJSON{Buckets: make([]proto.BucketInfoJSON, 0, len(buckets))}
	for _, b := range buckets {
		out.Buckets = append(out.Buckets, proto.ToBucketJSON(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) handleHeadBucket(w http.ResponseWriter, r *http.Request) {
	if _, err := h.cfg.Store.HeadBucket(r.Context(), r.PathValue("bucket")); err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	if err := h.cfg.Store.DeleteBucket(r.Context(), r.PathValue("bucket")); err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) handleListObjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidArgument, "limit 必须是整数"))
			return
		}
		limit = n
	}
	result, err := h.cfg.Store.List(r.Context(), r.PathValue("bucket"), filex.ListOptions{
		Prefix:    q.Get("prefix"),
		Marker:    q.Get("marker"),
		Limit:     limit,
		Delimiter: q.Get("delimiter"),
	})
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	writeJSON(w, http.StatusOK, proto.ToListJSON(result))
}

func (h *handler) handlePutObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")
	if key == "" {
		h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidKey, "键名不能为空"))
		return
	}
	metadata, err := parseMetadataHeader(r.Header.Get(proto.HeaderMetadata))
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	opts := filex.PutOptions{
		ContentType:    r.Header.Get("Content-Type"),
		Metadata:       metadata,
		ExpectedSHA256: r.Header.Get(proto.HeaderSHA256),
	}
	info, err := h.cfg.Store.Put(r.Context(), bucket, key, r.Body, opts)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	writeJSON(w, http.StatusCreated, proto.ToObjectJSON(info))
}

func (h *handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")
	if key == "" {
		h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidKey, "键名不能为空"))
		return
	}
	info, err := h.cfg.Store.Head(r.Context(), bucket, key)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	if !checkConditional(w, r, info.ETag) {
		return
	}
	rng, hasRange, err := proto.ParseRange(r.Header.Get("Range"), info.Size)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	opts := filex.GetOptions{Verify: r.URL.Query().Get("verify") == "1"}
	if hasRange {
		opts.Range = &rng
	}
	obj, err := h.cfg.Store.Get(r.Context(), bucket, key, opts)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	defer obj.Close()
	h.writeObjectHeaders(w, obj.Info)
	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.Start, rng.End, obj.Info.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(rng.Length(), 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Info.Size, 10))
		w.WriteHeader(http.StatusOK)
	}
	_, _ = io.Copy(w, obj)
}

func (h *handler) handleHeadObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")
	if key == "" {
		h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidKey, "键名不能为空"))
		return
	}
	info, err := h.cfg.Store.Head(r.Context(), bucket, key)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	h.writeObjectHeaders(w, info)
	w.WriteHeader(http.StatusOK)
}

func (h *handler) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	if err := h.cfg.Store.Delete(r.Context(), r.PathValue("bucket"), r.PathValue("key")); err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) writeObjectHeaders(w http.ResponseWriter, info filex.ObjectInfo) {
	hdr := w.Header()
	hdr.Set("ETag", quoteETag(info.ETag))
	hdr.Set(proto.HeaderSHA256, info.ETag)
	hdr.Set(proto.HeaderSize, strconv.FormatInt(info.Size, 10))
	if info.ContentType != "" {
		hdr.Set("Content-Type", info.ContentType)
	}
	if len(info.Metadata) > 0 {
		b, _ := json.Marshal(info.Metadata)
		hdr.Set(proto.HeaderMetadata, string(b))
	}
	hdr.Set(proto.HeaderCreatedAt, info.CreatedAt.UTC().Format(time.RFC3339))
	hdr.Set("Last-Modified", info.UpdatedAt.UTC().Format(http.TimeFormat))
	hdr.Set("Accept-Ranges", "bytes")
}

func (h *handler) writeError(w http.ResponseWriter, rid string, err error) {
	body := proto.ErrorBody{Kind: "internal", Message: err.Error(), RequestID: rid}
	status := http.StatusInternalServerError
	if e, ok := errx.As(err); ok {
		body.Code = string(e.Code())
		body.Kind = e.Kind().String()
		body.Message = e.Message()
		status = e.HTTPStatus()
		if errx.Is(err, filex.CodeInvalidRange) {
			status = http.StatusRequestedRangeNotSatisfiable
		}
	}
	writeJSON(w, status, body)
}

func parseMetadataHeader(v string) (map[string]string, error) {
	if v == "" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(v), &m); err != nil {
		return nil, errx.NewCode(filex.CodeInvalidMetadata, "元数据头必须是 JSON 对象")
	}
	return m, nil
}

func quoteETag(etag string) string {
	return `"` + etag + `"`
}

func checkConditional(w http.ResponseWriter, r *http.Request, etag string) bool {
	if im := r.Header.Get("If-Match"); im != "" {
		if !etagListMatch(im, etag) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return false
		}
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etagListMatch(inm, etag) {
			w.WriteHeader(http.StatusNotModified)
			return false
		}
	}
	return true
}

func etagListMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "*" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		if strings.Trim(strings.TrimSpace(part), `"`) == etag {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requestID(r *http.Request) string {
	return r.Header.Get(proto.HeaderRequestID)
}

func newRequestID() string {
	var b [16]byte
	if _, err := io.ReadFull(randReader, b[:]); err != nil {
		return fmt.Sprintf("rid-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
