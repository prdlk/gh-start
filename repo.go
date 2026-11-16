package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Repo describes the git checkout gh-start was launched from.
type Repo struct {
	Root     string
	Name     string
	Branch   string
	Branches []string

	recentCommits string
	topLevel      []string
	readme        string
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func loadRepo() (*Repo, error) {
	root, err := git("", "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil, errors.New("not inside a git repository")
	}
	r := &Repo{Root: root, Name: filepath.Base(root)}
	r.Branch, _ = git(root, "branch", "--show-current")
	if r.Branch == "" {
		r.Branch = "detached HEAD"
	}
	if out, err := git(root, "for-each-ref", "--format=%(refname:short)", "refs/heads"); err == nil && out != "" {
		r.Branches = strings.Split(out, "\n")
	}
	r.recentCommits, _ = git(root, "log", "--oneline", "-n", "15")
	if out, err := git(root, "ls-files"); err == nil && out != "" {
		seen := map[string]bool{}
		for _, f := range strings.Split(out, "\n") {
			top, _, isDir := strings.Cut(f, "/")
			if isDir {
				top += "/"
			}
			if !seen[top] {
				seen[top] = true
				r.topLevel = append(r.topLevel, top)
			}
		}
	}
	for _, name := range []string{"README.md", "README", "readme.md"} {
		if b, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			r.readme = truncate(string(b), 2000)
			break
		}
	}
	return r, nil
}

// PromptContext is the repository summary given to the autocomplete model.
func (r *Repo) PromptContext() string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\ncurrent branch: %s\n", r.Name, r.Branch)
	if len(r.topLevel) > 0 {
		fmt.Fprintf(&b, "top-level entries: %s\n", strings.Join(r.topLevel[:min(len(r.topLevel), 60)], " "))
	}
	if r.recentCommits != "" {
		fmt.Fprintf(&b, "recent commits:\n%s\n", r.recentCommits)
	}
	if r.readme != "" {
		fmt.Fprintf(&b, "README excerpt:\n%s\n", r.readme)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Model is an entry of `omp models --json`.
type Model struct {
	Selector string   `json:"selector"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Thinking []string `json:"thinking"`
}

// loadModels lists omp chat models; empty when omp is missing or fails.
func loadModels() []Model {
	out, err := exec.Command("omp", "models", "--json").Output()
	if err != nil {
		return nil
	}
	var resp struct {
		Models []Model `json:"models"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return nil
	}
	return slices.DeleteFunc(resp.Models, func(m Model) bool { return m.Kind != "" && m.Kind != "chat" })
}

var knownAgents = []string{"omp", "claude", "codex", "gemini", "opencode"}

// detectAgents returns installed agents with preferred first.
func detectAgents(preferred string) []string {
	var agents []string
	for _, a := range knownAgents {
		if _, err := exec.LookPath(a); err == nil {
			agents = append(agents, a)
		}
	}
	if i := slices.Index(agents, preferred); i > 0 {
		agents = slices.Insert(slices.Delete(agents, i, i+1), 0, preferred)
	} else if i < 0 {
		if _, err := exec.LookPath(preferred); err == nil {
			agents = slices.Insert(agents, 0, preferred)
		}
	}
	return agents
}

// workmuxAgent returns the `agent:` workmux will use in root: the project
// .workmux.yaml overrides the global config. Empty when neither sets it.
func workmuxAgent(root string) string {
	paths := []string{filepath.Join(root, ".workmux.yaml"), filepath.Join(root, ".workmux.yml")}
	if global, err := exec.Command("workmux", "config", "path").Output(); err == nil {
		paths = append(paths, strings.TrimSpace(string(global)))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			v, ok := strings.CutPrefix(line, "agent:") // top-level key only
			if !ok {
				continue
			}
			if i := strings.Index(v, " #"); i >= 0 {
				v = v[:i]
			}
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			if v != "" {
				return v
			}
		}
	}
	return ""
}
