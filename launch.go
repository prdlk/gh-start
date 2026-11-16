package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/huh/v2/spinner"
)

// agentCommand is the agent argv without the task prompt.
func agentCommand(cfg *Config) []string {
	cmd := []string{cfg.Agent}
	if cfg.Agent != "omp" {
		return cmd
	}
	if cfg.Model != "" {
		cmd = append(cmd, "--model", cfg.Model)
	}
	if cfg.Thinking != "" {
		cmd = append(cmd, "--thinking", cfg.Thinking)
	}
	if cfg.Approval != "" {
		cmd = append(cmd, "--approval-mode", cfg.Approval)
	}
	return cmd
}

// promptArgs passes the task as the agent's initial interactive prompt.
func promptArgs(agent, task string) []string {
	switch agent {
	case "gemini":
		return []string{"--prompt-interactive", task}
	case "opencode":
		return []string{"--prompt", task}
	default: // omp, claude, codex take it positionally
		return []string{task}
	}
}

// tmuxArgs builds `workmux add`; --agent is omitted when the agent command is
// exactly workmux's configured agent.
func tmuxArgs(cfg *Config, workmuxAgent, promptFile string) []string {
	args := []string{"add"}
	if cfg.Branch != "" {
		args = append(args, cfg.Branch)
	} else {
		args = append(args, "--auto-name")
	}
	if agent := shellJoin(agentCommand(cfg)); agent != workmuxAgent {
		args = append(args, "--agent", agent)
	}
	args = append(args, "--prompt-file", promptFile)
	if cfg.Base != "" {
		args = append(args, "--base", cfg.Base)
	}
	if slices.Contains(cfg.Options, OptMoveChanges) {
		args = append(args, "--with-changes", "--include-untracked")
	}
	if slices.Contains(cfg.Options, OptNoHooks) {
		args = append(args, "--no-hooks")
	}
	if slices.Contains(cfg.Options, OptBackground) {
		args = append(args, "--background")
	}
	return args
}

func headlessArgs(cfg *Config, branch string) []string {
	args := []string{"add", branch, "--headless", "--json"}
	if cfg.Base != "" {
		args = append(args, "--base", cfg.Base)
	}
	if slices.Contains(cfg.Options, OptNoHooks) {
		args = append(args, "--no-hooks")
	}
	return args
}

func launch(cfg *Config, env *Env) error {
	if cfg.Launch == LaunchTmux {
		return launchTmux(cfg, env)
	}
	return launchHere(cfg, env)
}

func launchTmux(cfg *Config, env *Env) error {
	f, err := os.CreateTemp("", "gh-start-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(cfg.Task); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.Command("workmux", tmuxArgs(cfg, env.WorkmuxAgent, f.Name())...)
	cmd.Dir = env.Repo.Root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("workmux add: %w", err)
	}
	return nil
}

// receipt is the `workmux add --headless --json` output.
type receipt struct {
	Handle           string `json:"handle"`
	Branch           string `json:"branch"`
	WorktreePath     string `json:"worktree_path"`
	WorkingDirectory string `json:"working_directory"`
}

func launchHere(cfg *Config, env *Env) error {
	branch := cfg.Branch
	if branch == "" {
		branch = nameBranch(cfg.Task, env.Completer)
	}

	cmd := exec.Command("workmux", headlessArgs(cfg, branch)...)
	cmd.Dir = env.Repo.Root
	cmd.Stderr = os.Stderr // hook output
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("workmux add --headless: %w", err)
	}
	var r receipt
	if err := json.Unmarshal(out, &r); err != nil {
		return fmt.Errorf("parse workmux receipt %q: %w", out, err)
	}
	dir := cmp.Or(r.WorkingDirectory, r.WorktreePath)
	if dir == "" {
		return fmt.Errorf("workmux receipt has no worktree path: %s", out)
	}
	fmt.Fprintf(os.Stderr, "gh-start: worktree %s (%s) ready at %s\n", r.Handle, r.Branch, dir)

	argv := append(agentCommand(cfg), promptArgs(cfg.Agent, cfg.Task)...)
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return execAgent(bin, argv)
}

// nameBranch asks the completer for a branch name, falling back to a slug of
// the task when the model is unavailable or returns an invalid name.
func nameBranch(task string, c *Completer) string {
	name := ""
	if c != nil {
		_ = spinner.New().
			Title("Naming the branch…").
			ActionWithErr(func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				var err error
				name, err = c.BranchName(ctx, task)
				return err
			}).
			Run()
	}
	if name == "" || exec.Command("git", "check-ref-format", "--branch", name).Run() != nil {
		name = slugify(task)
	}
	if name == "" {
		name = "gh-start-" + time.Now().Format("20060102-150405")
	}
	return name
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellJoin quotes argv for sh; workmux runs --agent through a shell.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		if shellSafe.MatchString(a) {
			quoted[i] = a
		} else {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}
