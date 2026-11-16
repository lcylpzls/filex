package replication

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	testx "github.com/lcylpzls/testx"
)

func TestNewTaskStorageErrors(t *testing.T) {
	source, target := newStorePair(t)
	stateFile := filepath.Join(t.TempDir(), "state")
	testx.RequireNoError(t, os.WriteFile(stateFile, []byte("x"), 0o644))
	if _, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: stateFile,
		Buckets:  []string{"center"},
	}); err == nil {
		t.Fatal("状态目录为文件应报错")
	}

	base := taskFS
	t.Cleanup(func() { taskFS = base })
	taskFS.ReadDir = func(string) ([]os.DirEntry, error) {
		return nil, errors.New("读取任务目录失败")
	}
	if _, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	}); err == nil {
		t.Fatal("任务加载失败应返回错误")
	}
}

func TestRunLogsAndContinuesUntilCancel(t *testing.T) {
	source, target := newStorePair(t)
	replicator, err := New(Config{
		Source:   &hookSource{store: source, getBucketFn: func() (filex.BucketInfo, error) { return filex.BucketInfo{}, errors.New("源端不可用") }},
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
		Interval: time.Millisecond,
		Logger:   &testLogger{},
	})
	testx.RequireNoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	testx.RequireNoError(t, replicator.Run(ctx))
}

func TestReplaySkipsCompletedAndSurfacesSaveError(t *testing.T) {
	source, target := newStorePair(t)
	replicator, err := New(Config{
		Source:   &hookSource{store: source, getBucketFn: func() (filex.BucketInfo, error) { return filex.BucketInfo{}, errors.New("停止镜像") }},
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	completed := Task{ID: taskID("center", "done", ""), Bucket: "center", Key: "done", Status: TaskReplicated}
	replicator.tasks[completed.ID] = completed
	if _, err := replicator.Replay(context.Background()); err == nil {
		t.Fatal("镜像错误应返回")
	}

	failed := Task{ID: taskID("center", "failed", ""), Bucket: "center", Key: "failed", Status: TaskFailed}
	replicator.tasks[failed.ID] = failed
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, errors.New("保存失败") }
	if _, err := replicator.Replay(context.Background()); err == nil {
		t.Fatal("重放保存失败应返回错误")
	}
}

func TestTasksSortAndStatsRunning(t *testing.T) {
	source, target := newStorePair(t)
	replicator, err := New(Config{Source: source, Target: target, StateDir: t.TempDir(), Buckets: []string{"center"}})
	testx.RequireNoError(t, err)
	values := []Task{
		{Bucket: "b", Key: "a", VersionID: "1", Status: TaskPending},
		{Bucket: "a", Key: "b", Status: TaskRunning},
		{Bucket: "a", Key: "a", VersionID: "2", Status: TaskReplicated, LastTransition: time.Now().UTC()},
		{Bucket: "a", Key: "a", VersionID: "1", Status: TaskFailed},
	}
	for _, task := range values {
		task.ID = taskID(task.Bucket, task.Key, task.VersionID)
		replicator.tasks[task.ID] = task
	}
	tasks, err := replicator.Tasks(context.Background())
	testx.RequireNoError(t, err)
	if len(tasks) != 4 || tasks[0].Bucket != "a" || tasks[0].Key != "a" {
		t.Fatalf("任务排序不符：%+v", tasks)
	}
	stats, err := replicator.Stats(context.Background())
	testx.RequireNoError(t, err)
	if stats.Pending != 1 || stats.Running != 1 || stats.Replicated != 1 || stats.Failed != 1 {
		t.Fatalf("任务统计不符：%+v", stats)
	}
}

func TestMirrorBucketPreparationErrors(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	injected := errors.New("注入错误")
	t.Run("源桶读取", func(t *testing.T) {
		source, target := newStorePair(t)
		replicator := newTestReplicator(t, &hookSource{
			store:       source,
			getBucketFn: func() (filex.BucketInfo, error) { return filex.BucketInfo{}, injected },
		}, target)
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("源桶错误未返回：%v", err)
		}
	})
	t.Run("目标桶读取", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		replicator := newTestReplicator(t, source, &hookTarget{
			store:       target,
			getBucketFn: func() (filex.BucketInfo, error) { return filex.BucketInfo{}, injected },
		})
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("目标桶错误未返回：%v", err)
		}
	})
	t.Run("创建竞态", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		getCalls := 0
		replicator := newTestReplicator(t, source, &hookTarget{
			store: target,
			getBucketFn: func() (filex.BucketInfo, error) {
				getCalls++
				if getCalls == 1 {
					return filex.BucketInfo{}, errx.NewCode(filex.CodeBucketNotFound, "不存在")
				}
				return target.GetBucket(context.Background(), "center")
			},
			createBucketFn: func() (filex.BucketInfo, error) {
				return filex.BucketInfo{}, errx.NewCode(filex.CodeBucketExists, "已存在")
			},
		})
		_, err := replicator.Mirror(context.Background())
		testx.RequireNoError(t, err)
	})
	t.Run("创建失败", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		replicator := newTestReplicator(t, source, &hookTarget{
			store: target,
			getBucketFn: func() (filex.BucketInfo, error) {
				return filex.BucketInfo{}, errx.NewCode(filex.CodeBucketNotFound, "不存在")
			},
			createBucketFn: func() (filex.BucketInfo, error) {
				return filex.BucketInfo{}, injected
			},
		})
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("创建错误未返回：%v", err)
		}
	})
	t.Run("版本化配置", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		_, _ = source.SetBucketVersioning(context.Background(), "center", true)
		replicator := newTestReplicator(t, source, &hookTarget{
			store: target,
			setVersioningFn: func() (filex.BucketInfo, error) {
				return filex.BucketInfo{}, injected
			},
		})
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("版本化错误未返回：%v", err)
		}
	})
	t.Run("源列表", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		replicator := newTestReplicator(t, &hookSource{
			store:     source,
			listAllFn: func() ([]filex.ObjectInfo, error) { return nil, injected },
		}, target)
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("源列表错误未返回：%v", err)
		}
	})
	t.Run("目标列表", func(t *testing.T) {
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		replicator := newTestReplicator(t, source, &hookTarget{
			store:     target,
			listAllFn: func() ([]filex.ObjectInfo, error) { return nil, injected },
		})
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("目标列表错误未返回：%v", err)
		}
	})
}

