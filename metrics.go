package filex

// nopMetrics 是空实现，保证 Metrics 可空注入。
type nopMetrics struct{}

func (nopMetrics) Add(string, string, int64) {}
func (nopMetrics) IncError(string, string)   {}
