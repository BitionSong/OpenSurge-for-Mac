package controlapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
)

func (c HelperClient) PolicyWorkspace(ctx context.Context, path string, input PolicyWorkspaceInput) (PolicyWorkspaceResponse, error) {
	response, err := c.call(ctx, HelperRequest{Action: "policy-workspace", ConfigPath: path, Workspace: &input})
	if err != nil {
		return PolicyWorkspaceResponse{}, err
	}
	if response.Workspace == nil {
		return PolicyWorkspaceResponse{}, fmt.Errorf("helper did not return a policy workspace")
	}
	return *response.Workspace, nil
}

func (c HelperClient) HoldPolicyWorkspace(ctx context.Context, path string) (io.Closer, error) {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err = json.NewEncoder(conn).Encode(HelperRequest{Action: "policy-workspace-hold", ConfigPath: path}); err == nil {
		var response HelperResponse
		err = json.NewDecoder(conn).Decode(&response)
		if err == nil && !response.OK {
			err = fmt.Errorf("%s", response.Error)
		}
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

type preparedCloser struct{ cfg config.Config }

func (c preparedCloser) Close() error { return mihomo.StopPrepared(c.cfg) }
func (DirectRunner) HoldPolicyWorkspace(_ context.Context, path string) (io.Closer, error) {
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		return nil, err
	}
	return preparedCloser{cfg}, nil
}

type helperPolicyLeases struct {
	mu      sync.Mutex
	holders map[string]int
	stop    func(config.Config) error
}

func newHelperPolicyLeases() *helperPolicyLeases {
	return &helperPolicyLeases{holders: make(map[string]int), stop: mihomo.StopPrepared}
}
func (m *helperPolicyLeases) acquire(cfg config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.holders[cfg.Runtime.Dir]++
}
func (m *helperPolicyLeases) run(cfg config.Config, fn func() (PolicyWorkspaceResponse, error)) (PolicyWorkspaceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.holders[cfg.Runtime.Dir] == 0 {
		return PolicyWorkspaceResponse{}, fmt.Errorf("policy workspace lease is disconnected")
	}
	return fn()
}
func (m *helperPolicyLeases) release(_ context.Context, cfg config.Config) {
	// Keep ownership until cleanup succeeds. Gateway transitions can briefly
	// hold the shared lifecycle lock; retry instead of orphaning a prepared core.
	m.mu.Lock()
	m.holders[cfg.Runtime.Dir]--
	last := m.holders[cfg.Runtime.Dir] == 0
	if !last {
		m.mu.Unlock()
		return
	}
	var lastErr error
	for attempt := 0; attempt < 120; attempt++ {
		if lastErr = m.stop(cfg); lastErr == nil {
			delete(m.holders, cfg.Runtime.Dir)
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		// Cleanup ownership survives Helper context cancellation. Selecting an
		// already-closed ctx here would spin through every retry immediately.
		time.Sleep(250 * time.Millisecond)
		m.mu.Lock()
		if m.holders[cfg.Runtime.Dir] > 0 {
			m.mu.Unlock()
			return
		}
	}
	log.Printf("OpenSurge prepared policy engine cleanup remains pending for %s: %v", cfg.Runtime.Dir, lastErr)
	m.mu.Unlock()
}

func servePolicyWorkspaceLease(ctx context.Context, conn net.Conn, cfg config.Config, manager *helperPolicyLeases, err error) {
	if err == nil && manager == nil {
		err = fmt.Errorf("policy workspace lease manager is unavailable")
	}
	if err == nil {
		manager.acquire(cfg)
	}
	response := HelperResponse{OK: err == nil}
	if err != nil {
		response.Error = err.Error()
	}
	encodeErr := json.NewEncoder(conn).Encode(response)
	if err != nil {
		return
	}
	defer manager.release(ctx, cfg)
	if encodeErr != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	disconnected := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, conn); close(disconnected) }()
	select {
	case <-ctx.Done():
	case <-disconnected:
	}
}
