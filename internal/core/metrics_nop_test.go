package core

import (
	"testing"
)

// TestNopMetricsMethods 覆盖默认空指标实现的全部方法。
func TestNopMetricsMethods(t *testing.T) {
	var m nopMetrics
	m.IncCounter("x", nil)
	m.AddCounter("x", 1, nil)
	m.ObserveDuration("x", 1, nil)
	m.AddGauge("x", 1, nil)
	m.SetGauge("x", 1, nil)
	_ = m.RegisterMetric("x", "", nil)
}

// TestAddBytesNil 覆盖辅助函数的 nil 接收器分支。
func TestAddBytesNil(t *testing.T) {
	addBytes(nil, "b", "op", 1)
	incError(nil, "b", "code")
}

// TestAddBytesSink 覆盖辅助函数的非 nil 分支。
func TestAddBytesSink(t *testing.T) {
	addBytes(nopMetrics{}, "b", "op", 1)
	incError(nopMetrics{}, "b", "code")
}
