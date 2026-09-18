package docparse

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestExtractTexts(t *testing.T) {
	docx := zipBytes(t, "word/document.xml",
		`<?xml version="1.0"?><w:document xmlns:w="urn:example"><w:body>`+
			`<w:p><w:r><w:t>第一段</w:t></w:r></w:p>`+
			`<w:p><w:r><w:t>second</w:t></w:r></w:p></w:body></w:document>`)
	odt := zipBytes(t, "content.xml",
		`<?xml version="1.0"?><office:document-content xmlns:office="urn:office" xmlns:text="urn:text">`+
			`<office:body><text:h>标题</text:h><text:p>段落一</text:p></office:body></office:document-content>`)

	tests := []struct {
		name         string
		filename     string
		content      []byte
		wantFormat   string
		wantText     string
		wantTruncate bool
	}{
		{"txt utf-8 bom", "需求.txt", append([]byte{0xEF, 0xBB, 0xBF}, "你好\n世界"...), "text", "你好\n世界", false},
		{"txt legacy gbk", "需求.txt", []byte{0xC4, 0xE3, 0xBA, 0xC3}, "text", "你好", false},
		{"markdown", "plan.md", []byte("# 计划\n\n- 步骤一"), "markdown", "# 计划\n\n- 步骤一", false},
		{"docx paragraphs", "需求.docx", docx, "docx", "第一段\nsecond", false},
		{"odt headings", "需求.odt", odt, "odt", "标题\n段落一", false},
		{"rtf control words", "需求.rtf", []byte(`{\rtf1\ansi \u20320?\u22909? world\par second  line}`), "rtf", "你好 world\nsecond line", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := Extract(test.filename, test.content)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if parsed.Format != test.wantFormat || parsed.Text != test.wantText {
				t.Fatalf("got format=%q text=%q; want %q / %q", parsed.Format, parsed.Text, test.wantFormat, test.wantText)
			}
			if parsed.Chars != len([]rune(test.wantText)) || parsed.Truncated {
				t.Fatalf("chars=%d truncated=%v; want %d / false", parsed.Chars, parsed.Truncated, len([]rune(test.wantText)))
			}
			if parsed.Filename != test.filename {
				t.Fatalf("filename=%q", parsed.Filename)
			}
		})
	}
}

func TestExtractRefusals(t *testing.T) {
	if _, err := Extract("需求.exe", []byte("x")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported extension: %v", err)
	}
	if _, err := Extract("big.txt", bytes.Repeat([]byte("a"), MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := Extract("empty.txt", []byte("  \n\t ")); !errors.Is(err, ErrNoText) {
		t.Fatalf("no text: %v", err)
	}
	// Invalid GBK + invalid UTF-8 still decodes (replacement), never surfaces
	// as ErrUnsupported; a corrupt zip is ErrNoText (422), not a panic.
	if _, err := Extract("bad.docx", []byte("not a zip")); !errors.Is(err, ErrNoText) {
		t.Fatalf("corrupt docx: %v", err)
	}
}

func TestExtractTruncatesAtIntakeLimit(t *testing.T) {
	parsed, err := Extract("long.txt", []byte(strings.Repeat("字", MaxChars+500)))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !parsed.Truncated || parsed.Chars != MaxChars {
		t.Fatalf("chars=%d truncated=%v; want %d / true", parsed.Chars, parsed.Truncated, MaxChars)
	}
}

// zipBytes builds a single-file zip archive in memory for the OOXML tests.
func zipBytes(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := file.Write([]byte(content)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buffer.Bytes()
}
