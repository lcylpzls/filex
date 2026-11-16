package replication

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	"github.com/lcylpzls/logx"
)

const (
	defaultInterval  = 30 * time.Second
	defaultRetryBase = 2 * time.Second
	defaultRetryMax  = 5 * time.Minute
)

// Replicator 执行源端到专用灾备目标端的状态对账与复制。
type Replicator struct {
	cfg   Config
	store *taskStore
	mu    sync.Mutex
	tasks map[string]Task
}

// New 创建复制引擎；启动时会把遗留 running 任务恢复为 pending。
func New(cfg Config) (*Replicator, error) {
	if cfg.Source == nil {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "复制源不能为空")
	}
	if cfg.Target == nil {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "复制目标不能为空")
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "复制状态目录不能为空")
	}
	cfg.Buckets = uniqueBuckets(cfg.Buckets)
	if len(cfg.Buckets) == 0 {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "至少配置一个复制桶")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = defaultRetryBase
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = defaultRetryMax
	}
	if cfg.RetryMax < cfg.RetryBase {
		return nil, errx.NewCode(filex.CodeInvalidConfig, "最大退避不能小于基础退避")
	}
	store, err := newTaskStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	tasks, err := store.load()
	if err != nil {
		return nil, err
	}
	return &Replicator{cfg: cfg, store: store, tasks: tasks}, nil
}

// Mirror 执行一次完整对账；目标桶中不属于源端的对象和版本会被删除。
func (r *Replicator) Mirror(ctx context.Context) (Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mirrorLocked(ctx)
}

// Run 按 Interval 周期执行 Mirror，直到 ctx 结束；调用方负责决定是否异步运行。
func (r *Replicator) Run(ctx context.Context) error {
	for {
		if _, err := r.Mirror(ctx); err != nil && ctx.Err() == nil {
			r.logWarn("对象复制失败",
				logx.String("error", err.Error()),
			)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.cfg.Interval):
		}
	}
}

// Replay 重置失败任务并立即执行一次完整对账。
func (r *Replicator) Replay(ctx context.Context) (Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	for id, task := range r.tasks {
		if task.Status != TaskFailed {
			continue
		}
		task.Status = TaskPending
		task.Attempts = 0
		task.NextAttemptAt = time.Time{}
		task.LastError = ""
		task.LastTransition = now
		if err := r.store.save(task); err != nil {
			return Report{}, err
		}
		r.tasks[id] = task
	}
	return r.mirrorLocked(ctx)
}

// Tasks 返回任务快照，按桶、键和版本排序。
func (r *Replicator) Tasks(ctx context.Context) ([]Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		task.Expected.Metadata = maps.Clone(task.Expected.Metadata)
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bucket != out[j].Bucket {
			return out[i].Bucket < out[j].Bucket
		}
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].VersionID < out[j].VersionID
	})
	return out, nil
}

// Stats 返回任务统计与最近成功时间。
func (r *Replicator) Stats(ctx context.Context) (Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}
	return r.statsLocked(time.Now().UTC()), nil
}

func (r *Replicator) mirrorLocked(ctx context.Context) (Report, error) {
	start := time.Now()
	report := Report{Buckets: len(r.cfg.Buckets)}
	var errs []error
	for _, bucket := range r.cfg.Buckets {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := r.mirrorBucket(ctx, bucket, &report); err != nil {
			errs = append(errs, err)
			r.logWarn("复制桶失败",
				logx.String("bucket", bucket),
				logx.String("error", err.Error()),
			)
		}
	}
	stats := r.statsLocked(time.Now().UTC())
	report.Failed = stats.Failed
	report.LastSuccessAt = stats.LastSuccessAt
	r.emitMetrics(stats, time.Since(start))
	return report, errors.Join(errs...)
}

