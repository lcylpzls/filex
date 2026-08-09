package filex

import (
	"context"
	"errors"
	"io"
)

// contextReader 在读取前检查上下文，取消/超时立即中断长操作。
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func newContextReader(ctx context.Context, r io.Reader) io.Reader {
	if ctx == nil {
		return r
	}
	return &contextReader{ctx: ctx, r: r}
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// wrapCtxErr 将上下文取消/超时错误归一为 filex_cancelled。
func wrapCtxErr(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return newCode(CodeCancelled, "操作已取消")
	}
	return err
}

// storageErr 包装存储错误，取消类错误优先返回 filex_cancelled。
func storageErr(err error, msg string) error {
	if e := wrapCtxErr(err); e != err {
		return e
	}
	return wrapCode(err, CodeStorageFailed, msg)
}
