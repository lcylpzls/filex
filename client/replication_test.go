package client

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lcylpzls/errx"
	"github.com/lcylpzls/filex"
	testx "github.com/lcylpzls/testx"
)

func TestClientReplicationProtocol(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "source")
	_, _ = c.CreateBucket(ctx, "target")
	_, _ = c.SetBucketVersioning(ctx, "source", true)
	_, _ = c.SetBucketVersioning(ctx, "target", true)

	source, err := c.Put(ctx, "source", "key", strings.NewReader("payload"), filex.PutOptions{
		ContentType: "text/plain",
		Metadata:    map[string]string{"source": "a"},
	})
	testx.RequireNoError(t, err)
	_ = c.Delete(ctx, "source", "key")
	versions, err := c.ListAllVersions(ctx, "source")
	testx.RequireNoError(t, err)
	if len(versions) != 2 || !versions[0].Deleted {
		t.Fatalf("源端全量版本不符：%+v", versions)
	}

	replica, err := c.PutReplica(ctx, "target", "key", strings.NewReader("payload"), filex.ReplicaOptions{
		VersionID:   source.VersionID,
		SHA256:      source.ETag,
		ContentType: source.ContentType,
		Metadata:    source.Metadata,
		CreatedAt:   source.CreatedAt,
		UpdatedAt:   source.UpdatedAt,
	})
	testx.RequireNoError(t, err)
	if replica.VersionID != source.VersionID || !replica.CreatedAt.Equal(source.CreatedAt) {
		t.Fatalf("目标副本元数据不符：%+v", replica)
	}
	head, err := c.HeadVersion(ctx, "target", "key", source.VersionID)
	testx.RequireNoError(t, err)
	if head.ETag != source.ETag || head.Metadata["source"] != "a" {
		t.Fatalf("目标副本校验失败：%+v", head)
	}

	marker := versions[0]
	replicatedMarker, err := c.PutReplica(ctx, "target", "key", nil, filex.ReplicaOptions{
		VersionID: marker.VersionID,
		SHA256:    marker.ETag,
		Deleted:   true,
		CreatedAt: marker.CreatedAt,
		UpdatedAt: marker.UpdatedAt,
	})
	testx.RequireNoError(t, err)
	if !replicatedMarker.Deleted {
		t.Fatalf("删除标记未复制：%+v", replicatedMarker)
	}
	targetVersions, err := c.ListAllVersions(ctx, "target")
	testx.RequireNoError(t, err)
	if len(targetVersions) != 2 {
		t.Fatalf("目标端全量版本不符：%+v", targetVersions)
	}

	_, err = c.PutReplica(ctx, "target", "key", strings.NewReader("changed"), filex.ReplicaOptions{
		VersionID: source.VersionID,
		SHA256:    sha256Hex("changed"),
		CreatedAt: source.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	})
	if !errx.Is(err, filex.CodePreconditionFailed) {
		t.Fatalf("同版本冲突错误码不符：%v", err)
	}
}

func TestClientListAllVersionsError(t *testing.T) {
	c, _ := newClientServer(t)
	if _, err := c.ListAllVersions(context.Background(), "missing"); err == nil {
		t.Fatal("缺失桶应报错")
	}
}

func TestReplicaContentReadable(t *testing.T) {
	c, _ := newClientServer(t)
	ctx := context.Background()
	_, _ = c.CreateBucket(ctx, "abc")
	info, err := c.PutReplica(ctx, "abc", "key", strings.NewReader("flat"), filex.ReplicaOptions{
		SHA256:    sha256Hex("flat"),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	testx.RequireNoError(t, err)
	object, err := c.Get(ctx, "abc", "key", filex.GetOptions{Verify: true})
	testx.RequireNoError(t, err)
	data, err := io.ReadAll(object)
	_ = object.Close()
	testx.RequireNoError(t, err)
	if string(data) != "flat" || info.VersionID != "" {
		t.Fatalf("非版本化副本不符：%q %+v", data, info)
	}
}
