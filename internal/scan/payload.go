package scan

// The single AutoCard serializer. Storage, API views and fixtures all go
// through these two functions; adding a card field means editing only here
// (the Python port's lesson: four hand-copied field lists drift, and one of
// them silently lost a field).

func autoCardPayload(card AutoCard) map[string]any {
	return map[string]any{
		"topDirs":          card.TopDirs,
		"deps":             card.Deps,
		"depEvidence":      depEvidencePayload(card.DepEvidence),
		"identities":       card.Identities,
		"deployIdentities": card.DeployIdentities,
		"recentCommits":    card.RecentCommits,
		"exposedApis":      card.ExposedAPIs,
		"lowSignal":        card.LowSignal,
	}
}

func autoCardFromPayload(raw map[string]any) *AutoCard {
	// An empty payload (hand-registered row) means "no card", not "empty card".
	if len(raw) == 0 {
		return nil
	}
	return &AutoCard{
		TopDirs:          stringSlice(raw["topDirs"]),
		Deps:             stringSlice(raw["deps"]),
		DepEvidence:      depEvidenceFromPayload(raw["depEvidence"]),
		Identities:       stringSlice(raw["identities"]),
		DeployIdentities: stringSlice(raw["deployIdentities"]),
		RecentCommits:    stringSlice(raw["recentCommits"]),
		ExposedAPIs:      stringSlice(raw["exposedApis"]),
		LowSignal:        boolValue(raw["lowSignal"]),
	}
}

func depEvidencePayload(evidence []DepEvidence) []map[string]any {
	out := make([]map[string]any, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, map[string]any{
			"name":       item.Name,
			"mechanism":  string(item.Mechanism),
			"confidence": string(item.Confidence),
		})
	}
	return out
}

func depEvidenceFromPayload(raw any) []DepEvidence {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]DepEvidence, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		mechanism, _ := entry["mechanism"].(string)
		confidence, _ := entry["confidence"].(string)
		out = append(out, DepEvidence{
			Name:       name,
			Mechanism:  Mechanism(mechanism),
			Confidence: Confidence(confidence),
		})
	}
	return out
}

func stringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func boolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}
