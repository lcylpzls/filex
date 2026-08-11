package core

import "context"

// ObjectEvent 描述一次对象操作事件。
type ObjectEvent struct {
	// Bucket 桶名。
	Bucket string
	// Key 对象键；List 等桶级操作为空。
	Key string
	// Action 操作类型：put / get / head / delete / list / copy / move。
	Action string
	// Err 操作结果错误；nil 表示成功。
	Err error
}

// EventHook 是可选事件钩子（默认 no-op）。
// 库本身不依赖任何事件总线实现，由 eventx 等外部适配器接入。
type EventHook interface {
	// OnObjectEvent 在对象操作结束时调用。
	OnObjectEvent(ctx context.Context, e ObjectEvent)
}
