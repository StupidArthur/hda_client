package bundle

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"testing"
)

// The bundle is a fixed publish of files that live elsewhere in the repo. This
// test fails when a copy drifts, so the portal never ships a stale or dangling
// SKILL.md reference.
func TestBundleMatchesRepositorySources(t *testing.T) {
	pairs := []struct {
		embedded string
		source   string
	}{
		{"skills/hda-data-import/SKILL.md", "../../../hda_mocker_4/skills/hda-data-import/SKILL.md"},
		{"docs/HDA_MOCKER_4_RELEASE_1_5_0.md", "../../../hda_mocker_4/docs/HDA_MOCKER_4_RELEASE_1_5_0.md"},
		{"docs/HDA_MOCKER_4_RELEASE_1_5_1.md", "../../../hda_mocker_4/docs/HDA_MOCKER_4_RELEASE_1_5_1.md"},
		{"docs/HDA_MOCKER_4_RELEASE_1_5_2.md", "../../../hda_mocker_4/docs/HDA_MOCKER_4_RELEASE_1_5_2.md"},
		{"docs/HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md", "../../../hda_mocker_4/docs/HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md"},
		{"docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md", "../../../hda_mocker_4/docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md"},
		{"docs/HDA_MOCKER_4_UPGRADE_TO_1_5_0.md", "../../../hda_mocker_4/docs/HDA_MOCKER_4_UPGRADE_TO_1_5_0.md"},
		{"tools/reformat_dataset.py", "../../../hda_mocker_4/tools/reformat_dataset.py"},
	}
	walk, err := fs.Sub(embedded, "files")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	for _, pair := range pairs {
		embeddedBytes, err := fs.ReadFile(walk, pair.embedded)
		if err != nil {
			t.Fatalf("read embedded %s: %v", pair.embedded, err)
		}
		sourceBytes, err := os.ReadFile(filepath.FromSlash(pair.source))
		if err != nil {
			t.Fatalf("read source %s: %v", pair.source, err)
		}
		if !bytes.Equal(embeddedBytes, sourceBytes) {
			t.Fatalf("bundle copy of %s is out of sync with %s", pair.embedded, pair.source)
		}
	}
}

func TestZipContainsDocumentsAndScripts(t *testing.T) {
	data, err := Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty bundle")
	}
	walk, err := fs.Sub(embedded, "files")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	skill, err := fs.ReadFile(walk, "skills/hda-data-import/SKILL.md")
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !bytes.Contains(skill, []byte("../../docs/HDA_MOCKER_4_RELEASE_1_5_0.md")) {
		t.Fatal("SKILL.md should keep its relative documentation link; the bundle mirrors the repo layout")
	}
}

func TestItemsAreSorted(t *testing.T) {
	items, err := Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("no bundle items")
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].Path >= items[i].Path {
			t.Fatalf("items not sorted: %s >= %s", items[i-1].Path, items[i].Path)
		}
	}
}

func TestSkillDocumentationLinksResolveInsideBundle(t *testing.T) {
	walk, err := fs.Sub(embedded, "files")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := fs.ReadFile(walk, "skills/hda-data-import/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)`).FindAllSubmatch(skill, -1)
	if len(links) == 0 {
		t.Fatal("no documentation links to validate")
	}
	for _, link := range links {
		target := path.Join("skills/hda-data-import", string(link[1]))
		if !fs.ValidPath(target) {
			t.Fatalf("link escapes bundle: %s", link[1])
		}
		info, err := fs.Stat(walk, target)
		if err != nil || info.IsDir() {
			t.Fatalf("unusable documentation link %s: %v", target, err)
		}
	}
}