func TestMirrorTaskPersistenceErrors(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	injected := errors.New("注入错误")
	t.Run("标记完成", func(t *testing.T) {
		source, target := newStorePair(t)
		ctx := context.Background()
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		sourceInfo, err := source.Put(ctx, "center", "key", strings.NewReader("value"), filex.PutOptions{})
		testx.RequireNoError(t, err)
		_, err = target.PutReplica(ctx, "center", "key", strings.NewReader("value"), filex.ReplicaOptions{
			SHA256:    sourceInfo.ETag,
			CreatedAt: sourceInfo.CreatedAt,
			UpdatedAt: sourceInfo.UpdatedAt,
		})
		testx.RequireNoError(t, err)
		replicator := newTestReplicator(t, source, target)
		task := Task{
			ID:       taskID("center", "key", ""),
			Bucket:   "center",
			Key:      "key",
			Action:   ActionUpsert,
			Status:   TaskPending,
			Expected: sourceInfo,
		}
		replicator.tasks[task.ID] = task
		taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
		if _, err := replicator.Mirror(ctx); !errors.Is(err, injected) {
			t.Fatalf("标记完成保存错误未返回：%v", err)
		}
	})
	t.Run("源任务保存", func(t *testing.T) {
		taskFS = base
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		_, _ = source.Put(context.Background(), "center", "key", strings.NewReader("value"), filex.PutOptions{})
		replicator := newTestReplicator(t, source, target)
		taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("源任务保存错误未返回：%v", err)
		}
	})
	t.Run("标记完成成功", func(t *testing.T) {
		taskFS = base
		source, target := newStorePair(t)
		ctx := context.Background()
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		sourceInfo, err := source.Put(ctx, "center", "key", strings.NewReader("value"), filex.PutOptions{})
		testx.RequireNoError(t, err)
		_, err = target.PutReplica(ctx, "center", "key", strings.NewReader("value"), filex.ReplicaOptions{
			SHA256:    sourceInfo.ETag,
			CreatedAt: sourceInfo.CreatedAt,
			UpdatedAt: sourceInfo.UpdatedAt,
		})
		testx.RequireNoError(t, err)
		replicator := newTestReplicator(t, source, target)
		task := Task{
			ID:       taskID("center", "key", ""),
			Bucket:   "center",
			Key:      "key",
			Action:   ActionUpsert,
			Status:   TaskPending,
			Expected: sourceInfo,
		}
		replicator.tasks[task.ID] = task
		_, err = replicator.Mirror(ctx)
		testx.RequireNoError(t, err)
		if replicator.tasks[task.ID].Status != TaskReplicated {
			t.Fatalf("任务未标记完成：%+v", replicator.tasks[task.ID])
		}
	})
	t.Run("删除任务保存", func(t *testing.T) {
		taskFS = base
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		_, _ = target.Put(context.Background(), "center", "extra", strings.NewReader("value"), filex.PutOptions{})
		replicator := newTestReplicator(t, source, target)
		taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("删除任务保存错误未返回：%v", err)
		}
	})
	t.Run("清理任务", func(t *testing.T) {
		taskFS = base
		source, target := newStorePair(t)
		mustBucket(t, source, "center")
		mustBucket(t, target, "center")
		replicator := newTestReplicator(t, source, target)
		replicator.tasks["stale"] = Task{ID: "stale", Bucket: "center"}
		replicator.tasks["other"] = Task{ID: "other", Bucket: "other"}
		taskFS.Remove = func(string) error { return injected }
		if _, err := replicator.Mirror(context.Background()); !errors.Is(err, injected) {
			t.Fatalf("清理任务错误未返回：%v", err)
		}
		other := newTestReplicator(t, source, target)
		other.tasks["other"] = Task{ID: "other", Bucket: "other"}
		if err := other.purgeUnseen("center", nil); err != nil {
			t.Fatalf("其他桶任务应保留：%v", err)
		}
	})
}

