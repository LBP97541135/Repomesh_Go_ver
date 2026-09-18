package models

import (
	"bytes"
	"testing"
)

func TestParseSaveReplaceAndKeep(t *testing.T) {
	body, err := ReadSaveBody([]byte(`{"providerId":null,"expectedRevision":null,"name":"开发网关","baseUrl":"https://gateway.example.invalid/v1/","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"sk-test"},"models":[{"id":null,"modelId":"deepseek-chat","displayName":null,"contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	input, canonical, err := parseSave(body, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !input.replace || !bytes.Equal(input.secret, []byte("sk-test")) || input.baseURL != "https://gateway.example.invalid/v1" {
		t.Fatalf("parsed %#v", input)
	}
	encoded, err := encodeProtectedInput(canonical)
	if err != nil || len(encoded) == 0 {
		t.Fatal(err)
	}
	if compareProtectedInput(encoded, encoded) != nil {
		t.Fatal("same input compared unequal")
	}
}

func TestCloseCommandRejectsMismatchedKey(t *testing.T) {
	if _, err := NewCloseCommand("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", []byte(`{}`)); err == nil {
		t.Fatal("mismatched close keys accepted")
	}
	if _, err := NewCloseCommand("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", []byte(`{"x":1}`)); err == nil {
		t.Fatal("non-empty close body accepted")
	}
}
