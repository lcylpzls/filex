package core

import (
	"context"
	"errors"
	testx "github.com/lcylpzls/testx"
	"os"
	"strings"
	"testing"
)

func TestBucketStats(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	_, _ = s.Put(ctx, "abc", "a", strings.NewReader("hello"), PutOptions{})
	_, _ = s.Put(ctx, "abc", "b", strings.NewReader("world"), PutOptions{})
	stats, err := s.BucketStats(ctx, "abc")
	testx.RequireNoError(t, err)

	if stats.ObjectCount != 2 || stats.VersionCount != 2 || stats.Usage != 10 {
		t.Fatalf("非版本化统计不符：%+v", stats)
	}

	mustBucket(t, s, "vbc")
	_, _ = s.SetBucketVersioning(ctx, "vbc", true)
	_, _ = s.Put(ctx, "vbc", "a", strings.NewReader("aaaa"), PutOptions{})
	_, _ = s.Put(ctx, "vbc", "a", strings.NewReader("A"), PutOptions{})
	_, _ = s.Put(ctx, "vbc", "b", strings.NewReader("bb"), PutOptions{})
	_ = s.Delete(ctx, "vbc", "b")
	stats, err = s.BucketStats(ctx, "vbc")
	testx.RequireNoError(t, err)

	if stats.ObjectCount != 2 || stats.VersionCount != 3 || stats.Usage != 7 {
		t.Fatalf("版本化统计不符：%+v", stats)
	}
}

func TestBucketStatsErrors(t *testing.T) {
	s, _ := newStore(t)
	mustBucket(t, s, "abc")
	ctx := context.Background()
	if _, err := s.BucketStats(ctx, "missing"); err == nil {
		t.Fatal("缺失桶应报错")
	}
	if _, err := s.BucketStats(ctx, "BAD"); err == nil {
		t.Fatal("非法桶名应报错")
	}
	injected := errors.New("注入错误")
	s.fs.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := s.BucketStats(ctx, "abc"); !errors.Is(err, injected) {
		t.Fatalf("读取失败应透传：%v", err)
	}
}
