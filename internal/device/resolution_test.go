package device

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"open-mihomo-gateway/internal/lan"
)

func resolutionFixture() PolicySet {
	return PolicySet{
		Devices: []ManagedDevice{{ID: "phone", MAC: "aa:bb:cc:dd:ee:01", IPv4: "192.168.50.101", Profile: "home", EgressMode: EgressModeDedicated}},
		Profiles: []Profile{{ID: "home", DefaultPolicies: []string{"Present", "Gone"}, Rules: []Rule{
			{ID: "template", Match: RuleMatch{Template: "media"}, Action: "Gone"},
			{ID: "ruleset", Match: RuleMatch{RuleSets: []string{"media"}}, Policies: []string{"Gone", "Present"}},
			{ID: "domain", Match: RuleMatch{Domains: []string{"gone.example"}}, Action: "Gone"},
			{ID: "ip", Match: RuleMatch{IPCIDRs: []string{"203.0.113.0/24"}}, Action: "Gone"},
			{ID: "valid", Match: RuleMatch{Domains: []string{"kept.example"}}, Action: "REJECT"},
		}}},
		RuleSets:  []RuleSet{{ID: "media", Behavior: "domain", Payload: []string{"media.example"}}},
		Templates: []Template{{ID: "media", RuleSets: []string{"media"}}},
	}
}

func TestResolvePolicyBundleMissingSelectedDefaultFollowsGatewayAndSkipsUnavailableRoutes(t *testing.T) {
	set := resolutionFixture()
	before, _ := json.Marshal(set)
	bundle, err := CompilePolicyBundle(set)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolvePolicyBundle(bundle, PolicyResolution{AvailableTargets: []string{"Present"}, Selections: map[string]string{"device/phone/default": "Gone"}})
	if err != nil {
		t.Fatal(err)
	}
	current := resolved.Compiled.Devices[0]
	if current.EgressMode != EgressModeInheritGlobal || current.ConfiguredEgressMode != EgressModeDedicated || len(current.Groups) != 0 {
		t.Fatalf("effective device = %#v", current)
	}
	if len(current.PolicyAdjustments) != 5 || current.PolicyAdjustments[0].Effect != PolicyEffectInheritGlobal {
		t.Fatalf("adjustments = %#v", current.PolicyAdjustments)
	}
	if len(resolved.Compiled.SelectorGroups) != 0 || len(resolved.Compiled.RuleProviders) != 0 || len(resolved.Compiled.DedicatedRules) != 0 || len(resolved.Compiled.DefaultRules) != 0 {
		t.Fatalf("stale compiled routes = %#v", resolved.Compiled)
	}
	if !reflect.DeepEqual(resolved.Compiled.OverrideRules, []string{"AND,((SRC-IP-CIDR,192.168.50.101/32),(DOMAIN-SUFFIX,kept.example)),REJECT"}) {
		t.Fatalf("remaining override rules = %v", resolved.Compiled.OverrideRules)
	}
	if !reflect.DeepEqual(resolved.Compiled.Reservations, bundle.Compiled.Reservations) {
		t.Fatal("fallback changed DHCP reservations")
	}
	after, _ := json.Marshal(set)
	if string(before) != string(after) || resolved.Digest != bundle.Digest || !reflect.DeepEqual(bundle.Policy, resolved.Policy) {
		t.Fatal("resolution changed desired settings or digest")
	}
	if bundle.Compiled.Devices[0].EgressMode != EgressModeDedicated {
		t.Fatal("resolution mutated original compiled bundle")
	}

	path := filepath.Join(t.TempDir(), "applied.json")
	if err := WritePolicyBundleSnapshot(path, resolved); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPolicyBundleSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Compiled, resolved.Compiled) {
		t.Fatalf("applied snapshot resurrected unavailable routes: %#v", loaded.Compiled)
	}
	restored, err := ResolvePolicyBundle(loaded, PolicyResolution{AvailableTargets: []string{"Gone", "Present"}, Selections: loaded.Resolution.Selections})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Compiled, bundle.Compiled) {
		t.Fatalf("restored source did not restore original routes: %#v", restored.Compiled)
	}
}

