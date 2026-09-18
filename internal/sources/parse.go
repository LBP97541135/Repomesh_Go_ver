package sources

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/jsoninput"
)

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func ParseImport(data []byte) (ImportCommand, error) {
	data = bytes.TrimSpace(data)
	if jsoninput.Validate(data, 1024*1024) != nil {
		return ImportCommand{}, failure(2, "INVALID_JSON")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return ImportCommand{}, failure(2, "INVALID_JSON")
	}
	var version struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if json.Unmarshal(data, &version) != nil {
		return ImportCommand{}, failure(2, "INVALID_JSON")
	}
	switch version.SchemaVersion {
	case 1:
		return parseImportV1(data, fields)
	case 2:
		return parseImportV2(data, fields)
	default:
		return ImportCommand{}, failure(2, "UNSUPPORTED_SCHEMA")
	}
}

func parseImportV1(data []byte, fields map[string]json.RawMessage) (ImportCommand, error) {
	allowed := map[string]bool{
		"schemaVersion": true, "importId": true, "environmentTemplates": true,
		"executionProfiles": true, "defaultBindings": true,
	}
	for key := range fields {
		if !allowed[key] {
			return ImportCommand{}, failure(2, "UNKNOWN_FIELD")
		}
	}
	var manifest Manifest
	if json.Unmarshal(data, &manifest) != nil {
		return ImportCommand{}, failure(2, "INVALID_JSON")
	}
	if manifest.SchemaVersion != 1 {
		return ImportCommand{}, failure(2, "UNSUPPORTED_SCHEMA")
	}
	manifest.ImportID = strings.ToLower(manifest.ImportID)
	if !uuidPattern.MatchString(manifest.ImportID) {
		return ImportCommand{}, failure(2, "INVALID_IMPORT_ID")
	}
	if len(manifest.EnvironmentTemplates) == 0 || len(manifest.ExecutionProfiles) == 0 {
		return ImportCommand{}, failure(2, "VALIDATION_FAILED")
	}
	seenTemplate := map[string]bool{}
	for _, template := range manifest.EnvironmentTemplates {
		if !validID(template.ID) || !validID(template.Version) || !validID(template.ExecutorPoolID) || !validID(template.NetworkPolicyRef) || !validID(template.ResourceClassID) || !digestPattern.MatchString(template.ApprovedTemplateDigest) {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		key := template.ID + "\x00" + template.Version
		if seenTemplate[key] {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
		}
		seenTemplate[key] = true
	}
	seenProfile := map[string]bool{}
	owners := map[string]bool{}
	for _, profile := range manifest.ExecutionProfiles {
		if !validID(profile.ID) || !validID(profile.Version) || !validID(profile.OwnerID) || !validName(profile.Name) || !validID(profile.TemplateID) || !validID(profile.TemplateVersion) || profile.WorkerConcurrency < 1 || profile.WorkerConcurrency > 16 {
			return ImportCommand{}, failure(2, "VALIDATION_FAILED")
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
	}
	if len(manifest.DefaultBindings) == 0 {
		return ImportCommand{}, failure(2, "VALIDATION_FAILED")
	}
	seenDefault := map[string]bool{}
	for _, binding := range manifest.DefaultBindings {
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
		owners[binding.OwnerID] = true
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ImportCommand{}, unavailable()
	}
	return ImportCommand{payload: manifest, canonical: canonical, schemaVersion: 1}, nil
}

func validID(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128 && value != "." && value != ".." && !strings.ContainsAny(value, "/%\\?#\x00")
}

func validName(value string) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= 1 && length <= 200
}
