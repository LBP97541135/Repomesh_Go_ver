// Package docparse turns an uploaded requirement document into plain text.
//
// Port of the Python intake parser (repomesh document_parse.py): the intake
// write itself only ever takes requirement_text — this package is how a client
// converts a document into that text, so the pipeline never learns about
// document formats. Documents are parsed in memory and never stored.
package docparse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// MaxBytes caps the accepted document size (10 MiB, as the Python intake).
const MaxBytes = 10 * 1024 * 1024

// MaxChars caps the extracted text at the intake write's own limit.
const MaxChars = 20_000

var (
	// ErrUnsupported: the extension is not in the supported set (HTTP 415).
	ErrUnsupported = errors.New("unsupported document format")
	// ErrTooLarge: the document exceeds MaxBytes (HTTP 413).
	ErrTooLarge = errors.New("document too large")
	// ErrNoText: the document is unreadable or yields no text (HTTP 422).
	ErrNoText = errors.New("no extractable text")
)

// Supported formats mapped to the label reported back to the client.
var formats = map[string]string{
	".txt": "text", ".md": "markdown", ".markdown": "markdown",
	".docx": "docx", ".pdf": "pdf", ".odt": "odt", ".rtf": "rtf",
}

// Parsed is the wire shape (frontend ParsedDocumentView / Python ParsedDocument).
type Parsed struct {
	Filename  string `json:"filename"`
	Format    string `json:"format"`
	Text      string `json:"text"`
	Chars     int    `json:"chars"`
	Truncated bool   `json:"truncated"`
}

// Extract parses content per the filename's extension.
func Extract(filename string, content []byte) (Parsed, error) {
	if len(content) > MaxBytes {
		return Parsed{}, fmt.Errorf("%w (%d bytes); limit is %d bytes", ErrTooLarge, len(content), MaxBytes)
	}
	extension := strings.ToLower(strchr(filename))
	format, ok := formats[extension]
	if !ok {
		return Parsed{}, fmt.Errorf("%w %q; supported: .markdown .md .docx .odt .pdf .rtf .txt", ErrUnsupported, filename)
	}
	text, err := parsers[extension](content)
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: could not read %s document: %v", ErrNoText, extension, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Parsed{}, fmt.Errorf("%w in %s — is it a scanned image PDF or an empty document?", ErrNoText, filename)
	}
	runes := []rune(text)
	truncated := len(runes) > MaxChars
	if truncated {
		text = string(runes[:MaxChars])
	}
	return Parsed{Filename: filename, Format: format, Text: text, Chars: len([]rune(text)), Truncated: truncated}, nil
}

func strchr(filename string) string {
	if index := strings.LastIndexByte(filename, '.'); index >= 0 {
		return filename[index:]
	}
	return filename
}

var parsers = map[string]func([]byte) (string, error){
	".txt": decodeText, ".md": decodeText, ".markdown": decodeText,
	".docx": parseDocx, ".odt": parseOdt, ".rtf": parseRtf, ".pdf": parsePdf,
}

// decodeText reads a text file with Chinese-friendly fallbacks: UTF-8 (BOM
// optional) first, then GB18030 for legacy GBK/GB2312 documents. GB18030's
// decoder replaces invalid bytes rather than failing, so the latin-1 last
// resort of the Python original is unreachable here.
// ponytail: malformed-GBK documents decode with U+FFFD instead of latin-1
// bytes; add a strict GB18030 validation pass if that ever matters.
func decodeText(content []byte) (string, error) {
	trimmed := bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(trimmed) {
		return string(trimmed), nil
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(content)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// xmlLines walks an XML stream collecting the character data of every element
// whose local name is in want; each such element becomes one trimmed line.
func xmlLines(content []byte, want map[string]bool) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.Strict = false
	var lines []string
	var current strings.Builder
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
			if want[element.Name.Local] {
				if line := strings.TrimSpace(current.String()); line != "" {
					lines = append(lines, line)
				}
				current.Reset()
			}
		case xml.CharData:
			if depth > 0 {
				current.Write(element)
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

func zipReader(content []byte) (*zip.Reader, error) {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, errors.New("not a valid zip archive")
	}
	return archive, nil
}

func zipFile(archive *zip.Reader, name string) ([]byte, error) {
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer handle.Close()
		return io.ReadAll(handle)
	}
	return nil, fmt.Errorf("archive has no %s", name)
}

func parseDocx(content []byte) (string, error) {
	archive, err := zipReader(content)
	if err != nil {
		return "", err
	}
	xmlBytes, err := zipFile(archive, "word/document.xml")
	if err != nil {
		return "", err
	}
	return xmlLines(xmlBytes, map[string]bool{"p": true})
}

func parseOdt(content []byte) (string, error) {
	archive, err := zipReader(content)
	if err != nil {
		return "", err
	}
	xmlBytes, err := zipFile(archive, "content.xml")
	if err != nil {
		return "", err
	}
	return xmlLines(xmlBytes, map[string]bool{"p": true, "h": true})
}

func parsePdf(content []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", err
	}
	var pages []string
	for i := 1; i <= reader.NumPage(); i++ {
		// One corrupt page must not sink the file (mirrors the Python intake).
		text, err := reader.Page(i).GetPlainText(nil)
		if err != nil {
			continue
		}
		if strings.TrimSpace(text) != "" {
			pages = append(pages, text)
		}
	}
	return strings.Join(pages, "\n\n"), nil
}

var (
	rtfUnicode   = regexp.MustCompile(`\\u(-?\d{1,6})[^\s]?`)
	rtfLineBreak = regexp.MustCompile(`\\(?:par|line) ?`)
	rtfControl   = regexp.MustCompile(`\\[a-zA-Z]+-?\d* ?`)
	rtfHexEscape = regexp.MustCompile(`\\'[0-9a-fA-F]{2}`)
	rtfSpaceRun  = regexp.MustCompile(`[ \t]{2,}`)
)

func parseRtf(content []byte) (string, error) {
	text, err := decodeText(content)
	if err != nil {
		return "", err
	}
	// A literal backslash is escaped as \\ in RTF; collapse before stripping.
	text = strings.ReplaceAll(text, `\\`, `\`)
	// The delimiter space after a control word belongs to it — eat it too.
	text = rtfLineBreak.ReplaceAllString(text, "\n")
	text = rtfUnicode.ReplaceAllStringFunc(text, func(match string) string {
		groups := rtfUnicode.FindStringSubmatch(match)
		code, err := strconv.Atoi(groups[1])
		if err != nil {
			return ""
		}
		if code < 0 {
			code += 0x10000
		}
		if code > 0x10FFFF {
			return ""
		}
		return string(rune(code))
	})
	text = rtfControl.ReplaceAllString(text, "")
	text = rtfHexEscape.ReplaceAllString(text, "")
	text = strings.NewReplacer("{", "", "}", "", "\\", "").Replace(text)
	return rtfSpaceRun.ReplaceAllString(text, " "), nil
}
