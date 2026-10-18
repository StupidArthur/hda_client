package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "demo", "config.yaml"), "server:\n  endpoint: opc.tcp://0.0.0.0:4840\n")
	writeFile(t, filepath.Join(root, "demo", "summary.md"), "template")
	writeFile(t, filepath.Join(root, "plain", "config.yaml"), strings.Join([]string{
		"server:",
		"  endpoint: opc.tcp://10.0.0.5:18980",
		"  ns: 3",
		"  namespace: urn:hda:mocker4",
		"diagnostic:",
		"  listen: 0.0.0.0:4841",
		"playback:",
		"  files:",
		"    plain.parquet:",
		"      period_ms: 30000",
		"history:",
		"  retention_days: 0",
	}, "\n"))
	writeFile(t, filepath.Join(root, "plain", "summary.md"), "# plain")
	writeFile(t, filepath.Join(root, "plain", "hda", "plain.parquet"), "HDA")
	writeFile(t, filepath.Join(root, "plain", "hda", "nested", "hidden.parquet"), "HIDDEN")
	writeFile(t, filepath.Join(root, "plain", "da", "plain_da.parquet"), "DA")
	writeFile(t, filepath.Join(root, "unmonitored", "config.yaml"), strings.Join([]string{
		"server:",
		"  endpoint: opc.tcp://10.0.0.6:4840",
	}, "\n"))
	writeFile(t, filepath.Join(root, "incomplete", "config.yaml"), strings.Join([]string{
		"server:",
		"  endpoint: opc.tcp://0.0.0.0:18980",
		"diagnostic:",
		"  listen: 0.0.0.0:4841",
	}, "\n"))
	writeFile(t, filepath.Join(root, "broken", "config.yaml"), "server: [\n  endpoint: ")
	writeFile(t, filepath.Join(root, ".hidden", "config.yaml"), "server:\n  endpoint: opc.tcp://1.2.3.4:1\n")
	writeFile(t, filepath.Join(root, "material", "notes.md"), "not a preset")
	writeFile(t, filepath.Join(root, "nested", "inner", "config.yaml"), "server:\n  endpoint: opc.tcp://1.2.3.4:1\n")
	return root
}

