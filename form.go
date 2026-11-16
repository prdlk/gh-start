package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
)

// Launch modes.
const (
	LaunchTmux = "tmux" // workmux creates a tmux target and starts the agent there
	LaunchHere = "here" // headless worktree, agent replaces this process
)

// Worktree option values.
const (
	OptMoveChanges = "move-changes"
	OptNoHooks     = "no-hooks"
	OptBackground  = "background"
)

// Config is what the form collects. Fields are exported so huh can hash the
// struct when recomputing the summary.
type Config struct {
	Task      string
	Launch    string
	Agent     string
	Model     string
	Thinking  string
	Approval  string
	Branch    string
	Base      string
	Options   []string
	Confirmed bool
}

// Env is what gh-start discovered about the machine and repository.
type Env struct {
	Repo      *Repo
	Agents    []string
	Models    []Model
	Completer *Completer
	HasTmux   bool
}

func buildForm(cfg *Config, env *Env) *huh.Form {
	task := NewAIText(&cfg.Task, env.Completer)
	desc := "alt+enter / ctrl+j new line · ctrl+e $EDITOR"
	if env.Completer == nil {
		desc += " · autocomplete off (omp not found or --no-complete)"
	}
	task.Text.
		Title("What should the agent change?").
		Description(desc).
		Placeholder("e.g. Add a --json flag to the list command that prints machine-readable output").
		Lines(6).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("describe the change")
			}
			return nil
		})

	launches := []huh.Option[string]{huh.NewOption("This terminal (headless worktree, agent runs here)", LaunchHere)}
	if env.HasTmux {
		tmux := huh.NewOption("tmux via workmux (new window/session for the worktree)", LaunchTmux)
		launches = append([]huh.Option[string]{tmux}, launches...)
	}

	agents := make([]huh.Option[string], len(env.Agents))
	for i, a := range env.Agents {
		agents[i] = huh.NewOption(a, a)
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("gh start").
				Description(fmt.Sprintf("%s · on %s\nDescribe the change; the agent starts in a fresh workmux worktree.",
					env.Repo.Name, env.Repo.Branch)),
			task,
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Coding agent").Options(agents...).Value(&cfg.Agent),
			huh.NewSelect[string]().Title("Launch").Options(launches...).Value(&cfg.Launch),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("omp model").
				Description("/ to filter").
				Options(modelOptions(env.Models)...).
				Height(10).
				Value(&cfg.Model),
			huh.NewSelect[string]().
				Title("Thinking level").
				OptionsFunc(func() []huh.Option[string] { return thinkingOptions(env.Models, cfg.Model) }, &cfg.Model).
				Value(&cfg.Thinking),
			huh.NewSelect[string]().
				Title("Tool approval").
				Options(
					huh.NewOption("Settings default", ""),
					huh.NewOption("Always ask", "always-ask"),
					huh.NewOption("Always ask (cache approvals)", "always-ask-cached"),
					huh.NewOption("Ask only for writes", "write"),
					huh.NewOption("Yolo (approve everything)", "yolo"),
				).
				Value(&cfg.Approval),
		).WithHideFunc(func() bool { return cfg.Agent != "omp" }),
		huh.NewGroup(
			huh.NewInput().
				Title("Branch").
				Placeholder("blank: AI-generated from the task").
				Value(&cfg.Branch).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					if exec.Command("git", "check-ref-format", "--branch", s).Run() != nil {
						return fmt.Errorf("%q is not a valid branch name", s)
					}
					return nil
				}),
			huh.NewInput().
				Title("Base").
				Placeholder("blank: workmux base_branch (or current branch)").
				Suggestions(env.Repo.Branches).
				Value(&cfg.Base).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					if _, err := git(env.Repo.Root, "rev-parse", "--verify", "--quiet", s+"^{commit}"); err != nil {
						return fmt.Errorf("%q is not a branch, tag, or commit", s)
					}
					return nil
				}),
			huh.NewMultiSelect[string]().
				Title("Worktree options").
				OptionsFunc(func() []huh.Option[string] { return worktreeOptions(cfg.Launch) }, &cfg.Launch).
				Value(&cfg.Options),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Start the session?").
				DescriptionFunc(func() string { return summary(cfg, env) }, cfg).
				Affirmative("Start").
				Negative("Cancel").
				Value(&cfg.Confirmed),
		),
	).
		WithTheme(huh.ThemeFunc(huh.ThemeCharm)).
		WithAccessible(os.Getenv("ACCESSIBLE") != "")
}

func modelOptions(models []Model) []huh.Option[string] {
	opts := []huh.Option[string]{huh.NewOption("Settings default", "")}
	for _, m := range models {
		label := m.Selector
		if m.Name != "" {
			label = fmt.Sprintf("%s  (%s)", m.Name, m.Selector)
		}
		opts = append(opts, huh.NewOption(label, m.Selector))
	}
	return opts
}

var allThinking = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

func thinkingOptions(models []Model, selector string) []huh.Option[string] {
	levels := allThinking
	for _, m := range models {
		if m.Selector == selector {
			levels = m.Thinking
			break
		}
	}
	opts := []huh.Option[string]{huh.NewOption("Settings default", ""), huh.NewOption("off", "off")}
	for _, l := range levels {
		opts = append(opts, huh.NewOption(l, l))
	}
	if len(levels) > 0 {
		opts = append(opts, huh.NewOption("auto", "auto"))
	}
	return opts
}

func worktreeOptions(launch string) []huh.Option[string] {
	if launch == LaunchHere { // workmux --headless supports hooks/file ops only
		return []huh.Option[string]{huh.NewOption("Skip post-create hooks", OptNoHooks)}
	}
	return []huh.Option[string]{
		huh.NewOption("Move uncommitted changes (incl. untracked) to the worktree", OptMoveChanges),
		huh.NewOption("Skip post-create hooks", OptNoHooks),
		huh.NewOption("Open in background (don't switch to it)", OptBackground),
	}
}

func summary(cfg *Config, env *Env) string {
	branch := cfg.Branch
	if branch == "" {
		branch = "(AI-named from the task)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repo    %s\nbranch  %s\nagent   %s\n", env.Repo.Root, branch, shellJoin(agentCommand(cfg)))
	if cfg.Launch == LaunchTmux {
		fmt.Fprintf(&b, "\n$ %s", shellJoin(append([]string{"workmux"}, tmuxArgs(cfg, "<task.md>")...)))
	} else {
		fmt.Fprintf(&b, "\n$ %s\n$ cd <worktree> && %s <task>",
			shellJoin(append([]string{"workmux"}, headlessArgs(cfg, cmp.Or(cfg.Branch, "<branch>"))...)),
			shellJoin(agentCommand(cfg)))
	}
	return b.String()
}
