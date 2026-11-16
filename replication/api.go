// Package replication 在源端 filex 与专用灾备目标端之间执行异步对象镜像。
package replication

import (
	"context"
	"io"
	"time"

	"github.com/lcylpzls/filex"
)

// Source 是复制源端所需的最小接口，*filex.Store 与 *client.Client 均满足。
type Source interface {
	GetBucket(ctx context.Context, name string) (filex.BucketInfo, error)
	ListAllVersions(ctx context.Context, bucket string) ([]filex.ObjectInfo, error)
	Get(ctx context.Context, bucket, key string, opts filex.GetOptions) (*filex.Object, error)
	GetVersion(ctx context.Context, bucket, key, versionID string, opts filex.GetOptions) (*filex.Object, error)
}

// Target 是复制目标端所需的最小接口，目标桶必须是专用灾备副本。
type Target interface {
	GetBucket(ctx context.Context, name string) (filex.BucketInfo, error)
	CreateBucket(ctx context.Context, name string) (filex.BucketInfo, error)
	SetBucketVersioning(ctx context.Context, name string, enabled bool) (filex.BucketInfo, error)
	ListAllVersions(ctx context.Context, bucket string) ([]filex.ObjectInfo, error)
	PutReplica(ctx context.Context, bucket, key string, r io.Reader,
		opts filex.ReplicaOptions) (filex.ObjectInfo, error)
	Delete(ctx context.Context, bucket, key string) error
	DeleteVersion(ctx context.Context, bucket, key, versionID string) error
}

// Config 是复制引擎配置。
type Config struct {
	// Source 和 Target 必填；目标桶中不属于源端的对象会被删除。
	Source Source
	Target Target
	// StateDir 持久化任务状态，必填且应与源端数据目录分开。
	StateDir string
	// Buckets 是要镜像的源端桶名，至少一项。
	Buckets []string
	// Interval 是 Run 的轮询间隔，默认 30 秒。
	Interval time.Duration
	// RetryBase 和 RetryMax 控制失败任务的指数退避，默认 2 秒到 5 分钟。
	RetryBase time.Duration
	RetryMax  time.Duration
	// Logger、Metrics、TraceHook 均为可选可观测接口。
	Logger    filex.Logger
	Metrics   filex.Metrics
	TraceHook filex.TraceHook
}

// TaskAction 是任务的期望动作。
type TaskAction string

const (
	// ActionUpsert 表示写入或校验对象副本。
	ActionUpsert TaskAction = "upsert"
	// ActionDelete 表示删除目标端多余对象或版本。
	ActionDelete TaskAction = "delete"
)

// TaskStatus 是持久化任务状态。
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskRunning    TaskStatus = "running"
	TaskReplicated TaskStatus = "replicated"
	TaskFailed     TaskStatus = "failed"
)

// Task 是单个对象或版本的可持久化复制任务。
type Task struct {
	ID             string           `json:"id"`
	Bucket         string           `json:"bucket"`
	Key            string           `json:"key"`
	VersionID      string           `json:"version_id,omitempty"`
	Action         TaskAction       `json:"action"`
	Status         TaskStatus       `json:"status"`
	Expected       filex.ObjectInfo `json:"expected"`
	Attempts       int              `json:"attempts"`
	NextAttemptAt  time.Time        `json:"next_attempt_at,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
	LastTransition time.Time        `json:"last_transition"`
}

// Report 是一次镜像运行的结果。
type Report struct {
	Buckets       int
	Scanned       int
	Copied        int
	Deleted       int
	Skipped       int
	Failed        int
	LastSuccessAt time.Time
}

// Stats 是任务状态统计。
type Stats struct {
	Pending        int
	Running        int
	Replicated     int
	Failed         int
	LastSuccessAt  time.Time
	ReplicationLag time.Duration
}
