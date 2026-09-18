package projects

import (
	"errors"
	"reflect"
	"testing"
)

func TestProjectInputContract(t *testing.T) {
	for _, key := range []string{
		"00000000-0000-0000-0000-000000000000",
		"10000000-0000-1000-0000-000000000001",
		"60000000-0000-4000-8000-000000000001",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	} {
		if !validKey(key) {
			t.Fatalf("valid UUID rejected: %s", key)
		}
	}
	for _, key := range []string{"", "not-a-uuid", "60000000-0000-4000-g000-000000000001"} {
		if validKey(key) {
			t.Fatalf("invalid UUID accepted: %q", key)
		}
	}

	raw, err := ParseRawInput([]byte(`{"expectedProjectRevision":"60000000-0000-4000-8000-000000000001","name":"updated"}`))
	if err != nil {
		t.Fatal(err)
	}
	command, err := PrepareUpdate(raw, "project-项目", "AAAAAAAA-AAAA-1AAA-0AAA-AAAAAAAAAAAA")
	if err != nil || command.projectID != "project-项目" || command.key != "aaaaaaaa-aaaa-1aaa-0aaa-aaaaaaaaaaaa" {
		t.Fatalf("opaque project or UUID normalization failed: command=%+v err=%v", command, err)
	}

	raw, err = ParseRawInput([]byte(`{"purpose":"p","repositoryIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = parseCreate(raw, 1)
	var failure *Failure
	if !errors.As(err, &failure) || !reflect.DeepEqual(failure.FieldErrors, []FieldError{{Field: "name", Code: "REQUIRED"}}) {
		t.Fatalf("missing name field errors=%+v err=%v", failure, err)
	}

	raw, err = ParseRawInput([]byte(`{"name":null,"purpose":"p","repositoryIds":["repo_00000000000000000001"]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = parseCreate(raw, 1)
	if !errors.As(err, &failure) || !reflect.DeepEqual(failure.FieldErrors, []FieldError{{Field: "name", Code: "INVALID"}}) {
		t.Fatalf("invalid name field errors=%+v err=%v", failure, err)
	}
}
