package main

import "testing"

func TestRun(t *testing.T) {
	if err := run(t.TempDir()); err != nil {
		t.Fatalf("基础示例运行失败：%v", err)
	}
}
