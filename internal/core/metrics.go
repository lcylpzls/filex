package core

// nopMetrics 是空实现，保证 Metrics 可空注入。
type nopMetrics struct{}

// IncCounter 空实现：默认不收集计数指标。
func (nopMetrics) IncCounter(string, []string) {
}

// AddCounter 空实现：默认不累加计数指标。
func (nopMetrics) AddCounter(string, float64, []string) {
}

// ObserveDuration 空实现：默认不收集耗时指标。
func (nopMetrics) ObserveDuration(string, float64, []string) {
}

// AddGauge 空实现：默认不调整瞬时量指标。
func (nopMetrics) AddGauge(string, float64, []string) {
}

// SetGauge 空实现：默认不设置瞬时量指标。
func (nopMetrics) SetGauge(string, float64, []string) {
}

// RegisterMetric 空实现：默认不预注册指标。
func (nopMetrics) RegisterMetric(string, string, []string) error {
	return nil
}

// addBytes 按桶与操作累加字节量指标（统一映射到 metricsx 计数协议）。
func addBytes(m Metrics, bucket, operation string, bytes int64) {
	if m == nil {
		return
	}
	m.AddCounter("filex.bytes", float64(bytes), []string{"bucket", bucket, "operation", operation})
}

// incError 记录指定桶的错误码指标（统一映射到 metricsx 计数协议）。
func incError(m Metrics, bucket, code string) {
	if m == nil {
		return
	}
	m.IncCounter("filex.errors", []string{"bucket", bucket, "code", code})
}
