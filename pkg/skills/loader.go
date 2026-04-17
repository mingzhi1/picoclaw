package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

var (
	namePattern        = regexp.MustCompile(`^[a-zA-Z0-9]+([.\-][a-zA-Z0-9]+)*$`)
	reFrontmatter      = regexp.MustCompile(`(?s)^---(?:\r\n|\n|\r)(.*?)(?:\r\n|\n|\r)---`)
	reStripFrontmatter = regexp.MustCompile(`(?s)^---(?:\r\n|\n|\r)(.*?)(?:\r\n|\n|\r)---(?:\r\n|\n|\r)*`)
	reToolStep         = regexp.MustCompile(`(?m)^\d+\.\s*\[(parallel|serial)\]\s*(.+)$`)
	reKeywordSplit     = regexp.MustCompile(`[\s,()\x{3001}\x{ff0c}\x{ff08}\x{ff09}—/]+`)
)

const (
	MaxNameLength        = 64
	MaxDescriptionLength = 1024
)

// ToolStep represents one step in a skill's tool execution plan.
// Defined in SKILL.md body with format: "N. [parallel|serial] action1 | action2"
type ToolStep struct {
	Step    int      // 1-based step number
	Mode    string   // "parallel" or "serial"
	Actions []string // Tool actions (e.g. "read_file ~/.picoclaw/config.json")
}

type SkillMetadata struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ToolSteps   []ToolStep `json:"-"`
}

type SkillInfo struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Source      string     `json:"source"`
	Description string     `json:"description"`
	ToolSteps   []ToolStep `json:"-"`
}

func (info SkillInfo) validate() error {
	var errs error
	if info.Name == "" {
		errs = errors.Join(errs, errors.New("name is required"))
	} else {
		if len(info.Name) > MaxNameLength {
			errs = errors.Join(errs, fmt.Errorf("name exceeds %d characters", MaxNameLength))
		}
		if !namePattern.MatchString(info.Name) {
			errs = errors.Join(errs, errors.New("name must be alphanumeric with hyphens"))
		}
	}

	if info.Description == "" {
		errs = errors.Join(errs, errors.New("description is required"))
	} else if len(info.Description) > MaxDescriptionLength {
		errs = errors.Join(errs, fmt.Errorf("description exceeds %d character", MaxDescriptionLength))
	}
	return errs
}

type SkillsLoader struct {
	workspace       string
	workspaceSkills string // workspace skills (project-level)
	globalSkills    string // global skills (~/.picoclaw/skills)
	builtinSkills   string // builtin skills
}

func NewSkillsLoader(workspace string, globalSkills string, builtinSkills string) *SkillsLoader {
	return &SkillsLoader{
		workspace:       workspace,
		workspaceSkills: filepath.Join(workspace, "skills"),
		globalSkills:    globalSkills, // ~/.picoclaw/skills
		builtinSkills:   builtinSkills,
	}
}

func (sl *SkillsLoader) ListSkills() []SkillInfo {
	skills := make([]SkillInfo, 0)
	seen := make(map[string]bool)

	addSkills := func(dir, source string) {
		if dir == "" {
			return
		}
		dirs, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			skillFile := filepath.Join(dir, d.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				continue
			}
			info := SkillInfo{
				Name:   d.Name(),
				Path:   skillFile,
				Source: source,
			}
			metadata := sl.getSkillMetadata(skillFile)
			if metadata != nil {
				info.Description = metadata.Description
				info.ToolSteps = metadata.ToolSteps
				// Use metadata name only if it passes validation; otherwise keep directory name.
				if metadata.Name != "" && namePattern.MatchString(metadata.Name) && len(metadata.Name) <= MaxNameLength {
					info.Name = metadata.Name
				}
			}
			if err := info.validate(); err != nil {
				slog.Warn("invalid skill from "+source, "name", info.Name, "error", err)
				continue
			}
			if seen[info.Name] {
				continue
			}
			seen[info.Name] = true
			skills = append(skills, info)
		}
	}

	// Priority: workspace > global > builtin
	addSkills(sl.workspaceSkills, "workspace")
	addSkills(sl.globalSkills, "global")
	addSkills(sl.builtinSkills, "builtin")

	return skills
}

