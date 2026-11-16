package replication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/filex/client"
	"github.com/lcylpzls/filex/server"
	"github.com/lcylpzls/logx"
	testx "github.com/lcylpzls/testx"
)

func TestMirrorVersionedAndPermanentDelete(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	_, _ = source.SetBucketVersioning(ctx, "center", true)
	v1, err := source.Put(ctx, "center", "backup.sql", strings.NewReader("v1"), filex.PutOptions{
		ContentType: "application/sql",
		Metadata:    map[string]string{"kind": "backup"},
	})
	testx.RequireNoError(t, err)
	_, err = source.Put(ctx, "center", "backup.sql", strings.NewReader("v2"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	testx.RequireNoError(t, source.Delete(ctx, "center", "backup.sql"))

	replicator, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	report, err := replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 3 || report.Failed != 0 {
		t.Fatalf("首次镜像结果不符：%+v", report)
	}
	targetVersions, err := target.ListAllVersions(ctx, "center")
	testx.RequireNoError(t, err)
	if len(targetVersions) != 3 || !targetVersions[0].Deleted {
		t.Fatalf("目标版本不符：%+v", targetVersions)
	}
	object, err := target.GetVersion(ctx, "center", "backup.sql", v1.VersionID, filex.GetOptions{Verify: true})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != "v1" || object.Info.Metadata["kind"] != "backup" {
		t.Fatalf("目标历史版本不符：%q %+v", data, object.Info)
	}
	if _, err := target.Get(ctx, "center", "backup.sql", filex.GetOptions{}); !errx.Is(err, filex.CodeObjectNotFound) {
		t.Fatalf("目标当前对象应被删除标记隐藏：%v", err)
	}

	report, err = replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 0 || report.Skipped != 3 {
		t.Fatalf("重复镜像应幂等：%+v", report)
	}
	tasks, err := replicator.Tasks(ctx)
	testx.RequireNoError(t, err)
	if len(tasks) != 3 {
		t.Fatalf("任务数量不符：%+v", tasks)
	}
	for _, task := range tasks {
		if task.Status != TaskReplicated {
			t.Fatalf("任务应已完成：%+v", task)
		}
	}

	testx.RequireNoError(t, source.DeleteVersion(ctx, "center", "backup.sql", v1.VersionID))
	report, err = replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Deleted != 1 {
		t.Fatalf("永久删除未传播：%+v", report)
	}
	if _, err := target.HeadVersion(ctx, "center", "backup.sql", v1.VersionID); !errx.Is(err, filex.CodeVersionNotFound) {
		t.Fatalf("目标已删版本仍存在：%v", err)
	}
}

func TestMirrorAcrossHTTPEndpoints(t *testing.T) {
	sourceStore, targetStore := newStorePair(t)
	sourceServer := httptest.NewServer(server.NewHandler(server.HandlerConfig{Store: sourceStore}))
	targetServer := httptest.NewServer(server.NewHandler(server.HandlerConfig{Store: targetStore}))
	t.Cleanup(sourceServer.Close)
	t.Cleanup(targetServer.Close)
	source, err := client.New(sourceServer.URL)
	testx.RequireNoError(t, err)
	target, err := client.New(targetServer.URL)
	testx.RequireNoError(t, err)
	ctx := context.Background()
	_, err = source.CreateBucket(ctx, "center")
	testx.RequireNoError(t, err)
	_, err = source.SetBucketVersioning(ctx, "center", true)
	testx.RequireNoError(t, err)
	first, err := source.Put(ctx, "center", "backup.sql", strings.NewReader("v1"), filex.PutOptions{
		ContentType: "application/sql",
		Metadata:    map[string]string{"kind": "backup"},
	})
	testx.RequireNoError(t, err)
	_, err = source.Put(ctx, "center", "backup.sql", strings.NewReader("v2"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	_ = source.Delete(ctx, "center", "backup.sql")

	replicator, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	report, err := replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 3 {
		t.Fatalf("HTTP 镜像结果不符：%+v", report)
	}
	versions, err := target.ListAllVersions(ctx, "center")
	testx.RequireNoError(t, err)
	if len(versions) != 3 || !versions[0].Deleted {
		t.Fatalf("HTTP 目标版本不符：%+v", versions)
	}
	object, err := target.GetVersion(ctx, "center", "backup.sql", first.VersionID, filex.GetOptions{Verify: true})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != "v1" || object.Info.Metadata["kind"] != "backup" {
		t.Fatalf("HTTP 目标历史版本不符：%q %+v", data, object.Info)
	}
}

func TestMirrorUnversionedTargetIsAuthoritative(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	mustBucket(t, target, "center")
	_, err := source.Put(ctx, "center", "key", strings.NewReader("old"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	current, err := source.Put(ctx, "center", "key", strings.NewReader("new"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	_, err = target.Put(ctx, "center", "extra", strings.NewReader("remove-me"), filex.PutOptions{})
	testx.RequireNoError(t, err)

	replicator, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center", "center"},
	})
	testx.RequireNoError(t, err)
	report, err := replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 1 || report.Deleted != 1 {
		t.Fatalf("非版本化镜像结果不符：%+v", report)
	}
	object, err := target.Get(ctx, "center", "key", filex.GetOptions{Verify: true})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != "new" || object.Info.ETag != current.ETag {
		t.Fatalf("覆盖后目标内容不符：%q %+v", data, object.Info)
	}
	if _, err := target.Get(ctx, "center", "extra", filex.GetOptions{}); !errx.Is(err, filex.CodeObjectNotFound) {
		t.Fatalf("目标多余对象应被删除：%v", err)
	}

	testx.RequireNoError(t, source.Delete(ctx, "center", "key"))
	report, err = replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Deleted != 1 {
		t.Fatalf("源端删除未传播：%+v", report)
	}
	if _, err := target.Get(ctx, "center", "key", filex.GetOptions{}); !errx.Is(err, filex.CodeObjectNotFound) {
		t.Fatalf("目标对象应被删除：%v", err)
	}
}

func TestReplicationFailureBackoffAndReplay(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	_, _ = source.Put(ctx, "center", "key", strings.NewReader("value"), filex.PutOptions{})
	wrapped := &failingTarget{Store: target}
	wrapped.failPut.Store(true)
	stateDir := t.TempDir()
	replicator, err := New(Config{
		Source:    source,
		Target:    wrapped,
		StateDir:  stateDir,
		Buckets:   []string{"center"},
		RetryBase: time.Hour,
		RetryMax:  time.Hour,
		Logger:    &testLogger{},
	})
	testx.RequireNoError(t, err)
	report, err := replicator.Mirror(ctx)
	if err == nil || report.Failed != 1 {
		t.Fatalf("首次失败结果不符：%+v, %v", report, err)
	}
	tasks, err := replicator.Tasks(ctx)
	testx.RequireNoError(t, err)
	if len(tasks) != 1 || tasks[0].Status != TaskFailed || tasks[0].Attempts != 1 {
		t.Fatalf("失败任务状态不符：%+v", tasks)
	}
	if tasks[0].NextAttemptAt.Before(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("指数退避未生效：%+v", tasks[0])
	}

	wrapped.failPut.Store(false)
	restarted, err := New(Config{
		Source:    source,
		Target:    wrapped,
		StateDir:  stateDir,
		Buckets:   []string{"center"},
		RetryBase: time.Hour,
		RetryMax:  time.Hour,
	})
	testx.RequireNoError(t, err)
	report, err = restarted.Mirror(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 0 || report.Failed != 1 {
		t.Fatalf("退避期内不应立即重试：%+v", report)
	}
	report, err = restarted.Replay(ctx)
	testx.RequireNoError(t, err)
	if report.Copied != 1 || report.Failed != 0 {
		t.Fatalf("人工重放结果不符：%+v", report)
	}
	stats, err := restarted.Stats(ctx)
	testx.RequireNoError(t, err)
	if stats.Replicated != 1 || stats.Failed != 0 || stats.LastSuccessAt.IsZero() {
		t.Fatalf("统计不符：%+v", stats)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := restarted.Tasks(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tasks 应响应取消：%v", err)
	}
	if _, err := restarted.Stats(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stats 应响应取消：%v", err)
	}
	if err := restarted.Run(cancelled); err != nil {
		t.Fatalf("Run 取消应正常退出：%v", err)
	}
}

func TestReplicationChecksumFailure(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	_, err := source.Put(ctx, "center", "key", strings.NewReader("expected"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	replicator, err := New(Config{
		Source:   &corruptSource{Store: source},
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	_, err = replicator.Mirror(ctx)
	if !errx.Is(err, filex.CodeChecksumMismatch) {
		t.Fatalf("校验失败错误码不符：%v", err)
	}
	tasks, err := replicator.Tasks(ctx)
	testx.RequireNoError(t, err)
	if len(tasks) != 1 || tasks[0].Status != TaskFailed {
		t.Fatalf("校验失败任务不符：%+v", tasks)
	}
}

func TestReplicationVersionConflictDoesNotOverwrite(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	mustBucket(t, target, "center")
	_, _ = source.SetBucketVersioning(ctx, "center", true)
	_, _ = target.SetBucketVersioning(ctx, "center", true)
	info, err := source.Put(ctx, "center", "key", strings.NewReader("source"), filex.PutOptions{})
	testx.RequireNoError(t, err)
	_, err = target.PutReplica(ctx, "center", "key", strings.NewReader("tampered"), filex.ReplicaOptions{
		VersionID: info.VersionID,
		SHA256:    sha256Hex("tampered"),
		CreatedAt: info.CreatedAt,
		UpdatedAt: info.UpdatedAt,
	})
	testx.RequireNoError(t, err)

	replicator, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	_, err = replicator.Mirror(ctx)
	if !errx.Is(err, filex.CodePreconditionFailed) {
		t.Fatalf("版本冲突错误码不符：%v", err)
	}
	object, err := target.GetVersion(ctx, "center", "key", info.VersionID, filex.GetOptions{})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != "tampered" {
		t.Fatalf("冲突版本不应被覆盖：%q", data)
	}
}

func TestReplicationConfigMetricsAndTrace(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	_, _ = source.Put(ctx, "center", "key", strings.NewReader("value"), filex.PutOptions{})
	metrics := &testMetrics{}
	trace := &testTrace{}
	replicator, err := New(Config{
		Source:    source,
		Target:    target,
		StateDir:  t.TempDir(),
		Buckets:   []string{"center"},
		Metrics:   metrics,
		TraceHook: trace,
	})
	testx.RequireNoError(t, err)
	_, err = replicator.Mirror(ctx)
	testx.RequireNoError(t, err)
	if len(trace.names) != 1 || trace.names[0] != "filex.replication" || trace.errs[0] != nil {
		t.Fatalf("追踪记录不符：%+v", trace)
	}
	if _, ok := metrics.gauges["filex.replication.pending"]; !ok {
		t.Fatalf("待复制指标未记录：%+v", metrics.gauges)
	}
	if _, ok := metrics.gauges["filex.replication.failed"]; !ok {
		t.Fatalf("指标未记录：%+v", metrics.gauges)
	}
	if metrics.durations != 1 {
		t.Fatalf("耗时指标未记录：%d", metrics.durations)
	}

	invalid := []Config{
		{Target: target, StateDir: t.TempDir(), Buckets: []string{"center"}},
		{Source: source, StateDir: t.TempDir(), Buckets: []string{"center"}},
		{Source: source, Target: target, Buckets: []string{"center"}},
		{Source: source, Target: target, StateDir: t.TempDir()},
		{Source: source, Target: target, StateDir: t.TempDir(), Buckets: []string{"center"}, RetryBase: time.Hour, RetryMax: time.Minute},
	}
	for _, cfg := range invalid {
		if _, err := New(cfg); !errx.Is(err, filex.CodeInvalidConfig) {
			t.Fatalf("非法配置应报错：%+v, %v", cfg, err)
		}
	}
}

func newStorePair(t *testing.T) (*filex.Store, *filex.Store) {
	t.Helper()
	source, err := filex.New(filex.Config{DataDir: t.TempDir()})
	testx.RequireNoError(t, err)
	target, err := filex.New(filex.Config{DataDir: t.TempDir()})
	testx.RequireNoError(t, err)
	t.Cleanup(func() {
		_ = source.Close()
		_ = target.Close()
	})
	return source, target
}

func mustBucket(t *testing.T, store *filex.Store, bucket string) {
	t.Helper()
	_, err := store.CreateBucket(context.Background(), bucket)
	testx.RequireNoError(t, err)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type failingTarget struct {
	*filex.Store
	failPut atomic.Bool
}

func (f *failingTarget) PutReplica(ctx context.Context, bucket, key string, reader io.Reader,
	opts filex.ReplicaOptions) (filex.ObjectInfo, error) {
	if f.failPut.Load() {
		return filex.ObjectInfo{}, errors.New("目标端不可用")
	}
	return f.Store.PutReplica(ctx, bucket, key, reader, opts)
}

type corruptSource struct {
	*filex.Store
}

func (c *corruptSource) Get(ctx context.Context, bucket, key string,
	opts filex.GetOptions) (*filex.Object, error) {
	object, err := c.Store.Get(ctx, bucket, key, opts)
	if err != nil {
		return nil, err
	}
	info := object.Info
	_ = object.Close()
	return &filex.Object{Info: info, ReadCloser: io.NopCloser(strings.NewReader("tampered"))}, nil
}

type testLogger struct{}

func (*testLogger) Info(string, logx.FieldGroup)  {}
func (*testLogger) Warn(string, logx.FieldGroup)  {}
func (*testLogger) Error(string, logx.FieldGroup) {}

type testMetrics struct {
	mu        sync.Mutex
	gauges    map[string]float64
	durations int
}

func (m *testMetrics) IncCounter(string, []string)          {}
func (m *testMetrics) AddCounter(string, float64, []string) {}
func (m *testMetrics) ObserveDuration(string, float64, []string) {
	m.mu.Lock()
	m.durations++
	m.mu.Unlock()
}
func (m *testMetrics) AddGauge(string, float64, []string) {}
func (m *testMetrics) SetGauge(name string, value float64, _ []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gauges == nil {
		m.gauges = make(map[string]float64)
	}
	m.gauges[name] = value
}
func (m *testMetrics) RegisterMetric(string, string, []string) error { return nil }

type testTrace struct {
	names []string
	errs  []error
}

func (t *testTrace) Start(ctx context.Context, name string, _ ...filex.TraceAttr) (context.Context, func(error)) {
	t.names = append(t.names, name)
	index := len(t.names) - 1
	return ctx, func(err error) {
		for len(t.errs) <= index {
			t.errs = append(t.errs, nil)
		}
		t.errs[index] = err
	}
}
