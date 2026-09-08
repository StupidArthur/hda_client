package hda

import (
	"strings"
	"testing"
)

func TestExpandTagExpression(t *testing.T) {
	tags, err := ExpandTagExpression("M{index:4d}.VALUE[0,2]")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(tags, ","), "M0000.VALUE,M0001.VALUE,M0002.VALUE"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
	// Empty prefix and suffix are valid, making the grammar useful for raw IDs.
	tags, err = ExpandTagExpression("{index:2d}[8,9]")
	if err != nil || strings.Join(tags, ",") != "08,09" {
		t.Fatalf("empty affix = %#v, %v", tags, err)
	}
	preview, err := PreviewTagExpression("M{index:4d}.VALUE[0,9999]")
	if err != nil || preview.Count != 10_000 || preview.First != "M0000.VALUE" || preview.Second != "M0001.VALUE" || preview.Last != "M9999.VALUE" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
}

func TestExpandTagExpressionRejectsInvalidInput(t *testing.T) {
	for _, source := range []string{
		"M{index:4d}.VALUE[2,1]", "M{index:0d}.VALUE[0,1]", "M{index:2d}.VALUE[0,100]",
		"M{index:6d}.VALUE[0,100000]", "not an expression",
	} {
		if _, err := ExpandTagExpression(source); err == nil {
			t.Errorf("ExpandTagExpression(%q) unexpectedly succeeded", source)
		}
	}
}

func TestParseCSVTagsSupportsBOMHeaderAndQuotes(t *testing.T) {
	tags, err := ParseCSVTags(strings.NewReader("\ufeffnode,comment\n\"M,0001.VALUE\",quoted\nM0002.VALUE,plain\nM0002.VALUE,duplicate\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(tags, "|"), "M,0001.VALUE|M0002.VALUE"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
}
