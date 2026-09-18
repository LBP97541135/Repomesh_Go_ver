package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseIssueDocumentEndpoint(t *testing.T) {
	post := func(t *testing.T, filename, content string, omitFile bool) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if !omitFile {
			part, err := writer.CreateFormFile("file", filename)
			if err != nil {
				t.Fatalf("CreateFormFile: %v", err)
			}
			if _, err := part.Write([]byte(content)); err != nil {
				t.Fatalf("part write: %v", err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("writer close: %v", err)
		}
		request := httptest.NewRequest("POST", "/api/issues/parse-document", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		if err := parseIssueDocument(recorder, request); err != nil {
			t.Fatalf("parseIssueDocument returned error: %v", err)
		}
		return recorder
	}

	t.Run("parses an uploaded txt", func(t *testing.T) {
		recorder := post(t, "需求.txt", "第一行\n第二行", false)
		if recorder.Code != 200 {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var parsed struct {
			Filename string `json:"filename"`
			Format   string `json:"format"`
			Text     string `json:"text"`
			Chars    int    `json:"chars"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("body %q: %v", recorder.Body.String(), err)
		}
		if parsed.Format != "text" || parsed.Text != "第一行\n第二行" || parsed.Chars != 7 {
			t.Fatalf("parsed=%+v", parsed)
		}
	})

	t.Run("refuses unsupported formats with 415", func(t *testing.T) {
		recorder := post(t, "需求.exe", "MZ", false)
		if recorder.Code != 415 || !strings.Contains(recorder.Body.String(), "UNSUPPORTED_MEDIA_TYPE") {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("requires the file field with 422", func(t *testing.T) {
		recorder := post(t, "", "", true)
		if recorder.Code != 422 || !strings.Contains(recorder.Body.String(), "VALIDATION_FAILED") {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})
}
