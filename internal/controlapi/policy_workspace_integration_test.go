package controlapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/mihomo"
	"open-mihomo-gateway/internal/runtime"
)

// This gate proves the source-free product path against a real mihomo core.
// It starts only the prepared authenticated loopback controller: no gateway
// Manager, DHCP/DNS listener, TUN, pf, forwarding or Tailscale identity.
func TestPolicyWorkspaceOverlayOnlyRealCore(t *testing.T) {
	binary := os.Getenv("OMG_PREPARED_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("set OMG_PREPARED_MIHOMO_BINARY to run the source-free prepared workspace gate")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = filepath.Join(dir, "runtime")
	cfg.Mihomo.Binary = binary
	cfg.Mihomo.Config = filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
	if err := writeAtomic(path, []byte(config.Render(cfg)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	const originalBase = `{"effective_path":"unused-previous-profile"}`
	if err := writeAtomic(policyWorkspaceBasePath(cfg), []byte(originalBase), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := mihomo.StopPrepared(cfg); err != nil {
			t.Errorf("stop source-free prepared core: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	input := PolicyWorkspaceInput{
		Request:              PolicyWorkspaceRequest{Action: "read"},
		Revision:             fileDigest(path),
		ExpectedGatewayState: policyWorkspaceGatewayStopped,
		Overlay:              []byte(strings.ReplaceAll(policyWorkspaceOverlayFixture, "{name: Overlay-Node, type: direct}", "{name: Overlay-Node, type: socks5, server: 127.0.0.1, port: 1}")),
	}
	workspace, err := runPolicyWorkspace(ctx, path, input)
	if err != nil {
		t.Fatal(err)
	}
	group := findWorkspaceGroup(workspace.Groups, "Added")
	if group == nil || !containsWorkspaceString(group.Options, "Overlay-Node") || !containsWorkspaceString(group.Options, "DIRECT") {
		t.Fatalf("source-free selector is unavailable: %+v", workspace.Groups)
	}
	if !workspaceProxyProbeable(workspace, "Overlay-Node") {
		t.Fatalf("source-free node is not testable: %+v", workspace.Health.Proxies)
	}

	input.Request = PolicyWorkspaceRequest{Action: "select", Group: "Added", Policy: "DIRECT"}
	input.Revision = workspace.Revision
	workspace, err = runPolicyWorkspace(ctx, path, input)
	if err != nil {
		t.Fatal(err)
	}
	group = findWorkspaceGroup(workspace.Groups, "Added")
	if group == nil || group.Selected != "DIRECT" {
		t.Fatalf("source-free selector did not change: %+v", group)
	}

	// Every stopped action leaves desired and recovery records untouched. Probe
	// the fixture's closed loopback proxy; reachability is immaterial here.
	input.Request = PolicyWorkspaceRequest{Action: "test", Names: []string{"Overlay-Node"}}
	if _, err := runPolicyWorkspace(ctx, path, input); err != nil {
		t.Fatal(err)
	}
	if err := mihomo.StopPrepared(cfg); err != nil {
		t.Fatal(err)
	}
	input.Request = PolicyWorkspaceRequest{Action: "read"}
	workspace, err = runPolicyWorkspace(ctx, path, input)
	if err != nil {
		t.Fatal(err)
	}
	if group := findWorkspaceGroup(workspace.Groups, "Added"); group == nil || group.Selected != "DIRECT" {
		t.Fatalf("prepared restart lost native selection cache: %+v", group)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != config.Render(cfg) {
		t.Fatalf("preview changed desired: %v\n%s", err, data)
	}
	if data, err := os.ReadFile(policyWorkspaceBasePath(cfg)); err != nil || string(data) != originalBase {
		t.Fatalf("preview changed base recovery metadata: %v", err)
	}
	if _, exists, err := runtime.LoadState(runtime.NewPaths(cfg).StateFile); err != nil || exists {
		t.Fatalf("prepared workspace created gateway state: exists=%t err=%v", exists, err)
	}
}

// This gate exercises the Web GUI's no-Policies-visit startup transaction up
// to (but deliberately excluding) gateway.Manager network takeover. A real
// mihomo binary must accept the source-free candidate before it is persisted
// and handed to the locked starter.
func TestStartPolicyWorkspaceOverlayOnlyRealValidation(t *testing.T) {
	binary := os.Getenv("OMG_PREPARED_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("set OMG_PREPARED_MIHOMO_BINARY to run the source-free direct-start validation gate")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = filepath.Join(dir, "runtime")
	cfg.Mihomo.Binary = binary
	cfg.Mihomo.Config = filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
	if err := writeAtomic(path, []byte(config.Render(cfg)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := false
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = startPolicyWorkspace(ctx, path, PolicyWorkspaceInput{
		Request:              PolicyWorkspaceRequest{Action: "read"},
		Revision:             fileDigest(path),
		ExpectedGatewayState: policyWorkspaceGatewayStopped,
		Overlay:              []byte(policyWorkspaceOverlayFixture),
	}, policyWorkspaceStartDeps{
		geteuid: func() int { return 0 },
		startLocked: func(ctx context.Context, candidate config.Config, commit func() error) error {
			if err := mihomo.StopPreparedLocked(candidate); err != nil {
				return err
			}
			paths := runtime.NewPaths(candidate)
			if err := runtime.Ensure(paths); err != nil {
				return err
			}
			manager := mihomo.New(candidate, paths)
			if err := manager.WriteConfig(); err != nil {
				return err
			}
			if err := manager.ValidateWrittenConfigContext(ctx); err != nil {
				return err
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != config.Render(cfg) {
				t.Fatalf("desired changed before final validation: %v", err)
			}
			if err := commit(); err != nil {
				return err
			}
			started = true
			final, err := mihomo.RenderConfig(candidate)
			if err != nil {
				return err
			}
			if workspaceTestGroups(t, final)["Added"] == nil || !strings.Contains(final, "DOMAIN,overlay.example,Added") {
				return fmt.Errorf("validated direct-start candidate lost source-free overlay")
			}
			if _, exists, err := runtime.LoadState(runtime.NewPaths(candidate).StateFile); err != nil || exists {
				return fmt.Errorf("validation-only direct start created gateway state: exists=%t err=%v", exists, err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("validated candidate was not handed to the locked gateway starter")
	}
	desired, err := config.Load(path)
	if err != nil || desired.Mihomo.ProfileOverlayDigest != mihomo.ProfileOverlayDigest([]byte(policyWorkspaceOverlayFixture)) {
		t.Fatalf("validated source-free candidate was not persisted: cfg=%#v err=%v", desired.Mihomo, err)
	}
}

func TestPolicyWorkspaceMaterializedHTTPProvidersRealCore(t *testing.T) {
	binary := os.Getenv("OMG_PREPARED_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("set OMG_PREPARED_MIHOMO_BINARY to run the materialized provider workspace gate")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies.yaml":
			_, _ = io.WriteString(w, "proxies:\n  - {name: provider-node, type: direct}\n")
		case "/rules.yaml":
			_, _ = io.WriteString(w, "payload:\n  - DOMAIN,provider.example\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.DHCP.Enabled = false
	cfg.Runtime.Dir = filepath.Join(dir, "runtime")
	cfg.Mihomo.Binary = binary
	cfg.Mihomo.Config = filepath.Join(cfg.Runtime.Dir, "mihomo.yaml")
	cfg.Mihomo.ProfileMode = config.MihomoProfileModeImported
	cfg.Mihomo.Profile = filepath.Join(dir, "selected-source", "profile.yaml")
	source := fmt.Sprintf(`proxy-providers:
  subscription:
    type: http
    url: %q
    path: ./proxy-provider-cache.yaml
    interval: 3600
    health-check: {enable: false}
proxy-groups:
  - {name: ProviderGroup, type: select, use: [subscription]}
rule-providers:
  custom:
    type: http
    behavior: classical
    format: yaml
    url: %q
    path: ./rule-provider-cache.yaml
    interval: 3600
rules:
  - RULE-SET,custom,ProviderGroup
  - MATCH,DIRECT
`, origin.URL+"/proxies.yaml", origin.URL+"/rules.yaml")
	cfg.Mihomo.ProfileSourceDigest = mihomo.ProfileOverlayDigest([]byte(source))
	if err := writeAtomic(cfg.Mihomo.Profile, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte(config.Render(cfg)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := mihomo.StopPrepared(cfg); err != nil {
			t.Errorf("stop provider prepared core: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	workspace, err := runPolicyWorkspace(ctx, path, PolicyWorkspaceInput{
		Request:              PolicyWorkspaceRequest{Action: "read"},
		Revision:             fileDigest(path),
		ExpectedGatewayState: policyWorkspaceGatewayStopped,
		Source:               []byte(source),
		Overlay:              []byte("schema-version: 1\nenabled: true\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	group := findWorkspaceGroup(workspace.Groups, "ProviderGroup")
	providerDeadline := time.Now().Add(5 * time.Second)
	for (group == nil || !containsWorkspaceString(group.Options, "provider-node")) && time.Now().Before(providerDeadline) {
		time.Sleep(100 * time.Millisecond)
		workspace, err = runPolicyWorkspace(ctx, path, PolicyWorkspaceInput{
			Request:              PolicyWorkspaceRequest{Action: "read"},
			Revision:             workspace.Revision,
			ExpectedGatewayState: policyWorkspaceGatewayStopped,
			Source:               []byte(source),
			Overlay:              []byte("schema-version: 1\nenabled: true\n"),
		})
		if err != nil {
			t.Fatal(err)
		}
		group = findWorkspaceGroup(workspace.Groups, "ProviderGroup")
	}
	if group == nil || !containsWorkspaceString(group.Options, "provider-node") {
		logData, _ := os.ReadFile(filepath.Join(cfg.Runtime.Dir, "prepared-mihomo", "mihomo.log"))
		t.Fatalf("materialized proxy provider was not expanded: %+v\nprepared log:\n%s", workspace.Groups, logData)
	}
	desired, _, err := workspaceCandidate(path, cfg, PolicyWorkspaceInput{Source: []byte(source), Overlay: []byte("schema-version: 1\nenabled: true\n")})
	if err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "data")
	if filepath.Dir(desired.Mihomo.Profile) != workDir {
		t.Fatalf("materialized profile work directory=%q, want %q", filepath.Dir(desired.Mihomo.Profile), workDir)
	}
	final, err := mihomo.RenderConfig(desired)
	if err != nil {
		t.Fatal(err)
	}
	var renderedProviders struct {
		ProxyProviders map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"proxy-providers"`
		RuleProviders map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal([]byte(final), &renderedProviders); err != nil {
		t.Fatal(err)
	}
	for name, cachePath := range map[string]string{
		"subscription": renderedProviders.ProxyProviders["subscription"].Path,
		"custom":       renderedProviders.RuleProviders["custom"].Path,
	} {
		if filepath.Dir(cachePath) != workDir {
			t.Fatalf("provider %q cache left managed work dir: %q", name, cachePath)
		}
		if _, err := os.Stat(cachePath); err != nil {
			t.Fatalf("provider %q cache was not materialized: %v", name, err)
		}
	}
	prepared, exists, err := mihomo.LoadPrepared(desired)
	if err != nil || !exists {
		t.Fatalf("prepared provider core state: exists=%t err=%v", exists, err)
	}
	providers, err := mihomo.FetchProviders(ctx, mihomo.PreparedConfig(desired, prepared))
	if err != nil {
		t.Fatal(err)
	}
	foundRuleProvider := false
	for _, provider := range providers.RuleProviders {
		if provider.Name == "custom" && provider.RuleCount == 1 {
			foundRuleProvider = true
		}
	}
	if !foundRuleProvider {
		t.Fatalf("materialized rule provider did not load: %+v", providers.RuleProviders)
	}
	if _, err := os.Stat(policyWorkspaceBasePath(desired)); !os.IsNotExist(err) {
		t.Fatalf("provider preview wrote base metadata: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != config.Render(cfg) {
		t.Fatalf("provider preview replaced desired: %v", err)
	}
	if strings.Contains(final, filepath.Join(dir, "selected-source")) {
		t.Fatalf("final provider paths escaped the managed mihomo work directory:\n%s", final)
	}
	if _, exists, err := runtime.LoadState(runtime.NewPaths(desired).StateFile); err != nil || exists {
		t.Fatalf("provider workspace created gateway state: exists=%t err=%v", exists, err)
	}
}

func findWorkspaceGroup(groups []mihomo.ProxyGroup, name string) *mihomo.ProxyGroup {
	for index := range groups {
		if groups[index].Name == name {
			return &groups[index]
		}
	}
	return nil
}

func workspaceProxyProbeable(workspace PolicyWorkspaceResponse, name string) bool {
	for _, proxy := range workspace.Health.Proxies {
		if proxy.Name == name {
			return proxy.Probeable
		}
	}
	return false
}
