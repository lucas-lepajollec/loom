package loom

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func mcpSkills() map[string]Capability {
	out := map[string]Capability{}
	skills := scanSkills()
	counts := map[string]int{}
	for _, c := range skills {
		counts[filepath.Base(c.Dir)]++
	}
	for _, c := range skills {
		name := filepath.Base(c.Dir)
		if counts[name] > 1 {
			name = c.Source + ":" + name
		}
		out[name] = c
	}
	return out
}

func (s *brainService) ListSkills() ([]brain.Skill, error) {
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	out := []brain.Skill{}
	for name, c := range mcpSkills() {
		out = append(out, brain.Skill{Name: name, Title: c.Name, Description: c.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *brainService) ReadSkill(req brain.ReadSkillRequest) (brain.SkillContent, error) {
	if err := brainAvailable(); err != nil {
		return brain.SkillContent{}, err
	}
	c, exists := mcpSkills()[req.Name]
	if !exists {
		return brain.SkillContent{}, errors.New("skill not found")
	}
	var source SkillSource
	for _, src := range skillSources() {
		if src.ID == c.Source {
			source = src
			break
		}
	}
	var folder *os.Root
	var err error
	if source.ID == "loom" {
		root, openErr := openSkillsRoot(false)
		if openErr != nil {
			return brain.SkillContent{}, openErr
		}
		folder, err = root.OpenRoot(filepath.Base(c.Dir))
		root.Close()
	} else {
		folder, err = os.OpenRoot(c.Dir)
	}
	if err != nil {
		return brain.SkillContent{}, err
	}
	defer folder.Close()
	f, err := folder.Open("SKILL.md")
	if err != nil {
		return brain.SkillContent{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return brain.SkillContent{}, errors.New("SKILL.md must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, brain.MaxFileBytes+1))
	if err != nil {
		return brain.SkillContent{}, err
	}
	if len(b) > brain.MaxFileBytes {
		return brain.SkillContent{}, errors.New("SKILL.md exceeds 1 MiB")
	}
	out := brain.SkillContent{Text: string(b), Files: []string{}}
	err = fs.WalkDir(folder.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out.Files = append(out.Files, path)
			if len(out.Files) == 200 {
				return fs.SkipAll
			}
		}
		return nil
	})
	return out, err
}
