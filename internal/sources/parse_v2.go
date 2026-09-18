package sources

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/projects"
)

type v2WireRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type v2WireExecution struct {
	ExecutionProfile
	BudgetPolicyRef    *v2WireRef `json:"budgetPolicyRef"`
	TimeLimitPolicyRef *v2WireRef `json:"timeLimitPolicyRef"`
}

type v2WireBudget struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	ScopeKind     string `json:"scopeKind"`
	Unit          string `json:"unit"`
	Period        string `json:"period"`
	Limit         int64  `json:"limit"`
	MaxUnresolved int64  `json:"maxUnresolved"`
	Enabled       bool   `json:"enabled"`
}

type v2WireTimeLimit struct {
	ID                   string `json:"id"`
	Version              string `json:"version"`
	ModelRequestSeconds  int64  `json:"modelRequestTimeoutSeconds"`
	WorkerAttemptSeconds int64  `json:"workerAttemptLimitSeconds"`
}

type v2WireEgress struct {
	ID                    string   `json:"id"`
	Version               string   `json:"version"`
	ApprovedBaseURLs      []string `json:"approvedBaseUrls"`
	AllowedPort           int      `json:"allowedPort"`
	AllowPrivateAddresses bool     `json:"allowPrivateAddresses"`
	FollowRedirects       bool     `json:"followRedirects"`
}

type v2WireTestBinding struct {
	OwnerID         string     `json:"ownerId"`
	BudgetPolicyRef *v2WireRef `json:"budgetPolicyRef"`
	LimitsPolicyRef *v2WireRef `json:"timeLimitPolicyRef"`
	EgressPolicyRef *v2WireRef `json:"egressPolicyRef"`
}

type v2WireManifest struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	ImportID             string                `json:"importId"`
	EnvironmentTemplates []EnvironmentTemplate `json:"environmentTemplates"`
	ExecutionProfiles    []v2WireExecution     `json:"executionProfiles"`
	DefaultBindings      []DefaultBinding      `json:"defaultBindings"`
	BudgetPolicies       []v2WireBudget        `json:"budgetPolicies"`
	TimeLimitPolicies    []v2WireTimeLimit     `json:"timeLimitPolicies"`
	EgressPolicies       []v2WireEgress        `json:"egressPolicies"`
	TestBindings         []v2WireTestBinding   `json:"testBindings"`
}