func (sl *SkillsLoader) LoadSkill(name string) (string, bool) {
	// 1. load from workspace skills first (project-level)
	if sl.workspaceSkills != "" {
		skillFile := filepath.Join(sl.workspaceSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	// 2. then load from global skills (~/.picoclaw/skills)
	if sl.globalSkills != "" {
		skillFile := filepath.Join(sl.globalSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	// 3. finally load from builtin skills
	if sl.builtinSkills != "" {
		skillFile := filepath.Join(sl.builtinSkills, name, "SKILL.md")
		if content, err := os.ReadFile(skillFile); err == nil {
			return sl.stripFrontmatter(string(content)), true
		}
	}

	return "", false
}

func (sl *SkillsLoader) LoadSkillsForContext(skillNames []string) string {
	if len(skillNames) == 0 {
		return ""
	}

	var parts []string
	for _, name := range skillNames {
		content, ok := sl.LoadSkill(name)
		if ok {
			parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

func (sl *SkillsLoader) BuildSkillsSummary() string {
	allSkills := sl.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<skills>")
	for _, s := range allSkills {
		escapedName := escapeXML(s.Name)
		escapedDesc := escapeXML(s.Description)
		escapedPath := escapeXML(s.Path)

		lines = append(lines, "  <skill>")
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", escapedName))
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", escapedDesc))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", escapedPath))
		lines = append(lines, fmt.Sprintf("    <source>%s</source>", s.Source))
		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")

	return strings.Join(lines, "\n")
}

func (sl *SkillsLoader) getSkillMetadata(skillPath string) *SkillMetadata {
	content, err := os.ReadFile(skillPath)
	if err != nil {
		logger.WarnCF("skills", "Failed to read skill metadata",
			map[string]any{
				"skill_path": skillPath,
				"error":      err.Error(),
			})
		return nil
	}

	frontmatter := sl.extractFrontmatter(string(content))
	if frontmatter == "" {
		return &SkillMetadata{
			Name: filepath.Base(filepath.Dir(skillPath)),
		}
	}

	// Try JSON first (for backward compatibility)
	var jsonMeta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(frontmatter), &jsonMeta); err == nil {
		meta := &SkillMetadata{
			Name:        jsonMeta.Name,
			Description: jsonMeta.Description,
		}
		meta.ToolSteps = parseToolSteps(string(content))
		return meta
	}

	// Fall back to simple YAML parsing
	yamlMeta := sl.parseSimpleYAML(frontmatter)
	meta := &SkillMetadata{
		Name:        yamlMeta["name"],
		Description: yamlMeta["description"],
	}
	// Parse tool_steps from body (not frontmatter)
	meta.ToolSteps = parseToolSteps(string(content))
	return meta
}

// parseSimpleYAML parses simple key: value YAML format
// Example: name: github\n description: "..."
// Normalizes line endings to handle \n (Unix), \r\n (Windows), and \r (classic Mac)
func (sl *SkillsLoader) parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)

	// Normalize line endings: convert \r\n and \r to \n
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	for line := range strings.SplitSeq(normalized, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, "\"'")
			result[key] = value
		}
	}

	return result
}

func (sl *SkillsLoader) extractFrontmatter(content string) string {
	// Support \n (Unix), \r\n (Windows), and \r (classic Mac) line endings for frontmatter blocks
	match := reFrontmatter.FindStringSubmatch(content)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func (sl *SkillsLoader) stripFrontmatter(content string) string {
	return reStripFrontmatter.ReplaceAllString(content, "")
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// parseToolSteps extracts tool steps from SKILL.md content.
// Looks for lines matching: "N. [parallel|serial] action1 | action2"
// Example:
//
//  1. [parallel] read_file {skill_path} | read_file ~/.picoclaw/config.json
//  2. [serial] edit_file ~/.picoclaw/config.json
func parseToolSteps(content string) []ToolStep {
	// Normalize line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	matches := reToolStep.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	steps := make([]ToolStep, 0, len(matches))
	for i, m := range matches {
		mode := m[1]
		actionsStr := m[2]
		actions := strings.Split(actionsStr, "|")
		cleaned := make([]string, 0, len(actions))
		for _, a := range actions {
			a = strings.TrimSpace(a)
			if a != "" {
				cleaned = append(cleaned, a)
			}
		}
		if len(cleaned) > 0 {
			steps = append(steps, ToolStep{
				Step:    i + 1,
				Mode:    mode,
				Actions: cleaned,
			})
		}
	}
	return steps
}

// MatchSkillByMessage finds the best matching skill for a user message
// using keyword overlap with skill descriptions. No LLM needed.
// Only returns skills that have ToolSteps defined.
func (sl *SkillsLoader) MatchSkillByMessage(message string) *SkillInfo {
	msg := strings.ToLower(message)
	skills := sl.ListSkills()

	var bestMatch *SkillInfo
	bestScore := 0

	for i := range skills {
		s := &skills[i]
		if len(s.ToolSteps) == 0 {
			continue // Skip skills without tool_steps
		}

		score := 0
		desc := strings.ToLower(s.Description)

		// Extract keywords from description
		keywords := reKeywordSplit.Split(desc, -1)
		for _, kw := range keywords {
			kw = strings.TrimSpace(kw)
			if len(kw) >= 2 && strings.Contains(msg, kw) {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			match := skills[i] // copy
			bestMatch = &match
		}
	}

	if bestMatch != nil {
		logger.DebugCF("skills", "Matched skill by keywords",
			map[string]any{
				"skill":      bestMatch.Name,
				"score":      bestScore,
				"tool_steps": len(bestMatch.ToolSteps),
			})
	}
	return bestMatch
}
