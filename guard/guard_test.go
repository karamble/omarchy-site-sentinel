// Package guard holds the release guard.
//
// The marketplace refuses plugins that ship or expose instructions aimed at
// coding agents, whether installed, printed or embedded in a binary. This test
// walks the whole repository and fails when such content reappears, so a
// release cannot regress into it.
package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// skipDirs are not part of a release.
var skipDirs = map[string]bool{
	".git": true,
	"bin":  true,
}

// forbiddenNames are filenames that are an instruction channel by convention,
// whatever they contain.
var forbiddenNames = []string{
	"agents.md",
	"skill.md",
	"claude.md",
	"agent.md",
}

// forbiddenDirs are the roots a plugin must never write to or vendor.
var forbiddenDirs = []string{
	".claude",
	".agents",
	"skills",
}

// directive matches prose addressed to an agent rather than to a person.
//
// The phrases are assembled from fragments so this file does not match itself.
var directive = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`for cod` + `ing agents`,
	`you are an ` + `agent`,
	`before ` + `acting`,
	`the ` + `agent must`,
	`instructions for ` + `agents`,
	`agent-` + `facing guide`,
}, "|"))

// embedMarkdown catches a Go file embedding markdown into a binary, which is
// how a printed guide survives a deleted file.
var embedMarkdown = regexp.MustCompile(`go:embed\s+\S*\.md`)

// frontMatter catches a leading YAML block with a name and description, which
// is the shape that makes a markdown file auto-load as a skill.
var frontMatter = regexp.MustCompile(`(?s)\A---\r?\n.*?\bname:.*?\bdescription:.*?\r?\n---`)

func TestNoAgentInstructionSurface(t *testing.T) {
	root := ".."
	self, err := filepath.Abs("guard_test.go")
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			for _, bad := range forbiddenDirs {
				if strings.EqualFold(d.Name(), bad) {
					t.Errorf("%s: a plugin must not ship a %s directory", path, bad)
					return filepath.SkipDir
				}
			}
			return nil
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if abs == self {
			return nil
		}

		name := strings.ToLower(d.Name())
		for _, bad := range forbiddenNames {
			if name == bad {
				t.Errorf("%s: %s is an agent instruction channel", path, bad)
			}
		}

		switch filepath.Ext(name) {
		case ".md", ".go", ".qml", ".json", ".txt":
		default:
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)

		if m := directive.FindString(text); m != "" {
			t.Errorf("%s: reads as an instruction to an agent (%q)", path, m)
		}
		if m := embedMarkdown.FindString(text); m != "" {
			t.Errorf("%s: embeds markdown into a binary (%q)", path, m)
		}
		if frontMatter.MatchString(text) {
			t.Errorf("%s: has skill front matter", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
