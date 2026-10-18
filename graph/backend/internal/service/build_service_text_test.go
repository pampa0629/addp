package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildTextChunksPreserveUnicodeAndCharacterBudgets(t *testing.T) {
	text := strings.Repeat("长沙🙂数据图谱", 4)
	const size, overlap = 5, 2
	chunks := splitText(text, size, overlap)
	if len(chunks) < 2 {
		t.Fatal("fixture must exercise multiple chunk boundaries")
	}
	reconstructed := chunks[0]
	for i, chunk := range chunks {
		if !utf8.ValidString(chunk) || utf8.RuneCountInString(chunk) > size {
			t.Fatalf("chunk %d split a Unicode character or exceeded its character budget: %q", i, chunk)
		}
		if i > 0 {
			previous, current := []rune(chunks[i-1]), []rune(chunk)
			if len(current) < overlap || string(current[:overlap]) != string(previous[len(previous)-overlap:]) {
				t.Fatalf("chunk %d lost the declared overlap: %q", i, chunk)
			}
			reconstructed += string(current[overlap:])
		}
	}
	if reconstructed != text {
		t.Fatalf("chunking lost or changed source text: %q", reconstructed)
	}
}

func TestBuildDocumentContextUsesCompleteUnicodeCharacters(t *testing.T) {
	if got := extractDocContext("汉字🙂graph", 3); got != "汉字🙂" {
		t.Fatalf("document context did not use a character budget: %q", got)
	}
	if got := extractDocContext("汉🙂", 20); got != "汉🙂" {
		t.Fatalf("short document context changed: %q", got)
	}
}

func TestBuildTextSeparatorPositionsUseCharacterOffsets(t *testing.T) {
	chunks := splitText("甲乙丙丁。戊己庚辛壬癸", 6, 4)
	if len(chunks) < 2 || chunks[0] != "甲乙丙丁。" {
		t.Fatalf("semantic separator used byte offsets: %q", chunks)
	}
	for _, chunk := range chunks {
		if !utf8.ValidString(chunk) || utf8.RuneCountInString(chunk) > 6 {
			t.Fatalf("semantic boundary corrupted a character: %q", chunk)
		}
	}
}

func TestBuildTextLargeOverlapRetainsForwardProgress(t *testing.T) {
	text := "甲乙丙丁戊己庚辛"
	chunks := splitText(text, 2, 200)
	if strings.Join(chunks, "") != text {
		t.Fatalf("oversized overlap lost source text or failed to advance: %q", chunks)
	}
	for _, chunk := range chunks {
		if !utf8.ValidString(chunk) || utf8.RuneCountInString(chunk) > 2 {
			t.Fatalf("invalid bounded chunk: %q", chunk)
		}
	}
}