func parseImportV2(data []byte, fields map[string]json.RawMessage) (ImportCommand, error) {
	allowed := map[string]bool{
		"schemaVersion": true, "importId": true, "environmentTemplates": true,
		"executionProfiles": true, "defaultBindings": true,
		"budgetPolicies": true, "timeLimitPolicies": true,
		"egressPolicies": true, "testBindings": true,
	}
	for key := range fields {
		if !allowed[key] {
			return ImportCommand{}, failure(2, "UNKNOWN_FIELD")
		}
	}
	var wire v2WireManifest
	if json.Unmarshal(data, &wire) != nil {
		return ImportCommand{}, failure(2, "INVALID_JSON")
	}
	if wire.SchemaVersion != 2 {
		return ImportCommand{}, failure(2, "UNSUPPORTED_SCHEMA")
	}
	wire.ImportID = strings.ToLower(wire.ImportID)
	if !uuidPattern.MatchString(wire.ImportID) {
		return ImportCommand{}, failure(2, "INVALID_IMPORT_ID")
	}
	if len(wire.EnvironmentTemplates) == 0 || len(wire.ExecutionProfiles) == 0 {
		return ImportCommand{}, failure(2, "VALIDATION_FAILED")
	}
	seenTemplate := map[string]bool{}
	for _, template := range wire.EnvironmentTemplates {
		if !validID(template.ID) || !validID(template.Version) || !validID(template.ExecutorPoolID) || !validID(template.NetworkPolicyRef) || !validID(template.ResourceClassID) || !digestPattern.MatchString(template.ApprovedTemplateDigest) {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		key := template.ID + "\x00" + template.Version
		if seenTemplate[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenTemplate[key] = true
	}
	seenPolicy := map[string]bool{}
	budgets := make(map[string]projects.RequestPolicy, len(wire.BudgetPolicies))
	manifest := ManifestV2{Common: ManifestCommon{SchemaVersion: 2, ImportID: wire.ImportID}}
	manifest.Common.EnvironmentTemplates = wire.EnvironmentTemplates
	for _, b := range wire.BudgetPolicies {
		if !validID(b.ID) || !validID(b.Version) || b.ScopeKind != "actor_model_test" && b.ScopeKind != "project_model_runtime" || b.Unit != "request" || b.Period != "utc_day" || b.Limit < 1 || b.Limit > 2147483647 || b.MaxUnresolved < 1 {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		key := b.ID + "\x00" + b.Version
		if seenPolicy[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenPolicy[key] = true
		manifest.Budgets = append(manifest.Budgets, projects.RequestPolicy{Ref: projects.PolicyRef{ID: b.ID, Version: b.Version}, Scope: b.ScopeKind, Limit: b.Limit, MaxUnresolved: b.MaxUnresolved, Enabled: b.Enabled})
		budgets[key] = manifest.Budgets[len(manifest.Budgets)-1]
	}
	for _, t := range wire.TimeLimitPolicies {
		if !validID(t.ID) || !validID(t.Version) || t.ModelRequestSeconds < 5 || t.ModelRequestSeconds > 120 || t.WorkerAttemptSeconds < 60 || t.WorkerAttemptSeconds > 86400 {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		key := t.ID + "\x00" + t.Version
		if seenPolicy[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenPolicy[key] = true
		manifest.Limits = append(manifest.Limits, projects.TimePolicy{Ref: projects.PolicyRef{ID: t.ID, Version: t.Version}, ModelRequestSeconds: t.ModelRequestSeconds, WorkerAttemptSeconds: t.WorkerAttemptSeconds})
	}
	egresses := make(map[string]projects.PolicyRef, len(wire.EgressPolicies))
	for _, e := range wire.EgressPolicies {
		if !validID(e.ID) || !validID(e.Version) || len(e.ApprovedBaseURLs) == 0 || e.AllowedPort != 443 || e.AllowPrivateAddresses || e.FollowRedirects {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenURL := map[string]bool{}
		for _, raw := range e.ApprovedBaseURLs {
			if !validEgressBaseURL(raw) {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			if seenURL[raw] {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			seenURL[raw] = true
		}
		key := e.ID + "\x00" + e.Version
		if seenPolicy[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenPolicy[key] = true
		manifest.Egress = append(manifest.Egress, EgressPolicy{ID: e.ID, Version: e.Version, ApprovedBaseURLs: e.ApprovedBaseURLs, AllowedPort: e.AllowedPort, AllowPrivateAddresses: e.AllowPrivateAddresses, FollowRedirects: e.FollowRedirects})
		egresses[key] = projects.PolicyRef{ID: e.ID, Version: e.Version}
	}
	seenProfile := map[string]bool{}
	owners := map[string]bool{}
	for _, p := range wire.ExecutionProfiles {
		profile := p.ExecutionProfile
		if !validID(profile.ID) || !validID(profile.Version) || !validID(profile.OwnerID) || !validName(profile.Name) || !validID(profile.TemplateID) || !validID(profile.TemplateVersion) || profile.WorkerConcurrency < 1 || profile.WorkerConcurrency > 16 {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		if p.BudgetPolicyRef == nil || p.TimeLimitPolicyRef == nil {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		if !validID(p.BudgetPolicyRef.ID) || !validID(p.BudgetPolicyRef.Version) || !validID(p.TimeLimitPolicyRef.ID) || !validID(p.TimeLimitPolicyRef.Version) {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		budget, ok := budgets[p.BudgetPolicyRef.ID+"\x00"+p.BudgetPolicyRef.Version]
		if !ok || budget.Scope != "project_model_runtime" {
			return ImportCommand{}, failure(2, "UNKNOWN_POLICY")
		}
		if _, ok := seenPolicy[p.TimeLimitPolicyRef.ID+"\x00"+p.TimeLimitPolicyRef.Version]; !ok {
			return ImportCommand{}, failure(2, "UNKNOWN_POLICY")
		}
		if !seenTemplate[profile.TemplateID+"\x00"+profile.TemplateVersion] {
			return ImportCommand{}, failure(2, "UNKNOWN_TEMPLATE")
		}
		key := profile.ID + "\x00" + profile.Version
		if seenProfile[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenProfile[key] = true
		owners[profile.OwnerID] = true
		manifest.ExecutionProfiles = append(manifest.ExecutionProfiles, CompleteExecution{Identity: profile, Budget: projects.PolicyRef{ID: p.BudgetPolicyRef.ID, Version: p.BudgetPolicyRef.Version}, Limits: projects.PolicyRef{ID: p.TimeLimitPolicyRef.ID, Version: p.TimeLimitPolicyRef.Version}})
	}
	if len(wire.TestBindings) > 0 {
		seenOwner := map[string]bool{}
		for _, tb := range wire.TestBindings {
			if !validID(tb.OwnerID) || tb.BudgetPolicyRef == nil || tb.LimitsPolicyRef == nil || tb.EgressPolicyRef == nil {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			if !validID(tb.BudgetPolicyRef.ID) || !validID(tb.BudgetPolicyRef.Version) || !validID(tb.LimitsPolicyRef.ID) || !validID(tb.LimitsPolicyRef.Version) || !validID(tb.EgressPolicyRef.ID) || !validID(tb.EgressPolicyRef.Version) {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			budget, ok := budgets[tb.BudgetPolicyRef.ID+"\x00"+tb.BudgetPolicyRef.Version]
			if !ok || budget.Scope != "actor_model_test" {
				return ImportCommand{}, failure(2, "UNKNOWN_POLICY")
			}
			if _, ok := seenPolicy[tb.LimitsPolicyRef.ID+"\x00"+tb.LimitsPolicyRef.Version]; !ok {
				return ImportCommand{}, failure(2, "UNKNOWN_POLICY")
			}
			if _, ok := egresses[tb.EgressPolicyRef.ID+"\x00"+tb.EgressPolicyRef.Version]; !ok {
				return ImportCommand{}, failure(2, "UNKNOWN_POLICY")
			}
			if seenOwner[tb.OwnerID] {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			seenOwner[tb.OwnerID] = true
			manifest.Tests = append(manifest.Tests, TestBinding{OwnerID: tb.OwnerID, Budget: projects.PolicyRef{ID: tb.BudgetPolicyRef.ID, Version: tb.BudgetPolicyRef.Version}, Limits: projects.PolicyRef{ID: tb.LimitsPolicyRef.ID, Version: tb.LimitsPolicyRef.Version}, Egress: projects.PolicyRef{ID: tb.EgressPolicyRef.ID, Version: tb.EgressPolicyRef.Version}})
			owners[tb.OwnerID] = true
		}
	}
	if len(wire.DefaultBindings) > 0 {
		seenDefault := map[string]bool{}
		for _, binding := range wire.DefaultBindings {
			if binding.Kind != "execution" || !validID(binding.OwnerID) || !validID(binding.ProfileID) || !validID(binding.ProfileVersion) {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			if !seenProfile[binding.ProfileID+"\x00"+binding.ProfileVersion] {
				return ImportCommand{}, failure(2, "UNKNOWN_PROFILE")
			}
			key := binding.Kind + "\x00" + binding.OwnerID
			if seenDefault[key] {
				return ImportCommand{}, failure(2, "VALIDATION_FAILED")
			}
			seenDefault[key] = true
			manifest.Common.DefaultBindings = append(manifest.Common.DefaultBindings, binding)
			owners[binding.OwnerID] = true
		}
	}
	_ = owners
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ImportCommand{}, unavailable()
	}
	return ImportCommand{payload: manifest, canonical: canonical, schemaVersion: 2}, nil
}

func validEgressBaseURL(value string) bool {
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > 512 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return false
	}
	host := parsed.Hostname()
	if strings.Contains(host, ":") {
		return false
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return false
	}
	return true
}
