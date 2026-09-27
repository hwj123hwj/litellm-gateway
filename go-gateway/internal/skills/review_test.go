package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncReportUsesJSONArray(t *testing.T) {
	repo := newTestRepo(t)
	report, err := Apply(repo, Manifest{Version: 1, Targets: []string{t.TempDir()}, Enabled: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	var body struct {
		Targets []map[string]json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"linked", "current", "removed", "skipped", "errors"} {
		if string(body.Targets[0][field]) == "null" {
			t.Errorf("%s must be an array: %s", field, raw)
		}
	}
}

func TestApplyRejectsTraversalBeforeFilesystemChanges(t *testing.T) {
	repo := newTestRepo(t)
	target := filepath.Join(t.TempDir(), "install")
	_, err := Apply(repo, Manifest{Version: 1, Targets: []string{target}, Enabled: []string{"../registry"}})
	if err == nil {
		t.Fatal("unsafe manifest must fail before any installation")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("invalid manifest created target")
	}
}

func TestRelativeRepoCreatesWorkingLinks(t *testing.T) {
	repo := newTestRepo(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, repo)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	manifest := Manifest{Version: 1, Targets: []string{target}, Enabled: []string{"alpha"}}
	if _, err := Apply(relative, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(target, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("broken installed link: %v", err)
	}
}

func TestInvalidRegistryIDsAreRejected(t *testing.T) {
	for _, id := range []string{"../escape", `..\escape`, "", ".", "..", "C:escape"} {
		t.Run(id, func(t *testing.T) {
			repo := newTestRepo(t)
			raw, _ := json.Marshal([]Skill{{ID: id}})
			if err := os.WriteFile(RegistryPath(repo), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadRegistry(repo); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}

func TestInvalidManifestDoesNotReplaceSavedConfig(t *testing.T) {
	repo := newTestRepo(t)
	original := Manifest{Version: 1, Targets: []string{t.TempDir()}, Enabled: []string{"alpha"}}
	if err := SaveManifest(repo, original); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(ManifestPath(repo))
	for _, invalid := range []Manifest{
		{Version: 2, Targets: original.Targets, Enabled: original.Enabled},
		{Version: 1, Targets: []string{"relative/path"}, Enabled: original.Enabled},
		{Version: 1, Targets: nil, Enabled: original.Enabled},
		{Version: 1, Targets: original.Targets, Enabled: []string{"unknown"}},
	} {
		if err := SaveManifest(repo, invalid); err == nil {
			t.Fatal("invalid manifest accepted")
		}
		after, _ := os.ReadFile(ManifestPath(repo))
		if string(after) != string(before) {
			t.Fatal("invalid save changed manifest")
		}
	}
}

func TestSaveDoesNotFollowManifestSymlink(t *testing.T) {
	repo := newTestRepo(t)
	outside := filepath.Join(t.TempDir(), "keep.json")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ManifestPath(repo)); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(repo, Manifest{Version: 1, Targets: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outside)
	if err != nil || string(raw) != "preserve" {
		t.Fatal("save modified symlink destination")
	}
}
