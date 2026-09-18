package jsoninput

import "testing"

func TestRejectAmbiguousJSON(t *testing.T) {
	for _, input := range []string{`{"id":"a","id":"b"}`, `{"id":"a","\u0069d":"b"}`, `{"id":"\ud800"}`, `{"id":"\udc00"}`, `{"id":"\ud800x\udc00"}`, `{"id":"a","actor":"b"}`, `{"id":"a"} {}`, "{\"id\":\"\xff\"}"} {
		t.Run(input, func(t *testing.T) {
			var target struct {
				ID string `json:"id"`
			}
			if Decode([]byte(input), &target, 1024) == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	var target struct {
		ID string `json:"id"`
	}
	if err := Decode([]byte(`{"id":"\ud83d\ude00"}`), &target, 1024); err != nil || target.ID != "😀" {
		t.Fatal("valid surrogate pair rejected", err)
	}
}

func TestValidatePreservesBusinessShape(t *testing.T) {
	for _, input := range []string{`{"unknown":true}`, `[]`, `null`, `"text"`, `42`} {
		if err := Validate([]byte(input), 1024); err != nil {
			t.Fatalf("Validate(%s): %v", input, err)
		}
	}
	for _, input := range []string{`{"a":1,"a":2}`, `{"a":"\ud800"}`, `{}`, `[] {}`} {
		limit := 1024
		if input == `{}` {
			limit = 1
		}
		if Validate([]byte(input), limit) == nil {
			t.Fatalf("Validate accepted %s", input)
		}
	}
}