func loadSnapshot(t *testing.T, root string) *Snapshot {
	t.Helper()
	cat, err := New(root, []string{"demo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return cat.Snapshot(true)
}

func TestDiscoveryCountsOnlyPresets(t *testing.T) {
	snap := loadSnapshot(t, newFixture(t))
	ids := []string{}
	for _, mock := range snap.Mocks {
		ids = append(ids, mock.ID)
	}
	want := []string{"broken", "incomplete", "plain", "unmonitored"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestConfigParsingAndDerivedDiagnostic(t *testing.T) {
	snap := loadSnapshot(t, newFixture(t))

	plain := snap.Find("plain")
	if plain == nil {
		t.Fatal("plain preset missing")
	}
	if !plain.ConfigValid {
		t.Fatalf("plain config should parse: %s", plain.ConfigError)
	}
	if !plain.UA.Valid || plain.UA.Host != "10.0.0.5" || plain.UA.Port != 18980 {
		t.Fatalf("unexpected endpoint: %+v", plain.UA)
	}
	if plain.Diag.State != DiagMonitored {
		t.Fatalf("diag state = %s", plain.Diag.State)
	}
	// Wildcard listener combined with a concrete UA host from the same config.
	if plain.Diag.URL != "http://10.0.0.5:4841" {
		t.Fatalf("diag url = %s", plain.Diag.URL)
	}
	if len(plain.Playback) != 1 || plain.Playback[0].PeriodMS != 30000 {
		t.Fatalf("playback = %+v", plain.Playback)
	}
	if plain.HDACount != 1 || plain.DACount != 1 {
		t.Fatalf("counts hda=%d da=%d", plain.HDACount, plain.DACount)
	}
	if !plain.SummaryExists {
		t.Fatal("summary should be detected")
	}

	incomplete := snap.Find("incomplete")
	if incomplete.Diag.State != DiagIncomplete {
		t.Fatalf("incomplete diag state = %s", incomplete.Diag.State)
	}
	if incomplete.Diag.URL != "" {
		t.Fatalf("incomplete diag url should be empty, got %s", incomplete.Diag.URL)
	}

	unmonitored := snap.Find("unmonitored")
	if unmonitored.Diag.State != DiagUnmonitored {
		t.Fatalf("unmonitored diag state = %s", unmonitored.Diag.State)
	}
	if unmonitored.ConfigurationIssue() == "" {
		t.Fatal("expected reason for unmonitored preset")
	}
}

func TestBrokenPresetSurvivesDiscovery(t *testing.T) {
	snap := loadSnapshot(t, newFixture(t))
	broken := snap.Find("broken")
	if broken == nil {
		t.Fatal("broken preset disappeared from discovery")
	}
	if broken.ConfigValid || broken.ConfigError == "" {
		t.Fatal("broken preset should be marked with a config error")
	}
	if broken.Diag.State != DiagInvalid {
		t.Fatalf("broken diag state = %s", broken.Diag.State)
	}
}

func TestMissingRootKeepsOldSnapshot(t *testing.T) {
	root := newFixture(t)
	cat, err := New(root, []string{"demo"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := cat.Snapshot(true)
	if len(first.Mocks) == 0 {
		t.Fatal("expected discovered presets")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}
	second := cat.Snapshot(true)
	if !second.Stale || second.Error == "" {
		t.Fatalf("expected stale snapshot with error, got %+v", second)
	}
	if len(second.Mocks) != len(first.Mocks) {
		t.Fatalf("stale snapshot lost entries: %d != %d", len(second.Mocks), len(first.Mocks))
	}
}

func TestListFilesAndDownloadScope(t *testing.T) {
	root := newFixture(t)
	snap := loadSnapshot(t, root)
	plain := snap.Find("plain")

	files := plain.ListFiles()
	names := []string{}
	for _, file := range files {
		names = append(names, file.Category+"/"+file.Name)
	}
	want := []string{"config/config.yaml", "da/plain_da.parquet", "hda/plain.parquet", "summary/summary.md"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", names, want)
	}
	for _, file := range files {
		if strings.Contains(file.Name, "hidden.parquet") {
			t.Fatal("nested parquet should not be published")
		}
	}

	var hdaID string
	for _, file := range files {
		if file.Category == CategoryHDA {
			hdaID = file.ID
		}
	}
	handle, meta, err := plain.OpenFile(hdaID)
	if err != nil {
		t.Fatalf("open hda: %v", err)
	}
	defer handle.Close()
	content := make([]byte, 8)
	n, _ := handle.Read(content)
	if string(content[:n]) != "HDA" {
		t.Fatalf("unexpected content %q", string(content[:n]))
	}
	if meta.Name != "plain.parquet" {
		t.Fatalf("meta name = %s", meta.Name)
	}

	if _, _, err := plain.OpenFile("../summary.md"); err == nil {
		t.Fatal("path traversal id must not resolve")
	}
	if _, _, err := plain.OpenFile(encodeFileID("hda", "nested/hidden.parquet")); err == nil {
		t.Fatal("nested file must not be downloadable")
	}
}

func TestSummaryReadsMarkdown(t *testing.T) {
	snap := loadSnapshot(t, newFixture(t))
	plain := snap.Find("plain")
	text, _, err := plain.Summary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !strings.Contains(text, "# plain") {
		t.Fatalf("unexpected summary %q", text)
	}
	unmonitored := snap.Find("unmonitored")
	if _, _, err := unmonitored.Summary(); err == nil {
		t.Fatal("missing summary should return an error, not an invented document")
	}
}

func TestEndpointWildcardAndLoopback(t *testing.T) {
	cases := []struct {
		raw      string
		valid    bool
		wildcard bool
		loopback bool
	}{
		{"opc.tcp://0.0.0.0:18980", true, true, false},
		{"opc.tcp://127.0.0.1:18980", true, false, true},
		{"opc.tcp://[::1]:18980", true, false, true},
		{"opc.tcp://10.0.0.5:18980", true, false, false},
		{"opc.tcp://10.0.0.5", false, false, false},
		{"http://10.0.0.5:80", false, false, false},
		{"", false, false, false},
	}
	for _, test := range cases {
		got := parseEndpoint(test.raw)
		if got.Valid != test.valid || got.Wildcard != test.wildcard || got.Loopback != test.loopback {
			t.Fatalf("parseEndpoint(%q) = %+v", test.raw, got)
		}
	}
}

func TestIPv6DiagnosticTarget(t *testing.T) {
	ua := parseEndpoint("opc.tcp://[2001:db8::1]:18980")
	target := deriveDiagTarget("0.0.0.0:4841", ua)
	if target.URL != "http://[2001:db8::1]:4841" {
		t.Fatalf("ipv6 target = %s", target.URL)
	}
}
