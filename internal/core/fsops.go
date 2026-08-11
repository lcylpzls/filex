package core

import (
	"io"
	"os"
)

// fsOps 是 Store 的文件系统操作接缝，测试可注入错误分支。
// 生产环境全部走 defaultFSOps。
type fsOps struct {
	MkdirAll    func(path string, perm os.FileMode) error
	ReadDir     func(path string) ([]os.DirEntry, error)
	ReadFile    func(path string) ([]byte, error)
	CreateTemp  func(dir, pattern string) (*os.File, error)
	OpenFile    func(name string, flag int, perm os.FileMode) (*os.File, error)
	WriteToFile func(dst io.Writer, r io.Reader) (int64, error)
	SyncFile    func(f *os.File) error
	CloseFile   func(f *os.File) error
	Rename      func(oldpath, newpath string) error
	Remove      func(name string) error
	RemoveAll   func(path string) error
	Stat        func(name string) (os.FileInfo, error)
	SyncPath    func(path string) error
}

var defaultFSOps = fsOps{
	MkdirAll:   os.MkdirAll,
	ReadDir:    os.ReadDir,
	ReadFile:   os.ReadFile,
	CreateTemp: os.CreateTemp,
	OpenFile:   os.OpenFile,
	WriteToFile: func(dst io.Writer, r io.Reader) (int64, error) {
		return io.Copy(dst, r)
	},
	SyncFile:  func(f *os.File) error { return f.Sync() },
	CloseFile: func(f *os.File) error { return f.Close() },
	Rename:    os.Rename,
	Remove:    os.Remove,
	RemoveAll: os.RemoveAll,
	Stat:      os.Stat,
	SyncPath:  defaultSyncPath,
}

// defaultSyncPath 打开目录并执行 fsync；Windows 上通常返回错误，
// 调用方按“尽力而为”忽略。
func defaultSyncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
