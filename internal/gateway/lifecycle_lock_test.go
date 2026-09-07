package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"open-mihomo-gateway/internal/config"
)

func TestLifecycleLockExcludesConcurrentGatewayOperations(t *testing.T) {
	cfg := config.Config{Runtime: config.RuntimeConfig{Dir: t.TempDir()}}
	first, err := acquireLifecycleLock(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.release() })

	busy, err := LifecycleOperationInProgress(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !busy {
		t.Fatal("held lifecycle lock was reported as available")
	}
	if _, err := acquireLifecycleLock(cfg); !errors.Is(err, ErrLifecycleOperationInProgress) {
		t.Fatalf("second lifecycle lock error = %v", err)
	}

	if err := first.release(); err != nil {
		t.Fatal(err)
	}
	busy, err = LifecycleOperationInProgress(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("released lifecycle lock remained busy")
	}
}

func TestLifecycleOperationInProgressWithoutLockFile(t *testing.T) {
	cfg := config.Config{Runtime: config.RuntimeConfig{Dir: t.TempDir()}}
	busy, err := LifecycleOperationInProgress(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("missing lifecycle lock file was reported as busy")
	}
}

func TestStartConfigLoadsDesiredConfigurationInsideLifecycleLock(t *testing.T) {
	lockConfig := config.Default()
	lockConfig.Runtime.Dir = t.TempDir()
	desired := lockConfig
	desired.Mihomo.APIAddr = "127.0.0.1:19090"
	loadedDesired := false
	started := false
	err := runConfigLifecycle(t.Context(), "/config.yaml", configLifecycleDeps{
		loadRuntime: func(string) (config.Config, error) { return lockConfig, nil },
		loadLocked: func(string) (config.Config, error) {
			busy, err := LifecycleOperationInProgress(lockConfig)
			if err != nil {
				t.Fatal(err)
			}
			if !busy {
				t.Fatal("desired config was read before the lifecycle lock")
			}
			loadedDesired = true
			return desired, nil
		},
		runLocked: func(_ context.Context, cfg config.Config) error {
			started = true
			if !loadedDesired || cfg.Mihomo.APIAddr != desired.Mihomo.APIAddr {
				t.Fatalf("started stale config: %#v", cfg.Mihomo)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("desired config was not started")
	}
}

func TestStartConfigRejectsRuntimeDirectoryChangedBeforeLockedReload(t *testing.T) {
	lockConfig := config.Default()
	lockConfig.Runtime.Dir = filepath.Join(t.TempDir(), "old-runtime")
	desired := lockConfig
	desired.Runtime.Dir = filepath.Join(t.TempDir(), "new-runtime")
	started := false
	err := runConfigLifecycle(t.Context(), "/config.yaml", configLifecycleDeps{
		loadRuntime: func(string) (config.Config, error) { return lockConfig, nil },
		loadLocked:  func(string) (config.Config, error) { return desired, nil },
		runLocked:   func(context.Context, config.Config) error { started = true; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "runtime.dir changed") {
		t.Fatalf("runtime directory race error=%v", err)
	}
	if started {
		t.Fatal("config protected by a different runtime lock was started")
	}
}
