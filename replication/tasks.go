package replication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type taskStore struct {
	dir string
}

type taskFile interface {
	Name() string
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type taskDir interface {
	Sync() error
	Close() error
}

type taskFSOps struct {
	MkdirAll   func(string, os.FileMode) error
	ReadDir    func(string) ([]os.DirEntry, error)
	ReadFile   func(string) ([]byte, error)
	CreateTemp func(string, string) (taskFile, error)
	Rename     func(string, string) error
	Remove     func(string) error
	Open       func(string) (taskDir, error)
}

var taskFS = taskFSOps{
	MkdirAll: os.MkdirAll,
	ReadDir:  os.ReadDir,
	ReadFile: os.ReadFile,
	CreateTemp: func(dir, pattern string) (taskFile, error) {
		return os.CreateTemp(dir, pattern)
	},
	Rename: os.Rename,
	Remove: os.Remove,
	Open: func(path string) (taskDir, error) {
		return os.Open(path)
	},
}

func newTaskStore(stateDir string) (*taskStore, error) {
	dir := filepath.Join(stateDir, "tasks")
	if err := taskFS.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建复制任务目录失败：%w", err)
	}
	return &taskStore{dir: dir}, nil
}

func (s *taskStore) load() (map[string]Task, error) {
	entries, err := taskFS.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("扫描复制任务失败：%w", err)
	}
	tasks := make(map[string]Task, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := taskFS.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("读取复制任务失败：%w", err)
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("解析复制任务失败：%w", err)
		}
		if task.ID == "" || task.Bucket == "" || task.Key == "" {
			return nil, fmt.Errorf("复制任务字段缺失：%s", entry.Name())
		}
		if task.ID != taskID(task.Bucket, task.Key, task.VersionID) ||
			task.VersionID != task.Expected.VersionID {
			return nil, fmt.Errorf("复制任务标识不一致：%s", entry.Name())
		}
		if task.Status == TaskRunning {
			task.Status = TaskPending
			task.LastTransition = time.Now().UTC()
			if err := s.save(task); err != nil {
				return nil, err
			}
		}
		tasks[task.ID] = task
	}
	return tasks, nil
}

func (s *taskStore) save(task Task) error {
	// Task 仅包含 JSON 原生可编码字段，编码失败意味着类型定义被破坏。
	data, _ := json.Marshal(task)
	file, err := taskFS.CreateTemp(s.dir, ".task-*.tmp")
	if err != nil {
		return fmt.Errorf("创建复制任务临时文件失败：%w", err)
	}
	tmp := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = taskFS.Remove(tmp)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入复制任务失败：%w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步复制任务失败：%w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭复制任务失败：%w", err)
	}
	if err := taskFS.Rename(tmp, filepath.Join(s.dir, task.ID+".json")); err != nil {
		return fmt.Errorf("提交复制任务失败：%w", err)
	}
	committed = true
	syncTaskDir(s.dir)
	return nil
}

func (s *taskStore) remove(id string) error {
	err := taskFS.Remove(filepath.Join(s.dir, id+".json"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除复制任务失败：%w", err)
	}
	return nil
}

func taskID(bucket, key, versionID string) string {
	sum := sha256.Sum256([]byte(bucket + "\x00" + key + "\x00" + versionID))
	return hex.EncodeToString(sum[:])
}

func syncTaskDir(path string) {
	dir, err := taskFS.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
