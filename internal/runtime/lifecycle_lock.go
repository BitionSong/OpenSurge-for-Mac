package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"open-mihomo-gateway/internal/config"
)

const lifecycleLockName = ".gateway-lifecycle.lock"

var ErrLifecycleOperationInProgress = errors.New("another gateway lifecycle operation is already running")

// LifecycleLock excludes both gateway network transitions and the prepared
// engine. CLI startup cannot race a Helper using the same cache/tsnet identity.
type LifecycleLock struct {
	file *os.File
}

func AcquireLifecycleLock(cfg config.Config) (*LifecycleLock, error) {
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare gateway lifecycle lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(cfg.Runtime.Dir, lifecycleLockName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open gateway lifecycle lock: %w", err)
	}
	if err := ensureLifecycleLockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLifecycleOperationInProgress
		}
		return nil, fmt.Errorf("lock gateway lifecycle: %w", err)
	}
	return &LifecycleLock{file: file}, nil
}

func (l *LifecycleLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}

func ensureLifecycleLockFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect gateway lifecycle lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("gateway lifecycle lock is not a regular file")
	}
	return nil
}

// LifecycleOperationInProgress inspects the kernel-held lock, not the file's
// existence. Read-only users may inspect but cannot create this lock.
func LifecycleOperationInProgress(cfg config.Config) (bool, error) {
	file, err := os.Open(filepath.Join(cfg.Runtime.Dir, lifecycleLockName))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open gateway lifecycle lock for inspection: %w", err)
	}
	defer file.Close()
	if err := ensureLifecycleLockFile(file); err != nil {
		return false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return true, nil
		}
		return false, fmt.Errorf("inspect gateway lifecycle lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return false, fmt.Errorf("release inspected gateway lifecycle lock: %w", err)
	}
	return false, nil
}

func WithLifecycleLock(cfg config.Config, run func() error) (err error) {
	lock, err := AcquireLifecycleLock(cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	return run()
}
