// Package server 实现 filex 自研对象存储协议的 HTTP 处理器，
// 可直接挂载到 net/http 或 webx。
package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lcylpzls/cryptox"
	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/proto"
	"github.com/lcylpzls/idgenx"
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
	InitiateMultipartUpload(ctx context.Context, bucket, key string, opts filex.PutOptions) (filex.UploadInfo, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, r io.Reader) (filex.PartInfo, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) (filex.ObjectInfo, error)
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	ListParts(ctx context.Context, bucket, key, uploadID string) ([]filex.PartInfo, error)
	SetBucketVersioning(ctx context.Context, name string, enabled bool) (filex.BucketInfo, error)
	SetBucketQuota(ctx context.Context, name string, quota int64) (filex.BucketInfo, error)
	GetVersion(ctx context.Context, bucket, key, versionID string, opts filex.GetOptions) (*filex.Object, error)
	HeadVersion(ctx context.Context, bucket, key, versionID string) (filex.ObjectInfo, error)
	DeleteVersion(ctx context.Context, bucket, key, versionID string) error
	RestoreVersion(ctx context.Context, bucket, key, versionID string) (filex.ObjectInfo, error)
	ListVersions(ctx context.Context, bucket, key string) ([]filex.ObjectInfo, error)
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (filex.ObjectInfo, error)
	Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (filex.ObjectInfo, error)
	Health(ctx context.Context) error
}

// HandlerConfig 是协议处理器的配置。
type HandlerConfig struct {
	// Store 是底层存储引擎（必填），*filex.Store 可直接使用。
	Store Store
	// Logger 是可选结构化日志。
	Logger filex.Logger
	// Token 设置后要求 Authorization: Bearer <Token>。
	Token string
	// Authenticate 是可选鉴权回调；返回错误则拒绝请求。
	Authenticate func(r *http.Request) error
	// HMACSecret 设置后要求 X-Filex-Signature 请求签名。
	HMACSecret []byte
	// Audit 是可选审计回调。
	Audit func(AuditEvent)
	// Metrics 是可选请求指标。
	Metrics filex.Metrics
}

// AuditEvent 是审计事件。
type AuditEvent struct {
	RequestID string
	Method    string
	Path      string
	Subject   string
	Status    int
	At        time.Time
}

// randReader 可注入，便于测试随机数失败分支。
var randReader = idgenx.RandomHex

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
	h.mux.HandleFunc("GET "+proto.BasePath+"/health", h.handleHealth)
	h.mux.HandleFunc("PUT "+proto.BasePath+"/buckets/{bucket}", h.handleCreateBucket)
	h.mux.HandleFunc("GET "+proto.BasePath+"/buckets", h.handleListBuckets)
	h.mux.HandleFunc("GET "+proto.BasePath+"/buckets/{bucket}", h.handleGetBucket)
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
	subject := "anonymous"
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
		if h.cfg.Audit != nil {
			h.cfg.Audit(AuditEvent{
				RequestID: rid,
				Method:    r.Method,
				Path:      r.URL.Path,
				Subject:   subject,
				Status:    sw.status,
				At:        time.Now(),
			})
		}
		if h.cfg.Metrics != nil {
			if sw.status >= 400 {
				h.cfg.Metrics.IncCounter("filex.errors", []string{"bucket", "", "code", strconv.Itoa(sw.status)})
			} else {
				h.cfg.Metrics.AddCounter("filex.bytes", 1, []string{"bucket", "", "operation", "http_request"})
			}
		}
	}()
	var ok bool
	subject, ok = h.authenticate(sw, r, rid)
	if !ok {
		return
	}
	h.mux.ServeHTTP(sw, r)
}

func (h *handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := h.cfg.Store.Health(r.Context()); err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// authenticate 执行可选的 Bearer / HMAC / 回调鉴权与防重放校验。
func (h *handler) authenticate(w http.ResponseWriter, r *http.Request, rid string) (string, bool) {
	if h.cfg.Token == "" && h.cfg.Authenticate == nil && len(h.cfg.HMACSecret) == 0 {
		return "anonymous", true
	}
	tsStr := r.Header.Get("X-Filex-Timestamp")
	if tsStr == "" {
		h.writeError(w, rid, errx.NewCode(filex.CodeUnauthorized, "缺少时间戳"))
		return "", false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		h.writeError(w, rid, errx.NewCode(filex.CodeUnauthorized, "时间戳非法"))
		return "", false
	}
	diff := time.Since(time.Unix(ts, 0))
	if diff > 5*time.Minute || diff < -5*time.Minute {
		h.writeError(w, rid, errx.NewCode(filex.CodeUnauthorized, "时间戳过期"))
		return "", false
	}
	subject := "anonymous"
	if len(h.cfg.HMACSecret) > 0 {
		payload := []byte(fmt.Sprintf("%s\n%s\n%s", r.Method, r.URL.RequestURI(), tsStr))
		sig, err := hex.DecodeString(r.Header.Get("X-Filex-Signature"))
		if err != nil || !cryptox.VerifyHMAC(h.cfg.HMACSecret, payload, sig) {
			h.writeError(w, rid, errx.NewCode(filex.CodeUnauthorized, "请求签名无效"))
			return "", false
		}
		subject = "hmac"
	}
	if h.cfg.Token != "" {
		auth := r.Header.Get("Authorization")
		want := "Bearer " + h.cfg.Token
		if !cryptox.ConstantTimeEquals([]byte(auth), []byte(want)) {
			h.writeError(w, rid, errx.NewCode(filex.CodeUnauthorized, "令牌无效"))
			return "", false
		}
		subject = "token"
	}
	if h.cfg.Authenticate != nil {
		if err := h.cfg.Authenticate(r); err != nil {
			h.writeError(w, rid, errx.NewCode(filex.CodeForbidden, "鉴权回调拒绝"))
			return "", false
		}
		subject = "callback"
	}
	return subject, true
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
	q := r.URL.Query()
	if v := q.Get("versioning"); v != "" {
		info, err := h.cfg.Store.SetBucketVersioning(r.Context(), r.PathValue("bucket"), v == "true")
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToBucketJSON(info))
		return
	}
	if v := q.Get("quota"); v != "" {
		quota, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidArgument, "quota 必须是整数"))
			return
		}
		info, err := h.cfg.Store.SetBucketQuota(r.Context(), r.PathValue("bucket"), quota)
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToBucketJSON(info))
		return
	}
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

