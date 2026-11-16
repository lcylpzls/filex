package replication

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lcylpzls/filex"
	testx "github.com/lcylpzls/testx"
)

func TestTaskStoreLoadErrors(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	injected := errors.New("注入错误")
	store := &taskStore{dir: t.TempDir()}
	taskFS.ReadDir = func(string) ([]os.DirEntry, error) { return nil, injected }
	if _, err := store.load(); !errors.Is(err, injected) {
		t.Fatalf("目录扫描错误未返回：%v", err)
	}

	taskFS = base
	fileName := filepath.Join(store.dir, "task.json")
	testx.RequireNoError(t, os.WriteFile(fileName, []byte("{"), 0o644))
	testx.RequireNoError(t, os.WriteFile(filepath.Join(store.dir, "ignore.txt"), []byte("x"), 0o644))
	testx.RequireNoError(t, os.Mkdir(filepath.Join(store.dir, "directory.json"), 0o755))
	if _, err := store.load(); err == nil {
		t.Fatal("损坏任务应报错")
	}

	taskFS = base
	testx.RequireNoError(t, os.WriteFile(fileName, []byte(`{}`), 0o644))
	if _, err := store.load(); err == nil {
		t.Fatal("缺少字段应报错")
	}

	taskFS = base
	badID := Task{Bucket: "center", Key: "key", ID: "wrong", Expected: filexObjectInfo("key", "")}
	testx.RequireNoError(t, writeTaskFile(fileName, badID))
	if _, err := store.load(); err == nil {
		t.Fatal("任务 ID 不一致应报错")
	}

	taskFS = base
	taskFS.ReadFile = func(string) ([]byte, error) { return nil, injected }
	if _, err := store.load(); !errors.Is(err, injected) {
		t.Fatalf("读取任务错误未返回：%v", err)
	}
}

func TestTaskStoreRunningRecoveryAndSaveErrors(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	store := &taskStore{dir: t.TempDir()}
	task := Task{
		Bucket:    "center",
		Key:       "key",
		VersionID: "version",
		Action:    ActionUpsert,
		Status:    TaskRunning,
		Expected:  filexObjectInfo("key", "version"),
	}
	task.ID = taskID(task.Bucket, task.Key, task.VersionID)
	task.Expected.Bucket = task.Bucket
	task.Expected.VersionID = task.VersionID
	if err := store.save(task); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.load()
	testx.RequireNoError(t, err)
	if tasks[task.ID].Status != TaskPending {
		t.Fatalf("running 未恢复为 pending：%+v", tasks[task.ID])
	}

	injected := errors.New("注入错误")
	task.Status = TaskRunning
	testx.RequireNoError(t, store.save(task))
	taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
	if _, err := store.load(); !errors.Is(err, injected) {
		t.Fatalf("running 恢复保存错误未返回：%v", err)
	}
	taskFS = base
	cases := []struct {
		name  string
		setup func()
	}{
		{"创建临时文件", func() {
			taskFS.CreateTemp = func(string, string) (taskFile, error) { return nil, injected }
		}},
		{"写入", func() {
			taskFS.CreateTemp = func(string, string) (taskFile, error) {
				return &fakeTaskFile{name: filepath.Join(store.dir, "tmp"), writeErr: injected}, nil
			}
		}},
		{"同步", func() {
			taskFS.CreateTemp = func(string, string) (taskFile, error) {
				return &fakeTaskFile{name: filepath.Join(store.dir, "tmp"), syncErr: injected}, nil
			}
		}},
		{"关闭", func() {
			taskFS.CreateTemp = func(string, string) (taskFile, error) {
				return &fakeTaskFile{name: filepath.Join(store.dir, "tmp"), closeErr: injected}, nil
			}
		}},
		{"重命名", func() {
			taskFS.Rename = func(string, string) error { return injected }
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			taskFS = base
			test.setup()
			if err := store.save(task); !errors.Is(err, injected) {
				t.Fatalf("保存错误未返回：%v", err)
			}
		})
	}
}

func TestTaskStoreRemoveAndDirectorySyncErrors(t *testing.T) {
	base := taskFS
	t.Cleanup(func() { taskFS = base })
	store := &taskStore{dir: t.TempDir()}
	injected := errors.New("注入错误")
	taskFS.Open = func(string) (taskDir, error) { return nil, injected }
	syncTaskDir(store.dir)

	taskFS = base
	dirPath := filepath.Join(store.dir, "task.json")
	testx.RequireNoError(t, os.Mkdir(dirPath, 0o755))
	testx.RequireNoError(t, os.WriteFile(filepath.Join(dirPath, "child"), []byte("x"), 0o644))
	if err := store.remove("task"); err == nil {
		t.Fatal("删除非空目录应报错")
	}
	taskFS = base
	if err := store.remove("missing"); err != nil {
		t.Fatalf("删除不存在任务应幂等：%v", err)
	}

	taskFS = base
	testx.RequireNoError(t, os.WriteFile(filepath.Join(store.dir, "closed"), []byte("x"), 0o644))
	taskFS.Open = func(path string) (taskDir, error) {
		return &fakeTaskDir{closeErr: injected}, nil
	}
	syncTaskDir(store.dir)

	taskFS = base
	taskFS.MkdirAll = func(string, os.FileMode) error { return injected }
	if _, err := newTaskStore(t.TempDir()); !errors.Is(err, injected) {
		t.Fatalf("创建任务目录错误未返回：%v", err)
	}
}

type fakeTaskFile struct {
	name     string
	writeErr error
	syncErr  error
	closeErr error
}

func (f *fakeTaskFile) Name() string { return f.name }
func (f *fakeTaskFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(data), nil
}
func (f *fakeTaskFile) Sync() error  { return f.syncErr }
func (f *fakeTaskFile) Close() error { return f.closeErr }

type fakeTaskDir struct {
	closeErr error
}

func (*fakeTaskDir) Sync() error    { return nil }
func (d *fakeTaskDir) Close() error { return d.closeErr }

func writeTaskFile(path string, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func filexObjectInfo(key, versionID string) filex.ObjectInfo {
	return filex.ObjectInfo{
		Bucket:    "center",
		Key:       key,
		VersionID: versionID,
		ETag:      taskID("center", key, versionID),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

var _ io.Writer = (*fakeTaskFile)(nil)
