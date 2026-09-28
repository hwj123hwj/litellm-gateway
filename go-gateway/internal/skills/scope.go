package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Scope keeps project selections separate from the shared catalog and global manifest.
type Scope struct{ Repo, Project string }
type LocalSkill struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

func ResolveScope(repo, project string) (Scope, error) {
	s := Scope{Repo: repo}
	if project == "" {
		return s, nil
	}
	project = expandTilde(strings.TrimSpace(project))
	if !filepath.IsAbs(project) {
		return s, fmt.Errorf("请输入项目的绝对路径")
	}
	real, err := filepath.EvalSymlinks(project)
	if err != nil {
		return s, fmt.Errorf("项目目录不存在或无法访问")
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return s, fmt.Errorf("项目路径必须是目录")
	}
	home, _ := os.UserHomeDir()
	if real == string(filepath.Separator) || real == home {
		return s, fmt.Errorf("请选择具体项目目录，不能使用主目录或根目录")
	}
	// Project writes may not be redirected into a global scope by a symlink.
	for _, part := range []string{".agents", ".agents/skills", ".agents/skills.enabled.json"} {
		info, err := os.Lstat(filepath.Join(real, part))
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return s, fmt.Errorf("项目 %s 不能是符号链接", part)
		}
		if err != nil && !os.IsNotExist(err) {
			return s, err
		}
	}
	s.Project = real
	return s, nil
}
func (s Scope) Path() string {
	if s.Project == "" {
		return ManifestPath(s.Repo)
	}
	return filepath.Join(s.Project, ".agents", manifestName)
}
func (s Scope) Load() (Manifest, bool, error) {
	if s.Project == "" {
		return LoadManifest(s.Repo)
	}
	m := Manifest{Version: 1, Enabled: []string{}}
	raw, err := os.ReadFile(s.Path())
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return m, false, err
	}
	if exists {
		if err := json.Unmarshal(raw, &m); err != nil {
			return m, true, fmt.Errorf("项目技能清单格式错误: %w", err)
		}
	}
	m.Targets = []string{filepath.Join(s.Project, ".agents", "skills")}
	return normalize(m), exists, nil
}
func (s Scope) Save(m Manifest) error {
	if s.Project == "" {
		return SaveManifest(s.Repo, m)
	}
	if _, err := ResolveScope(s.Repo, s.Project); err != nil {
		return err
	}
	registry, err := LoadRegistry(s.Repo)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, skill := range registry {
		known[skill.ID] = true
	}
	m = normalize(m)
	for _, id := range m.Enabled {
		if !known[id] {
			return fmt.Errorf("技能不在目录中: %s", id)
		}
	}
	// Relative target is portable when the project moves; Load binds it to this project.
	m.Targets = []string{".agents/skills"}
	m.Version = 1
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path()), ".skills-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.Path())
}
func (s Scope) Status(m Manifest) (Status, error) {
	st, err := BuildStatus(s.Repo, m)
	if err != nil {
		return st, err
	}
	st.ManifestPath = s.Path()
	_, err = os.Stat(s.Path())
	st.ManifestExists = err == nil
	return st, nil
}

// Existing project-authored and externally installed skills are observational only.
func (s Scope) LocalSkills(m Manifest) []LocalSkill {
	paths := expandAll(m.Targets)
	if s.Project != "" {
		paths = []string{filepath.Join(s.Project, ".agents", "skills"), filepath.Join(s.Project, ".claude", "skills"), filepath.Join(s.Project, ".codex", "skills")}
	}
	result := []LocalSkill{}
	for _, target := range dedupe(paths) {
		entries, err := os.ReadDir(target)
		if err != nil {
			continue
		}
		for _, e := range entries {
			path := filepath.Join(target, e.Name())
			if linkPointsTo(path, filepath.Join(SkillsDir(s.Repo), e.Name())) {
				continue
			}
			if info, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
				continue
			}
			result = append(result, LocalSkill{ID: e.Name(), Path: path, Source: filepath.Base(filepath.Dir(target))})
		}
	}
	return result
}
