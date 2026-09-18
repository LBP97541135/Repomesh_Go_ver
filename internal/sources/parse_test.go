package sources

import (
	"errors"
	"testing"
)

func TestParseImportSchema1(t *testing.T) {
	command, err := ParseImport([]byte(`{
		"schemaVersion":1,
		"importId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"environmentTemplates":[{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}],
		"executionProfiles":[{"id":"exec","version":"v1","ownerId":"owner-1","name":"执行","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false}],
		"defaultBindings":[{"kind":"execution","ownerId":"owner-1","profileId":"exec","profileVersion":"v1"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := command.payload.(Manifest)
	if !ok || manifest.ImportID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || len(command.canonical) == 0 {
		t.Fatal("import command incomplete")
	}
	_, err = ParseImport([]byte(`{"schemaVersion":1,"importId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","actor":"self","environmentTemplates":[{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}],"executionProfiles":[{"id":"exec","version":"v1","ownerId":"owner-1","name":"执行","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false}],"defaultBindings":[{"kind":"execution","ownerId":"owner-1","profileId":"exec","profileVersion":"v1"}]}`))
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "UNKNOWN_FIELD" {
		t.Fatalf("self-reported actor: %v", err)
	}
	_, err = ParseImport([]byte(`{"schemaVersion":1,"importId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","extra":true,"environmentTemplates":[],"executionProfiles":[],"defaultBindings":[]}`))
	if !errors.As(err, &failure) || failure.Code != "UNKNOWN_FIELD" {
		t.Fatalf("unknown field: %v", err)
	}
}
