package filex

import (
	"context"
	"strings"
	"testing"

	"github.com/lcylpzls/errx"
)

func TestStoreClosed(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{})
	_, _ = s.SetBucketVersioning(ctx, "abc", true)
	up, _ := s.InitiateMultipartUpload(ctx, "abc", "mk", PutOptions{})
	_, _ = s.UploadPart(ctx, "abc", "mk", up.UploadID, 1, strings.NewReader("p"))
	vi, _ := s.Put(ctx, "abc", "vk", strings.NewReader("v"), PutOptions{})
	_ = s.Delete(ctx, "abc", "vk")

	if err := s.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("重复关闭应幂等：%v", err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"CreateBucket", func() error { _, err := s.CreateBucket(ctx, "x"); return err }},
		{"DeleteBucket", func() error { return s.DeleteBucket(ctx, "abc") }},
		{"HeadBucket", func() error { _, err := s.HeadBucket(ctx, "abc"); return err }},
		{"ListBuckets", func() error { _, err := s.ListBuckets(ctx); return err }},
		{"SetBucketVersioning", func() error { _, err := s.SetBucketVersioning(ctx, "abc", true); return err }},
		{"SetBucketQuota", func() error { _, err := s.SetBucketQuota(ctx, "abc", 1); return err }},
		{"SetBucketLifecycle", func() error { _, err := s.SetBucketLifecycle(ctx, "abc", LifecycleOptions{}); return err }},
		{"Put", func() error { _, err := s.Put(ctx, "abc", "k", strings.NewReader("v"), PutOptions{}); return err }},
		{"Get", func() error { _, err := s.Get(ctx, "abc", "k", GetOptions{}); return err }},
		{"Head", func() error { _, err := s.Head(ctx, "abc", "k"); return err }},
		{"Delete", func() error { return s.Delete(ctx, "abc", "k") }},
		{"List", func() error { _, err := s.List(ctx, "abc", ListOptions{}); return err }},
		{"Copy", func() error { _, err := s.Copy(ctx, "abc", "k", "abc", "k2"); return err }},
		{"Move", func() error { _, err := s.Move(ctx, "abc", "k", "abc", "k2"); return err }},
		{"InitiateMultipartUpload", func() error { _, err := s.InitiateMultipartUpload(ctx, "abc", "x", PutOptions{}); return err }},
		{"UploadPart", func() error { _, err := s.UploadPart(ctx, "abc", "x", "u", 1, strings.NewReader("v")); return err }},
		{"CompleteMultipartUpload", func() error { _, err := s.CompleteMultipartUpload(ctx, "abc", "x", "u"); return err }},
		{"AbortMultipartUpload", func() error { return s.AbortMultipartUpload(ctx, "abc", "x", "u") }},
		{"ListParts", func() error { _, err := s.ListParts(ctx, "abc", "x", "u"); return err }},
		{"GetVersion", func() error { _, err := s.GetVersion(ctx, "abc", "vk", vi.VersionID, GetOptions{}); return err }},
		{"HeadVersion", func() error { _, err := s.HeadVersion(ctx, "abc", "vk", vi.VersionID); return err }},
		{"DeleteVersion", func() error { return s.DeleteVersion(ctx, "abc", "vk", vi.VersionID) }},
		{"RestoreVersion", func() error { _, err := s.RestoreVersion(ctx, "abc", "vk", vi.VersionID); return err }},
		{"ListVersions", func() error { _, err := s.ListVersions(ctx, "abc", "vk"); return err }},
		{"VerifyObject", func() error { return s.VerifyObject(ctx, "abc", "k") }},
		{"VerifyAll", func() error { _, err := s.VerifyAll(ctx, 2); return err }},
		{"BucketUsage", func() error { _, err := s.BucketUsage(ctx, "abc"); return err }},
		{"BucketStats", func() error { _, err := s.BucketStats(ctx, "abc"); return err }},
		{"RunLifecycle", func() error { _, err := s.RunLifecycle(ctx, "abc"); return err }},
		{"SweepOrphans", func() error { _, err := s.SweepOrphans(ctx); return err }},
		{"Health", func() error { return s.Health(ctx) }},
	}
	for _, c := range cases {
		if err := c.run(); err == nil {
			t.Errorf("%s 关闭后应报错", c.name)
		} else if !errx.Is(err, CodeClosed) {
			t.Errorf("%s 关闭后错误码不符：%v", c.name, err)
		}
	}
}
