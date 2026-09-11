package device

import (
	"maps"
	"net"
	"slices"
	"strings"
)

const (
	PolicyEffectInheritGlobal    = "inherit_global"
	PolicyEffectSkipRule         = "skip_rule"
	PolicyEffectFilterCandidates = "filter_candidates"
)

// PolicyResolution freezes the target inventory and selector choices used by
// one run. The original declarative policy and its digest remain unchanged.
// Snapshots recompile with this context, never with a later subscription.
type PolicyResolution struct {
	AvailableTargets      []string          `json:"available_targets"`
	Selections            map[string]string `json:"selections,omitempty"`
	SelectionsUnavailable bool              `json:"selections_unavailable,omitempty"`
}

type PolicyAdjustment struct {
	Slot           string   `json:"slot"`
	Effect         string   `json:"effect"`
	MissingTargets []string `json:"missing_targets"`
	Selected       string   `json:"selected,omitempty"`
}

// ResolvePolicyBundle derives effective routes without rewriting registrations,
// profiles, templates or rule sets. Inactive identities stay inactive.
func ResolvePolicyBundle(bundle PolicyBundle, resolution PolicyResolution) (PolicyBundle, error) {
	resolution.AvailableTargets = slices.Clone(resolution.AvailableTargets)
	slices.Sort(resolution.AvailableTargets)
	resolution.AvailableTargets = slices.Compact(resolution.AvailableTargets)
	resolution.Selections = maps.Clone(resolution.Selections)
	active := bundle.Policy
	if bundle.ActiveLAN != "" {
		_, network, err := net.ParseCIDR(bundle.ActiveLAN)
		if err != nil {
			return PolicyBundle{}, err
		}
		active = activePolicySetForNetwork(active, network)
	}
	compiled, err := compilePolicySet(active, bundle.IPOnlyDevicesActive, &resolution)
	if err != nil {
		return PolicyBundle{}, err
	}
	bundle.Compiled, bundle.Resolution = compiled, &resolution
	return bundle, nil
}

func (r *PolicyResolution) targetAvailable(target string) bool {
	if r == nil {
		return true
	}
	switch strings.ToUpper(target) {
	case "DIRECT", "REJECT", "REJECT-DROP", "REJECT-TINYGIF":
		return true
	}
	return slices.Contains(r.AvailableTargets, target)
}

func (r *PolicyResolution) resolveSelector(deviceID, slot string, candidates []string) ([]string, *PolicyAdjustment) {
	if r == nil {
		return slices.Clone(candidates), nil
	}
	available, missing := []string{}, []string{}
	for _, candidate := range candidates {
		if r.targetAvailable(candidate) {
			available = append(available, candidate)
		} else {
			missing = append(missing, candidate)
		}
	}
	if len(missing) == 0 {
		return available, nil
	}
	selected := r.Selections[DeviceGroupName(deviceID, slot)]
	selectionUnknown := selected == "" && r.SelectionsUnavailable
	// A deliberate candidate edit can remove an old selection. In that case
	// use the first configured candidate, just like a new mihomo selector.
	if !slices.Contains(candidates, selected) && len(candidates) > 0 {
		selected = candidates[0]
	}
	adjustment := &PolicyAdjustment{Slot: slot, Effect: PolicyEffectFilterCandidates, MissingTargets: missing, Selected: selected}
	if len(available) == 0 || selectionUnknown || !r.targetAvailable(selected) {
		adjustment.Effect = PolicyEffectSkipRule
		if slot == "default" {
			adjustment.Effect = PolicyEffectInheritGlobal
		}
		return nil, adjustment
	}
	// Keep the known effective choice even if candidate composition moves the
	// core to a different cache directory.
	if index := slices.Index(available, selected); index > 0 {
		available = append([]string{selected}, append(available[:index], available[index+1:]...)...)
	}
	return available, adjustment
}