func (r *Replicator) mirrorBucket(ctx context.Context, bucket string, report *Report) error {
	sourceBucket, err := r.cfg.Source.GetBucket(ctx, bucket)
	if err != nil {
		return fmt.Errorf("读取源桶 %s 失败：%w", bucket, err)
	}
	targetBucket, err := r.cfg.Target.GetBucket(ctx, bucket)
	if errx.Is(err, filex.CodeBucketNotFound) {
		targetBucket, err = r.cfg.Target.CreateBucket(ctx, bucket)
		if errx.Is(err, filex.CodeBucketExists) {
			targetBucket, err = r.cfg.Target.GetBucket(ctx, bucket)
		}
	}
	if err != nil {
		return fmt.Errorf("准备目标桶 %s 失败：%w", bucket, err)
	}
	if targetBucket.Versioning != sourceBucket.Versioning {
		if _, err := r.cfg.Target.SetBucketVersioning(ctx, bucket, sourceBucket.Versioning); err != nil {
			return fmt.Errorf("设置目标桶 %s 版本化失败：%w", bucket, err)
		}
	}
	sourceObjects, err := r.cfg.Source.ListAllVersions(ctx, bucket)
	if err != nil {
		return fmt.Errorf("枚举源桶 %s 失败：%w", bucket, err)
	}
	targetObjects, err := r.cfg.Target.ListAllVersions(ctx, bucket)
	if err != nil {
		return fmt.Errorf("枚举目标桶 %s 失败：%w", bucket, err)
	}
	sourceMap := objectMap(sourceObjects)
	targetMap := objectMap(targetObjects)
	seen := make(map[string]struct{}, len(sourceObjects)+len(targetObjects))
	for _, source := range sourceObjects {
		report.Scanned++
		id := taskID(source.Bucket, source.Key, source.VersionID)
		seen[id] = struct{}{}
		target, ok := targetMap[id]
		if ok && sameObject(source, target) {
			report.Skipped++
			if err := r.markReplicated(id); err != nil {
				return err
			}
			continue
		}
		if err := r.upsertTask(Task{
			Bucket:    source.Bucket,
			Key:       source.Key,
			VersionID: source.VersionID,
			Action:    ActionUpsert,
			Expected:  source,
		}); err != nil {
			return err
		}
	}
	for id, target := range targetMap {
		if _, ok := sourceMap[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := r.upsertTask(Task{
			Bucket:    target.Bucket,
			Key:       target.Key,
			VersionID: target.VersionID,
			Action:    ActionDelete,
			Expected:  target,
		}); err != nil {
			return err
		}
	}
	if err := r.purgeUnseen(bucket, seen); err != nil {
		return err
	}
	return r.processBucketTasks(ctx, bucket, report)
}

func (r *Replicator) upsertTask(desired Task) error {
	id := taskID(desired.Bucket, desired.Key, desired.VersionID)
	desired.ID = id
	if existing, ok := r.tasks[id]; ok && sameDesired(existing, desired) &&
		existing.Status != TaskReplicated {
		return nil
	}
	now := time.Now().UTC()
	desired.Status = TaskPending
	desired.Attempts = 0
	desired.NextAttemptAt = time.Time{}
	desired.LastError = ""
	desired.LastTransition = now
	if err := r.store.save(desired); err != nil {
		return err
	}
	r.tasks[id] = desired
	return nil
}

func (r *Replicator) markReplicated(id string) error {
	task, ok := r.tasks[id]
	if !ok || task.Status == TaskReplicated {
		return nil
	}
	task.Status = TaskReplicated
	task.Attempts = 0
	task.NextAttemptAt = time.Time{}
	task.LastError = ""
	task.LastTransition = time.Now().UTC()
	if err := r.store.save(task); err != nil {
		return err
	}
	r.tasks[id] = task
	return nil
}

func (r *Replicator) purgeUnseen(bucket string, seen map[string]struct{}) error {
	for id, task := range r.tasks {
		if task.Bucket != bucket {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if err := r.store.remove(id); err != nil {
			return err
		}
		delete(r.tasks, id)
	}
	return nil
}

func (r *Replicator) processBucketTasks(ctx context.Context, bucket string, report *Report) error {
	tasks := make([]Task, 0)
	for _, task := range r.tasks {
		if task.Bucket == bucket && task.Status != TaskReplicated {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Key != tasks[j].Key {
			return tasks[i].Key < tasks[j].Key
		}
		return tasks[i].VersionID < tasks[j].VersionID
	})
	var errs []error
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if task.NextAttemptAt.After(time.Now().UTC()) {
			continue
		}
		copied, err := r.processTask(ctx, task)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if copied {
			if task.Action == ActionDelete {
				report.Deleted++
			} else {
				report.Copied++
			}
		}
	}
	return errors.Join(errs...)
}

func (r *Replicator) processTask(ctx context.Context, task Task) (bool, error) {
	ctx, endTrace := r.startTrace(ctx, task)
	var traceErr error
	defer func() { endTrace(traceErr) }()
	task.Status = TaskRunning
	task.LastTransition = time.Now().UTC()
	if err := r.store.save(task); err != nil {
		traceErr = err
		return false, err
	}
	r.tasks[task.ID] = task
	err := r.applyTask(ctx, task)
	if err == nil {
		task.Status = TaskReplicated
		task.Attempts = 0
		task.NextAttemptAt = time.Time{}
		task.LastError = ""
		task.LastTransition = time.Now().UTC()
		if saveErr := r.store.save(task); saveErr != nil {
			traceErr = saveErr
			return false, saveErr
		}
		r.tasks[task.ID] = task
		return true, nil
	}
	traceErr = err
	if isCancelled(ctx, err) {
		task.Status = TaskPending
		task.LastTransition = time.Now().UTC()
	} else {
		task.Status = TaskFailed
		task.Attempts++
		task.NextAttemptAt = time.Now().UTC().Add(r.backoff(task.Attempts))
		task.LastTransition = time.Now().UTC()
	}
	task.LastError = err.Error()
	if saveErr := r.store.save(task); saveErr != nil {
		return false, errors.Join(err, saveErr)
	}
	r.tasks[task.ID] = task
	r.logWarn("复制任务失败",
		logx.String("bucket", task.Bucket),
		logx.String("key", task.Key),
		logx.String("version_id", task.VersionID),
		logx.String("error", err.Error()),
	)
	return false, err
}

func (r *Replicator) applyTask(ctx context.Context, task Task) error {
	switch task.Action {
	case ActionDelete:
		if task.VersionID == "" {
			if err := r.cfg.Target.Delete(ctx, task.Bucket, task.Key); err != nil &&
				!errx.Is(err, filex.CodeObjectNotFound) {
				return err
			}
			return nil
		}
		if err := r.cfg.Target.DeleteVersion(ctx, task.Bucket, task.Key, task.VersionID); err != nil &&
			!errx.Is(err, filex.CodeVersionNotFound) {
			return err
		}
		return nil
	case ActionUpsert:
		opts := replicaOptions(task.Expected)
		if task.Expected.Deleted {
			_, err := r.cfg.Target.PutReplica(ctx, task.Bucket, task.Key, nil, opts)
			return err
		}
		var (
			object *filex.Object
			err    error
		)
		if task.VersionID == "" {
			object, err = r.cfg.Source.Get(ctx, task.Bucket, task.Key, filex.GetOptions{Verify: true})
		} else {
			object, err = r.cfg.Source.GetVersion(ctx, task.Bucket, task.Key, task.VersionID,
				filex.GetOptions{Verify: true})
		}
		if err != nil {
			return err
		}
		defer object.Close()
		if !sameObject(object.Info, task.Expected) {
			return fmt.Errorf("源端对象在复制期间发生变化")
		}
		info, err := r.cfg.Target.PutReplica(
			ctx,
			task.Bucket,
			task.Key,
			object,
			opts,
		)
		if err != nil {
			return err
		}
		if !sameObject(info, task.Expected) {
			return fmt.Errorf("目标端副本校验不一致")
		}
		return nil
	default:
		return fmt.Errorf("未知复制任务动作：%s", task.Action)
	}
}

func (r *Replicator) startTrace(ctx context.Context, task Task) (context.Context, func(error)) {
	if r.cfg.TraceHook == nil {
		return ctx, func(error) {}
	}
	attrs := []filex.TraceAttr{
		{Key: "filex.replication.bucket", Value: task.Bucket},
		{Key: "filex.replication.key", Value: task.Key},
		{Key: "filex.replication.version_id", Value: task.VersionID},
		{Key: "filex.replication.action", Value: string(task.Action)},
	}
	return r.cfg.TraceHook.Start(ctx, "filex.replication", attrs...)
}

func (r *Replicator) backoff(attempts int) time.Duration {
	delay := r.cfg.RetryBase
	for i := 1; i < attempts && delay < r.cfg.RetryMax; i++ {
		if delay > r.cfg.RetryMax/2 {
			return r.cfg.RetryMax
		}
		delay *= 2
	}
	if delay > r.cfg.RetryMax {
		return r.cfg.RetryMax
	}
	return delay
}

func (r *Replicator) statsLocked(now time.Time) Stats {
	stats := Stats{}
	for _, task := range r.tasks {
		switch task.Status {
		case TaskPending:
			stats.Pending++
		case TaskRunning:
			stats.Running++
		case TaskReplicated:
			stats.Replicated++
			if task.LastTransition.After(stats.LastSuccessAt) {
				stats.LastSuccessAt = task.LastTransition
			}
		case TaskFailed:
			stats.Failed++
		}
	}
	if !stats.LastSuccessAt.IsZero() && now.After(stats.LastSuccessAt) {
		stats.ReplicationLag = now.Sub(stats.LastSuccessAt)
	}
	return stats
}

func (r *Replicator) emitMetrics(stats Stats, elapsed time.Duration) {
	if r.cfg.Metrics == nil {
		return
	}
	labels := []string{"direction", "outbound"}
	r.cfg.Metrics.SetGauge("filex.replication.pending", float64(stats.Pending), labels)
	r.cfg.Metrics.SetGauge("filex.replication.failed", float64(stats.Failed), labels)
	r.cfg.Metrics.SetGauge("filex.replication.lag_seconds", stats.ReplicationLag.Seconds(), labels)
	if !stats.LastSuccessAt.IsZero() {
		r.cfg.Metrics.SetGauge("filex.replication.last_success", float64(stats.LastSuccessAt.Unix()), labels)
	}
	r.cfg.Metrics.ObserveDuration("filex.replication.duration", elapsed.Seconds(), labels)
}

func (r *Replicator) logWarn(message string, fields ...logx.Field) {
	if r.cfg.Logger != nil {
		r.cfg.Logger.Warn(message, logx.Fields(fields...))
	}
}

func uniqueBuckets(buckets []string) []string {
	seen := make(map[string]struct{}, len(buckets))
	out := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		if _, ok := seen[bucket]; ok {
			continue
		}
		seen[bucket] = struct{}{}
		out = append(out, bucket)
	}
	return out
}