func (h *handler) handleGetBucket(w http.ResponseWriter, r *http.Request) {
	info, err := h.cfg.Store.HeadBucket(r.Context(), r.PathValue("bucket"))
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	writeJSON(w, http.StatusOK, proto.ToBucketJSON(info))
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
	q := r.URL.Query()
	if q.Get("copy") == "1" || q.Get("move") == "1" {
		srcBucket := q.Get("source-bucket")
		srcKey := q.Get("source-key")
		if srcBucket == "" || srcKey == "" {
			h.writeError(w, requestID(r), errx.NewCode(filex.CodeInvalidArgument, "copy/move 必须提供 source-bucket 与 source-key"))
			return
		}
		var info filex.ObjectInfo
		var err error
		if q.Get("move") == "1" {
			info, err = h.cfg.Store.Move(r.Context(), srcBucket, srcKey, bucket, key)
		} else {
			info, err = h.cfg.Store.Copy(r.Context(), srcBucket, srcKey, bucket, key)
		}
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToObjectJSON(info))
		return
	}
	if q.Get("restore") == "1" {
		info, err := h.cfg.Store.RestoreVersion(r.Context(), bucket, key, q.Get("version-id"))
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToObjectJSON(info))
		return
	}
	switch q.Get("upload") {
	case "initiate":
		metadata, err := parseMetadataHeader(r.Header.Get(proto.HeaderMetadata))
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		opts := filex.PutOptions{
			ContentType: r.Header.Get("Content-Type"),
			Metadata:    metadata,
		}
		info, err := h.cfg.Store.InitiateMultipartUpload(r.Context(), bucket, key, opts)
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToUploadJSON(info))
		return
	case "part":
		uploadID := q.Get("upload-id")
		partNumber, err := strconv.Atoi(q.Get("part-number"))
		if err != nil {
			h.writeError(w, requestID(r), errx.NewCode(filex.CodeUploadInvalid, "part-number 必须是整数"))
			return
		}
		info, err := h.cfg.Store.UploadPart(r.Context(), bucket, key, uploadID, partNumber, r.Body)
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToPartJSON(info))
		return
	case "complete":
		info, err := h.cfg.Store.CompleteMultipartUpload(r.Context(), bucket, key, q.Get("upload-id"))
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToObjectJSON(info))
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
	q := r.URL.Query()
	if q.Get("upload") == "parts" {
		parts, err := h.cfg.Store.ListParts(r.Context(), bucket, key, q.Get("upload-id"))
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToPartListJSON(parts))
		return
	}
	if q.Get("versions") == "true" {
		objs, err := h.cfg.Store.ListVersions(r.Context(), bucket, key)
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		writeJSON(w, http.StatusOK, proto.ToObjectListJSON(objs))
		return
	}
	if vid := q.Get("version-id"); vid != "" {
		obj, err := h.cfg.Store.GetVersion(r.Context(), bucket, key, vid, filex.GetOptions{})
		if err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		defer obj.Close()
		h.writeObjectHeaders(w, obj.Info)
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Info.Size, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, obj)
		return
	}
	info, err := h.cfg.Store.Head(r.Context(), bucket, key)
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	ok, condErr := checkConditional(w, r, info.ETag)
	if !ok {
		if errx.Is(condErr, filex.CodeNotModified) {
			w.WriteHeader(http.StatusNotModified)
		} else {
			w.WriteHeader(http.StatusPreconditionFailed)
		}
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
	var info filex.ObjectInfo
	var err error
	if vid := r.URL.Query().Get("version-id"); vid != "" {
		info, err = h.cfg.Store.HeadVersion(r.Context(), bucket, key, vid)
	} else {
		info, err = h.cfg.Store.Head(r.Context(), bucket, key)
	}
	if err != nil {
		h.writeError(w, requestID(r), err)
		return
	}
	h.writeObjectHeaders(w, info)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *handler) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	if vid := r.URL.Query().Get("version-id"); vid != "" {
		if err := h.cfg.Store.DeleteVersion(r.Context(), r.PathValue("bucket"), r.PathValue("key"), vid); err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Query().Get("upload") == "abort" {
		if err := h.cfg.Store.AbortMultipartUpload(r.Context(), r.PathValue("bucket"), r.PathValue("key"), r.URL.Query().Get("upload-id")); err != nil {
			h.writeError(w, requestID(r), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
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
	if info.VersionID != "" {
		hdr.Set(proto.HeaderVersionID, info.VersionID)
	}
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

func checkConditional(w http.ResponseWriter, r *http.Request, etag string) (bool, error) {
	if im := r.Header.Get("If-Match"); im != "" {
		if !etagListMatch(im, etag) {
			return false, errx.NewCode(filex.CodePreconditionFailed, "If-Match 前置条件不满足")
		}
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etagListMatch(inm, etag) {
			return false, errx.NewCode(filex.CodeNotModified, "对象未修改")
		}
	}
	return true, nil
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
	id, err := randReader(16)
	if err != nil {
		return fmt.Sprintf("rid-%d", time.Now().UnixNano())
	}
	return id
}
