package filex

// nopMetrics 是空实现，保证 Metrics 可空注入。
type nopMetrics struct{}

// Add 空实现：默认不收集指标。
func (nopMetrics) Add(string, string, int64) {
	_ = 0
}

// IncError 空实现：默认不收集错误指标。
func (nopMetrics) IncError(string, string) {
	_ = 0
}