func TestProcessTaskPersistenceAndCancellation(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	source, target := newStorePair(t)
	ctx := context.Background()
	mustBucket(t, source, "center")
	mustBucket(t, target, "center")
	task := Task{
		ID:       taskID("center", "missing", ""),
		Bucket:   "center",
		Key:      "missing",
		Action:   ActionDelete,
		Status:   TaskPending,
		Expected: filex.ObjectInfo{Bucket: "center", Key: "missing"},
	}
	injected := errors.New("注入错误")

	replicator := newTestReplicator(t, source, target)
	replicator.tasks[task.ID] = task
	taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
	if _, err := replicator.processTask(ctx, task); !errors.Is(err, injected) {
		t.Fatalf("running 保存错误未返回：%v", err)
	}

	taskFS = base
	calls := 0
	taskFS.CreateTemp = func(dir, pattern string) (taskFile, error) {
		calls++
		if calls == 2 {
			return nil, injected
		}
		return base.CreateTemp(dir, pattern)
	}
	if _, err := replicator.processTask(ctx, task); !errors.Is(err, injected) {
		t.Fatalf("完成保存错误未返回：%v", err)
	}

	taskFS = base
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	replicator.cfg.Source = &hookSource{
		store: source,
		getFn: func() (*filex.Object, error) {
			return nil, errx.NewCode(filex.CodeCancelled, "已取消")
		},
	}
	upsert := task
	upsert.ID = taskID("center", "upsert", "")
	upsert.Key = "upsert"
	upsert.Action = ActionUpsert
	upsert.Expected = filex.ObjectInfo{Bucket: "center", Key: "upsert"}
	if _, err := replicator.processTask(cancelled, upsert); !errx.Is(err, filex.CodeCancelled) {
		t.Fatalf("取消错误码不符：%v", err)
	}

	taskFS.CreateTemp = func(dir, pattern string) (taskFile, error) {
		calls++
		if calls == 4 {
			return nil, injected
		}
		return base.CreateTemp(dir, pattern)
	}
	failing := &hookTarget{
		store: target,
		deleteFn: func() error {
			return injected
		},
	}
	replicator.cfg.Target = failing
	calls = 2
	if _, err := replicator.processTask(ctx, task); err == nil {
		t.Fatal("失败任务保存错误应返回")
	}
}

