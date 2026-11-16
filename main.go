// Command gh-start is a GitHub CLI extension that configures a coding agent
// session in a form, creates a workmux worktree for it, and starts the agent.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "gh-start:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	fs := flag.NewFlagSet("gh start", flag.ContinueOnError)
	agent := fs.String("agent", envOr("GH_START_AGENT", "omp"), "coding agent preselected in the form")
	completeModel := fs.String("complete-model", envOr("GH_START_COMPLETE_MODEL", "pi/smol"),
		"omp model used for autocomplete and branch names (empty: omp default)")
	noComplete := fs.Bool("no-complete", false, "disable AI autocomplete")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: gh start [flags] [task description...]\n\n"+
			"Configure a coding agent session, create a workmux worktree, and start the agent.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if _, err := exec.LookPath("workmux"); err != nil {
		return errors.New("workmux not found in PATH; install it from https://workmux.dev")
	}

	modelsCh := make(chan []Model, 1)
	go func() { modelsCh <- loadModels() }()

	repo, err := loadRepo()
	if err != nil {
		return err
	}
	agents := detectAgents(*agent)
	if len(agents) == 0 {
		return fmt.Errorf("no coding agent found in PATH (looked for %s)", strings.Join(knownAgents, ", "))
	}

	var completer *Completer
	if !*noComplete {
		if bin, err := exec.LookPath("omp"); err == nil {
			completer = &Completer{Bin: bin, Model: *completeModel, Dir: repo.Root, Context: repo.PromptContext()}
		}
	}

	_, tmuxErr := exec.LookPath("tmux")
	env := &Env{
		Repo:      repo,
		Agents:    agents,
		Models:    <-modelsCh,
		Completer: completer,
		HasTmux:   tmuxErr == nil,
	}
	cfg := &Config{
		Task:   strings.Join(fs.Args(), " "),
		Agent:  agents[0],
		Launch: LaunchHere,
	}
	if env.HasTmux && os.Getenv("TMUX") != "" {
		cfg.Launch = LaunchTmux
	}

	if err := buildForm(cfg, env).Run(); err != nil {
		return err
	}
	if !cfg.Confirmed {
		fmt.Fprintln(os.Stderr, "gh-start: cancelled")
		return nil
	}
	return launch(cfg, env)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
