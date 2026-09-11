package mihomo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/bbolt"
	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/device"
	"open-mihomo-gateway/internal/runtime"
)

func writeSelectionCache(t *testing.T, path string, selections map[string]string) {
	t.Helper()
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("selected"))
		if err != nil {
			return err
		}
		for group, selected := range selections {
			if err := bucket.Put([]byte(group), []byte(selected)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReadDevicePolicySelectionsUsesPinnedCoreCacheWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	if selections, err := readDevicePolicySelections(path); err != nil || len(selections) != 0 {
		t.Fatalf("missing cache: %v %v", selections, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read created a cache")
	}
	writeSelectionCache(t, path, map[string]string{"device/phone/default": "Missing", "Main": "Present"})
	before, _ := os.ReadFile(path)
	selections, err := readDevicePolicySelections(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selections, map[string]string{"device/phone/default": "Missing"}) {
		t.Fatalf("selections = %v", selections)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("read modified the core cache")
	}
}

func TestDevicePolicySelectionsUsesLiveCoreAndPreservesSkippedAppliedChoice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"proxies":{"device/phone/default":{"type":"Selector","now":"Present","all":["Present","Gone"]}}}`))
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Runtime.Dir = t.TempDir()
	cfg.Mihomo.APIAddr = strings.TrimPrefix(server.URL, "http://")
	// This cannot be opened as a database: a successful controller read must
	// not try to take the running core's cache lock.
	cfg.DevicePolicy.SelectionCachePath = cfg.Runtime.Dir
	bundle, err := device.CompilePolicyBundle(device.PolicySet{
		Devices:  []device.ManagedDevice{{ID: "skipped", MAC: "aa:bb:cc:dd:ee:02", IPv4: "192.168.50.102", Profile: "home", EgressMode: device.EgressModeDedicated}},
		Profiles: []device.Profile{{ID: "home", DefaultPolicies: []string{"Present", "Gone"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = device.ResolvePolicyBundle(bundle, device.PolicyResolution{AvailableTargets: []string{"Present"}, Selections: map[string]string{"device/skipped/default": "Gone"}})
	if err != nil {
		t.Fatal(err)
	}
	paths := runtime.NewPaths(cfg)
	if err := device.WritePolicyBundleSnapshot(paths.DevicePolicyApplied, bundle); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveState(paths.StateFile, runtime.State{DevicePolicyDigest: bundle.Digest}); err != nil {
		t.Fatal(err)
	}
	selected, unavailable := devicePolicySelections(cfg)
	if unavailable || selected["device/phone/default"] != "Present" || selected["device/skipped/default"] != "Gone" {
		t.Fatalf("selected=%v unavailable=%t", selected, unavailable)
	}
}

func TestPreparedPolicyReadsKeepOmittedSelectorDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{}}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Runtime.Dir, cfg.Mihomo.Config = dir, filepath.Join(dir, "mihomo.yaml")
	cfg.DevicePolicy.File = filepath.Join(dir, "device.json")
	cfg.UpstreamProxy.Enabled = true
	cfg.UpstreamProxy.Name = "Present"
	policy := `{"devices":[{"id":"phone","mac":"aa:bb:cc:dd:ee:01","ipv4":"192.168.50.101","profile":"home","egress_mode":"dedicated"}],"profiles":[{"id":"home","default_policies":["Present","Gone"]}]}`
	if err := os.WriteFile(cfg.DevicePolicy.File, []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	state := PreparedState{BootSessionID: "test", StartedAt: time.Now(), APIAddr: strings.TrimPrefix(server.URL, "http://"), Secret: "test", DevicePolicyResolution: &device.PolicyResolution{AvailableTargets: []string{"Present"}, Selections: map[string]string{"device/phone/default": "Gone"}}}
	if err := os.MkdirAll(filepath.Dir(preparedStatePath(cfg)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := savePrepared(cfg, state); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		candidate := cfg
		if err := PrepareDevicePolicy(&candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.DevicePolicy.Bundle.Compiled.Devices[0].EgressMode != device.EgressModeInheritGlobal {
			t.Fatal("reading the prepared core resurrected an omitted selector")
		}
		state.DevicePolicyResolution = candidate.DevicePolicy.Bundle.Resolution
		if err := savePrepared(cfg, state); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrepareDevicePolicyAndRenderShareResolvedSnapshot(t *testing.T) {
	for _, selected := range []string{"Present", "Missing"} {
		t.Run(selected, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Default()
			cfg.Runtime.Dir, cfg.Mihomo.Config = dir, filepath.Join(dir, "mihomo.yaml")
			cfg.Mihomo.ProfileMode, cfg.Mihomo.Profile = config.MihomoProfileModeImported, filepath.Join(dir, "profile.yaml")
			cfg.DevicePolicy.File = filepath.Join(dir, "devices.json")
			cfg.Transparent.TUNIPv6 = config.TUNIPv6Always
			profile := "proxy-groups:\n  - name: Present\n    type: select\n    proxies: [DIRECT]\nrules: ['MATCH,Present']\n"
			if err := os.WriteFile(cfg.Mihomo.Profile, []byte(profile), 0o600); err != nil {
				t.Fatal(err)
			}
			policy := device.PolicySet{
				Devices: []device.ManagedDevice{{ID: "phone", MAC: "aa:bb:cc:dd:ee:01", IPv4: "192.168.50.101", Profile: "home", EgressMode: device.EgressModeDedicated}},
				Profiles: []device.Profile{{ID: "home", DefaultPolicies: []string{"Present", "Missing"}, Rules: []device.Rule{
					{ID: "template", Match: device.RuleMatch{Template: "media"}, Action: "Missing"},
					{ID: "ruleset", Match: device.RuleMatch{RuleSets: []string{"media"}}, Action: "Missing"},
					{ID: "valid", Match: device.RuleMatch{Domains: []string{"kept.example"}}, Action: "REJECT"},
				}}},
				Templates: []device.Template{{ID: "media", RuleSets: []string{"media"}}},
				RuleSets:  []device.RuleSet{{ID: "media", Behavior: "domain", Payload: []string{"media.example"}}},
			}
			data, _ := json.Marshal(policy)
			if err := os.WriteFile(cfg.DevicePolicy.File, data, 0o600); err != nil {
				t.Fatal(err)
			}
			writeSelectionCache(t, filepath.Join(dir, "cache.db"), map[string]string{"device/phone/default": selected})
			if err := PrepareDevicePolicy(&cfg); err != nil {
				t.Fatal(err)
			}
			effective := cfg.DevicePolicy.Bundle.Compiled.Devices[0]
			wantMode := device.EgressModeDedicated
			if selected == "Missing" {
				wantMode = device.EgressModeInheritGlobal
			}
			if effective.EgressMode != wantMode {
				t.Fatalf("device = %#v", effective)
			}
			rendered, err := RenderConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"- \"Missing\"", ",Missing", "open-surge-ruleset-media", "device/phone/template", "device/phone/ruleset"} {
				if strings.Contains(rendered, forbidden) {
					t.Fatalf("rendered unavailable route %q", forbidden)
				}
			}
			if strings.Contains(rendered, "device/phone/default") != (selected == "Present") {
				t.Fatalf("wrong default route: %s", rendered)
			}
			for _, want := range []string{"MATCH,Present", "DOMAIN-SUFFIX,kept.example", "IN-USER,device:phone", "\"aa:bb:cc:dd:ee:01\": \"device:phone\""} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("lost valid rule or IPv6 identity: %s", want)
				}
			}
			// The applied artifact must keep the exact resolution even if the
			// mutable core cache changes between preparation and rendering.
			writeSelectionCache(t, filepath.Join(dir, "cache.db"), map[string]string{"device/phone/default": "Present"})
			second, err := RenderConfig(cfg)
			if err != nil || second != rendered {
				t.Fatal("renderer re-read mutable selection state")
			}
		})
	}
}
