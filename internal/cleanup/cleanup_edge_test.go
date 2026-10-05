package cleanup

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// fakeArchiveFile 实现 archiveFile，按注入的错误依次让 Write/Sync/Close 失败。
type fakeArchiveFile struct {
	name     string
	writeErr error
	syncErr  error
	closeErr error
}

func (f *fakeArchiveFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeArchiveFile) Sync() error { return f.syncErr }

func (f *fakeArchiveFile) Close() error { return f.closeErr }

func (f *fakeArchiveFile) Name() string { return f.name }

// stubCreateTemp 替换 createArchiveTemp 并在测试结束时还原。
func stubCreateTemp(t *testing.T, fn func(dir, pattern string) (archiveFile, error)) {
	t.Helper()
	orig := createArchiveTemp
	createArchiveTemp = fn
	t.Cleanup(func() { createArchiveTemp = orig })
}

func TestSaveCreateTempError(t *testing.T) {
	stubCreateTemp(t, func(string, string) (archiveFile, error) {
		return nil, errors.New("no temp")
	})
	err := Save(filepath.Join(t.TempDir(), "archive", "commands.json"), &Archive{Schema: 1})
	if err == nil || !strings.Contains(err.Error(), "创建临时文件失败") {
		t.Errorf("CreateTemp 失败应报创建临时文件失败: %v", err)
	}
}

func TestSaveWriteError(t *testing.T) {
	stubCreateTemp(t, func(string, string) (archiveFile, error) {
		return &fakeArchiveFile{name: filepath.Join(t.TempDir(), "x.tmp"), writeErr: errors.New("disk full")}, nil
	})
	err := Save(filepath.Join(t.TempDir(), "archive", "commands.json"), &Archive{Schema: 1})
	if err == nil || !strings.Contains(err.Error(), "写入归档失败") {
		t.Errorf("Write 失败应报写入归档失败: %v", err)
	}
}

func TestSaveSyncError(t *testing.T) {
	stubCreateTemp(t, func(string, string) (archiveFile, error) {
		return &fakeArchiveFile{name: filepath.Join(t.TempDir(), "x.tmp"), syncErr: errors.New("sync failed")}, nil
	})
	err := Save(filepath.Join(t.TempDir(), "archive", "commands.json"), &Archive{Schema: 1})
	if err == nil || !strings.Contains(err.Error(), "刷新归档失败") {
		t.Errorf("Sync 失败应报刷新归档失败: %v", err)
	}
}

func TestSaveCloseError(t *testing.T) {
	stubCreateTemp(t, func(string, string) (archiveFile, error) {
		return &fakeArchiveFile{name: filepath.Join(t.TempDir(), "x.tmp"), closeErr: errors.New("close failed")}, nil
	})
	err := Save(filepath.Join(t.TempDir(), "archive", "commands.json"), &Archive{Schema: 1})
	if err == nil || !strings.Contains(err.Error(), "关闭归档文件失败") {
		t.Errorf("Close 失败应报关闭归档文件失败: %v", err)
	}
}

func TestSaveChmodError(t *testing.T) {
	// Name 指向不存在的路径 → Chmod 必失败
	missing := filepath.Join(t.TempDir(), "no-such-dir", "gone.tmp")
	stubCreateTemp(t, func(string, string) (archiveFile, error) {
		return &fakeArchiveFile{name: missing}, nil
	})
	err := Save(filepath.Join(t.TempDir(), "archive", "commands.json"), &Archive{Schema: 1})
	if err == nil || !strings.Contains(err.Error(), "设置归档文件权限失败") {
		t.Errorf("Chmod 失败应报设置归档文件权限失败: %v", err)
	}
}

func TestParseCommandEmptyCommandName(t *testing.T) {
	// "/ x"：斜杠后直接空格 → 命令名为空 → unknown
	if got := ParseCommand("/ x"); got != "unknown" {
		t.Errorf(`ParseCommand("/ x") = %q, want "unknown"`, got)
	}
}
