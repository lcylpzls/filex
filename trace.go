package filex

import "context"

// TraceAttr 链路追踪属性。
type TraceAttr struct {
	Key   string
	Value string
}

// TraceHook 链路追踪钩子（可选，默认 no-op）。
// 库本身不依赖任何追踪实现，由 tracex 等外部适配器接入。
type TraceHook interface {
	// Start 在存储操作开始前调用：返回携带链路上下文的 ctx 与结束回调
	// （结束回调入参为操作结果错误，nil 表示成功）。
	Start(ctx context.Context, name string, attrs ...TraceAttr) (context.Context, func(error))
}