func objectMap(objects []filex.ObjectInfo) map[string]filex.ObjectInfo {
	out := make(map[string]filex.ObjectInfo, len(objects))
	for _, object := range objects {
		out[taskID(object.Bucket, object.Key, object.VersionID)] = object
	}
	return out
}

func sameDesired(a, b Task) bool {
	return a.Action == b.Action &&
		a.Bucket == b.Bucket &&
		a.Key == b.Key &&
		a.VersionID == b.VersionID &&
		sameObject(a.Expected, b.Expected)
}

func sameObject(a, b filex.ObjectInfo) bool {
	return a.Bucket == b.Bucket &&
		a.Key == b.Key &&
		a.VersionID == b.VersionID &&
		a.Size == b.Size &&
		strings.EqualFold(a.ETag, b.ETag) &&
		normalizedContentType(a.ContentType) == normalizedContentType(b.ContentType) &&
		maps.Equal(a.Metadata, b.Metadata) &&
		a.Deleted == b.Deleted &&
		a.CreatedAt.Equal(b.CreatedAt) &&
		a.UpdatedAt.Equal(b.UpdatedAt)
}

func normalizedContentType(contentType string) string {
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func replicaOptions(info filex.ObjectInfo) filex.ReplicaOptions {
	return filex.ReplicaOptions{
		VersionID:   info.VersionID,
		SHA256:      info.ETag,
		ContentType: info.ContentType,
		Metadata:    info.Metadata,
		CreatedAt:   info.CreatedAt,
		UpdatedAt:   info.UpdatedAt,
		Deleted:     info.Deleted,
	}
}

func isCancelled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errx.Is(err, filex.CodeCancelled)
}
