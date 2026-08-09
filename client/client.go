// Package client 实现 filex 自研对象存储协议的客户端。
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/proto"
)

// Client 是 filex 协议客户端。
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// Option 是客户端配置项。
type Option func(*Client)

// WithHTTPClient 注入自定义 HTTP 客户端（如 httpx）。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithToken 设置 Bearer 令牌。
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// New 创建客户端。
func New(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "服务地址解析失败")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "服务地址必须使用 http 或 https")
	}
	if u.Host == "" {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "服务地址缺少主机")
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// CreateBucket 创建桶。
func (c *Client) CreateBucket(ctx context.Context, name string) (filex.BucketInfo, error) {
	path := proto.BasePath + "/buckets/" + url.PathEscape(name)
	var out proto.BucketInfoJSON
	if err := c.doJSON(ctx, http.MethodPut, path, nil, nil, &out); err != nil {
		return filex.BucketInfo{}, err
	}
	return out.ToFilex(), nil
}

// DeleteBucket 删除空桶。
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	path := proto.BasePath + "/buckets/" + url.PathEscape(name)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil, nil)
}

// HeadBucket 查询桶是否存在。
func (c *Client) HeadBucket(ctx context.Context, name string) (filex.BucketInfo, error) {
	path := proto.BasePath + "/buckets/" + url.PathEscape(name)
	return filex.BucketInfo{}, c.doJSON(ctx, http.MethodHead, path, nil, nil, nil)
}

// ListBuckets 枚举全部桶。
func (c *Client) ListBuckets(ctx context.Context) ([]filex.BucketInfo, error) {
	var out struct {
		Buckets []proto.BucketInfoJSON `json:"buckets"`
	}
	if err := c.doJSON(ctx, http.MethodGet, proto.BasePath+"/buckets", nil, nil, &out); err != nil {
		return nil, err
	}
	result := make([]filex.BucketInfo, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		result = append(result, b.ToFilex())
	}
	return result, nil
}

// Put 写入对象。
func (c *Client) Put(ctx context.Context, bucket, key string, r io.Reader, opts filex.PutOptions) (filex.ObjectInfo, error) {
	hdr := http.Header{}
	ct := opts.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	hdr.Set("Content-Type", ct)
	if opts.ExpectedSHA256 != "" {
		hdr.Set(proto.HeaderSHA256, opts.ExpectedSHA256)
	}
	if len(opts.Metadata) > 0 {
		b, _ := json.Marshal(opts.Metadata)
		hdr.Set(proto.HeaderMetadata, string(b))
	}
	var out proto.ObjectInfoJSON
	if err := c.doJSON(ctx, http.MethodPut, c.objectPath(bucket, key), r, hdr, &out); err != nil {
		return filex.ObjectInfo{}, err
	}
	return out.ToFilex(), nil
}

// Get 读取对象；返回流式内容，调用方负责 Close。
func (c *Client) Get(ctx context.Context, bucket, key string, opts filex.GetOptions) (*filex.Object, error) {
	path := c.objectPath(bucket, key)
	q := url.Values{}
	if opts.Verify {
		q.Set("verify", "1")
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	hdr := http.Header{}
	if opts.Range != nil {
		hdr.Set("Range", fmt.Sprintf("bytes=%d-%d", opts.Range.Start, opts.Range.End))
	}
	if opts.IfMatch != "" {
		hdr.Set("If-Match", opts.IfMatch)
	}
	if opts.IfNoneMatch != "" {
		hdr.Set("If-None-Match", opts.IfNoneMatch)
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil, hdr)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	info, err := parseObjectHeaders(bucket, key, resp.Header)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return &filex.Object{Info: info, ReadCloser: resp.Body}, nil
}

// Head 查询对象元数据。
func (c *Client) Head(ctx context.Context, bucket, key string) (filex.ObjectInfo, error) {
	resp, err := c.do(ctx, http.MethodHead, c.objectPath(bucket, key), nil, nil)
	if err != nil {
		return filex.ObjectInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return filex.ObjectInfo{}, decodeError(resp)
	}
	return parseObjectHeaders(bucket, key, resp.Header)
}

// Delete 删除对象。
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	return c.doJSON(ctx, http.MethodDelete, c.objectPath(bucket, key), nil, nil, nil)
}

// List 枚举对象。
func (c *Client) List(ctx context.Context, bucket string, opts filex.ListOptions) (filex.ListResult, error) {
	q := url.Values{}
	if opts.Prefix != "" {
		q.Set("prefix", opts.Prefix)
	}
	if opts.Marker != "" {
		q.Set("marker", opts.Marker)
	}
	if opts.Limit != 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Delimiter != "" {
		q.Set("delimiter", opts.Delimiter)
	}
	path := proto.BasePath + "/buckets/" + url.PathEscape(bucket) + "/objects"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out proto.ListResultJSON
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return filex.ListResult{}, err
	}
	return out.ToFilex(), nil
}

func (c *Client) objectPath(bucket, key string) string {
	return proto.BasePath + "/buckets/" + url.PathEscape(bucket) + "/objects/" + escapeKey(key)
}

func escapeKey(key string) string {
	return (&url.URL{Path: key}).EscapedPath()
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, hdr http.Header) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, body, hdr)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, hdr http.Header) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, hdr http.Header, out any) error {
	resp, err := c.do(ctx, method, path, body, hdr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var body proto.ErrorBody
	if err := json.Unmarshal(data, &body); err != nil || body.Code == "" {
		return errx.NewCode(filex.CodeStorageFailed, fmt.Sprintf("协议请求失败：HTTP %d", resp.StatusCode))
	}
	return errx.NewCode(errx.Code(body.Code), body.Message)
}

func parseObjectHeaders(bucket, key string, hdr http.Header) (filex.ObjectInfo, error) {
	info := filex.ObjectInfo{Bucket: bucket, Key: key}
	if v := hdr.Get(proto.HeaderSize); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return info, errx.NewCode(filex.CodeStorageFailed, "响应大小头非法")
		}
		info.Size = n
	}
	info.ETag = hdr.Get(proto.HeaderSHA256)
	info.ContentType = hdr.Get("Content-Type")
	if v := hdr.Get(proto.HeaderMetadata); v != "" {
		if err := json.Unmarshal([]byte(v), &info.Metadata); err != nil {
			return info, errx.NewCode(filex.CodeStorageFailed, "响应元数据头非法")
		}
	}
	if v := hdr.Get(proto.HeaderCreatedAt); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return info, errx.NewCode(filex.CodeStorageFailed, "响应创建时间头非法")
		}
		info.CreatedAt = t
	}
	if v := hdr.Get("Last-Modified"); v != "" {
		t, err := http.ParseTime(v)
		if err != nil {
			return info, errx.NewCode(filex.CodeStorageFailed, "响应修改时间头非法")
		}
		info.UpdatedAt = t
	}
	return info, nil
}