func TestProcessBucketTasksCancellation(t *testing.T) {
	source, target := newStorePair(t)
	replicator := newTestReplicator(t, source, target)
	task := Task{ID: taskID("center", "key", ""), Bucket: "center", Key: "key", Status: TaskPending}
	replicator.tasks[task.ID] = task
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := replicator.processBucketTasks(ctx, "center", &Report{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("处理任务应响应取消：%v", err)
	}
}

func TestApplyTaskErrors(t *testing.T) {
	source, target := newStorePair(t)
	ctx := context.Background()
	injected := errors.New("注入错误")
	mustBucket(t, target, "center")
	replicator := newTestReplicator(t, source, target)
	baseTask := Task{
		Bucket:   "center",
		Key:      "key",
		Action:   ActionUpsert,
		Expected: filex.ObjectInfo{Bucket: "center", Key: "key"},
	}
	cases := []struct {
		name string
		task Task
		src  Source
		tgt  Target
	}{
		{
			"删除对象失败",
			Task{Bucket: "center", Key: "key", Action: ActionDelete},
			source,
			&hookTarget{store: target, deleteFn: func() error { return injected }},
		},
		{
			"删除版本失败",
			Task{Bucket: "center", Key: "key", VersionID: "v", Action: ActionDelete},
			source,
			&hookTarget{store: target, deleteVersionFn: func() error { return injected }},
		},
		{
			"源读取失败",
			baseTask,
			&hookSource{store: source, getFn: func() (*filex.Object, error) { return nil, injected }},
			target,
		},
		{
			"源版本读取失败",
			Task{Bucket: "center", Key: "key", VersionID: "v", Action: ActionUpsert, Expected: filex.ObjectInfo{Bucket: "center", Key: "key", VersionID: "v"}},
			&hookSource{store: source, getVersionFn: func() (*filex.Object, error) { return nil, injected }},
			target,
		},
		{
			"源对象变化",
			baseTask,
			&hookSource{store: source, getFn: func() (*filex.Object, error) {
				return &filex.Object{Info: filex.ObjectInfo{Bucket: "center", Key: "changed"}, ReadCloser: io.NopCloser(strings.NewReader("x"))}, nil
			}},
			target,
		},
		{
			"目标写入失败",
			baseTask,
			&hookSource{store: source, getFn: func() (*filex.Object, error) {
				return &filex.Object{Info: baseTask.Expected, ReadCloser: io.NopCloser(strings.NewReader("x"))}, nil
			}},
			&hookTarget{store: target, putReplicaFn: func() (filex.ObjectInfo, error) { return filex.ObjectInfo{}, injected }},
		},
		{
			"目标结果不一致",
			baseTask,
			&hookSource{store: source, getFn: func() (*filex.Object, error) {
				return &filex.Object{Info: baseTask.Expected, ReadCloser: io.NopCloser(strings.NewReader("x"))}, nil
			}},
			&hookTarget{store: target, putReplicaFn: func() (filex.ObjectInfo, error) {
				return filex.ObjectInfo{Bucket: "center", Key: "wrong"}, nil
			}},
		},
		{
			"未知动作",
			Task{Bucket: "center", Key: "key", Action: "unknown"},
			source,
			target,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			replicator.cfg.Source = test.src
			replicator.cfg.Target = test.tgt
			if err := replicator.applyTask(ctx, test.task); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}

	replicator.cfg.Target = target
	if err := replicator.applyTask(ctx, Task{Bucket: "center", Key: "missing", Action: ActionDelete}); err != nil {
		t.Fatalf("删除不存在对象应幂等：%v", err)
	}
	if err := replicator.applyTask(ctx, Task{Bucket: "center", Key: "missing", VersionID: "v", Action: ActionDelete}); err != nil {
		t.Fatalf("删除不存在版本应幂等：%v", err)
	}
}

func TestBackoffAndNormalizedContentType(t *testing.T) {
	source, target := newStorePair(t)
	replicator := newTestReplicator(t, source, target)
	replicator.cfg.RetryBase = 2 * time.Second
	replicator.cfg.RetryMax = 5 * time.Second
	if got := replicator.backoff(1); got != 2*time.Second {
		t.Fatalf("首次退避不符：%s", got)
	}
	if got := replicator.backoff(2); got != 4*time.Second {
		t.Fatalf("二次退避不符：%s", got)
	}
	if got := replicator.backoff(3); got != 5*time.Second {
		t.Fatalf("封顶退避不符：%s", got)
	}
	replicator.cfg.RetryBase = 10 * time.Second
	replicator.cfg.RetryMax = 5 * time.Second
	if got := replicator.backoff(1); got != 5*time.Second {
		t.Fatalf("基础值超上限应封顶：%s", got)
	}
	if got := normalizedContentType(""); got != "application/octet-stream" {
		t.Fatalf("空内容类型归一不符：%s", got)
	}
	if got := normalizedContentType("text/plain"); got != "text/plain" {
		t.Fatalf("内容类型不应变化：%s", got)
	}
}

func newTestReplicator(t *testing.T, source Source, target Target) *Replicator {
	t.Helper()
	replicator, err := New(Config{
		Source:   source,
		Target:   target,
		StateDir: t.TempDir(),
		Buckets:  []string{"center"},
	})
	testx.RequireNoError(t, err)
	return replicator
}

type hookSource struct {
	store        *filex.Store
	getBucketFn  func() (filex.BucketInfo, error)
	listAllFn    func() ([]filex.ObjectInfo, error)
	getFn        func() (*filex.Object, error)
	getVersionFn func() (*filex.Object, error)
}

func (s *hookSource) GetBucket(ctx context.Context, name string) (filex.BucketInfo, error) {
	if s.getBucketFn != nil {
		return s.getBucketFn()
	}
	return s.store.GetBucket(ctx, name)
}

func (s *hookSource) ListAllVersions(ctx context.Context, bucket string) ([]filex.ObjectInfo, error) {
	if s.listAllFn != nil {
		return s.listAllFn()
	}
	return s.store.ListAllVersions(ctx, bucket)
}

func (s *hookSource) Get(ctx context.Context, bucket, key string, opts filex.GetOptions) (*filex.Object, error) {
	if s.getFn != nil {
		return s.getFn()
	}
	return s.store.Get(ctx, bucket, key, opts)
}

func (s *hookSource) GetVersion(ctx context.Context, bucket, key, versionID string,
	opts filex.GetOptions) (*filex.Object, error) {
	if s.getVersionFn != nil {
		return s.getVersionFn()
	}
	return s.store.GetVersion(ctx, bucket, key, versionID, opts)
}

type hookTarget struct {
	store           *filex.Store
	getBucketFn     func() (filex.BucketInfo, error)
	createBucketFn  func() (filex.BucketInfo, error)
	setVersioningFn func() (filex.BucketInfo, error)
	listAllFn       func() ([]filex.ObjectInfo, error)
	putReplicaFn    func() (filex.ObjectInfo, error)
	deleteFn        func() error
	deleteVersionFn func() error
}

func (t *hookTarget) GetBucket(ctx context.Context, name string) (filex.BucketInfo, error) {
	if t.getBucketFn != nil {
		return t.getBucketFn()
	}
	return t.store.GetBucket(ctx, name)
}

func (t *hookTarget) CreateBucket(ctx context.Context, name string) (filex.BucketInfo, error) {
	if t.createBucketFn != nil {
		return t.createBucketFn()
	}
	return t.store.CreateBucket(ctx, name)
}

func (t *hookTarget) SetBucketVersioning(ctx context.Context, name string,
	enabled bool) (filex.BucketInfo, error) {
	if t.setVersioningFn != nil {
		return t.setVersioningFn()
	}
	return t.store.SetBucketVersioning(ctx, name, enabled)
}

func (t *hookTarget) ListAllVersions(ctx context.Context, bucket string) ([]filex.ObjectInfo, error) {
	if t.listAllFn != nil {
		return t.listAllFn()
	}
	return t.store.ListAllVersions(ctx, bucket)
}

func (t *hookTarget) PutReplica(ctx context.Context, bucket, key string, reader io.Reader,
	opts filex.ReplicaOptions) (filex.ObjectInfo, error) {
	if t.putReplicaFn != nil {
		return t.putReplicaFn()
	}
	return t.store.PutReplica(ctx, bucket, key, reader, opts)
}

func (t *hookTarget) Delete(ctx context.Context, bucket, key string) error {
	if t.deleteFn != nil {
		return t.deleteFn()
	}
	return t.store.Delete(ctx, bucket, key)
}

func (t *hookTarget) DeleteVersion(ctx context.Context, bucket, key, versionID string) error {
	if t.deleteVersionFn != nil {
		return t.deleteVersionFn()
	}
	return t.store.DeleteVersion(ctx, bucket, key, versionID)
}