func TestResolvePolicyBundleSelectorChoices(t *testing.T) {
	for _, tt := range []struct {
		name         string
		candidates   []string
		selected     string
		unavailable  bool
		wantMode     string
		wantPolicies []string
	}{
		{"unused missing candidate", []string{"Gone", "Present"}, "Present", false, EgressModeDedicated, []string{"Present"}},
		{"selected missing with valid alternative", []string{"Present", "Gone"}, "Gone", false, EgressModeInheritGlobal, nil},
		{"fresh selector first missing", []string{"Gone", "Present"}, "", false, EgressModeInheritGlobal, nil},
		{"fresh selector first valid", []string{"Present", "Gone"}, "", false, EgressModeDedicated, []string{"Present"}},
		{"all candidates missing", []string{"Gone", "AlsoGone"}, "Gone", false, EgressModeInheritGlobal, nil},
		{"explicit edit removed old choice", []string{"Present", "Gone"}, "Removed", false, EgressModeDedicated, []string{"Present"}},
		{"unknown cached choice", []string{"Present", "Gone"}, "", true, EgressModeInheritGlobal, nil},
		{"builtins preserved", []string{"DIRECT", "REJECT", "Gone"}, "REJECT", false, EgressModeDedicated, []string{"REJECT", "DIRECT"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := resolutionFixture()
			set.Profiles[0].DefaultPolicies, set.Profiles[0].Rules = tt.candidates, nil
			bundle, err := CompilePolicyBundle(set)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := ResolvePolicyBundle(bundle, PolicyResolution{AvailableTargets: []string{"Present"}, Selections: map[string]string{"device/phone/default": tt.selected}, SelectionsUnavailable: tt.unavailable})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Compiled.Devices[0].EgressMode != tt.wantMode {
				t.Fatalf("device = %#v", resolved.Compiled.Devices[0])
			}
			if tt.wantPolicies == nil {
				if len(resolved.Compiled.SelectorGroups) != 0 {
					t.Fatal("fallback still has a selector")
				}
			} else if !slices.Equal(resolved.Compiled.SelectorGroups[0].Policies, tt.wantPolicies) {
				t.Fatalf("policies = %v", resolved.Compiled.SelectorGroups)
			}
		})
	}
}

func TestResolvePolicyBundleRulesetKeepsValidChoiceAndOtherDevicesIndependent(t *testing.T) {
	set := resolutionFixture()
	set.Devices = append(set.Devices, ManagedDevice{ID: "tv", MAC: "aa:bb:cc:dd:ee:02", IPv4: "192.168.50.102", Profile: "home", EgressMode: EgressModeDedicated})
	bundle, err := CompilePolicyBundle(set)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolvePolicyBundle(bundle, PolicyResolution{AvailableTargets: []string{"Present"}, Selections: map[string]string{"device/phone/default": "Gone", "device/phone/ruleset": "Present", "device/tv/default": "Present"}})
	if err != nil {
		t.Fatal(err)
	}
	phone, tv := resolved.Compiled.Devices[0], resolved.Compiled.Devices[1]
	if phone.EgressMode != EgressModeInheritGlobal || phone.Groups["ruleset"] == "" || tv.EgressMode != EgressModeDedicated || tv.Groups["ruleset"] != "" {
		t.Fatalf("phone=%#v tv=%#v", phone, tv)
	}
	if len(resolved.Compiled.RuleProviders) != 1 {
		t.Fatal("valid ruleset binding lost its provider")
	}
	if !strings.Contains(strings.Join(resolved.Compiled.OverrideRules, "\n"), "device/phone/ruleset") {
		t.Fatal("valid ruleset binding was skipped")
	}
}

func TestResolvePolicyBundlePreservesInactiveAndRouterBypassIdentities(t *testing.T) {
	set := resolutionFixture()
	set.Devices[0].GatewayTarget = GatewayTargetUpstreamRouter
	set.Devices = append(set.Devices,
		ManagedDevice{ID: "outside", MAC: "aa:bb:cc:dd:ee:02", IPv4: "192.168.60.101", Profile: "home", EgressMode: EgressModeDedicated},
		ManagedDevice{ID: "no-mac", IPv4: "192.168.50.102", Profile: "home", EgressMode: EgressModeDedicated})
	scope, _ := lan.NewScope("192.168.50.1", 24)
	bundle, err := CompilePolicyBundleForLAN(set, scope, false)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolvePolicyBundle(bundle, PolicyResolution{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle.Compiled, resolved.Compiled) {
		t.Fatal("resolution changed inactive or bypass identities")
	}
}
