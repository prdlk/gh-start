// Command gh-start is a GitHub CLI extension that configures a coding agent
// session in a form, creates a workmux worktree for it, and starts the agent.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
)

const usage = `Usage: gh start [task description...]

Configure a coding agent session, create a workmux worktree, and start the agent.
Defaults come from your environment: the agent is workmux's configured agent,
autocomplete uses omp when installed, and the launch mode is tmux inside tmux.
`

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
	if len(argv) == 1 && (argv[0] == "-h" || argv[0] == "--help" || argv[0] == "help") {
		fmt.Print(usage)
		return nil
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
	wmAgent := workmuxAgent(repo.Root)
	agents := detectAgents(firstWord(wmAgent))
	if len(agents) == 0 {
		return fmt.Errorf("no coding agent found in PATH (looked for %s)", strings.Join(knownAgents, ", "))
	}

	var completer *Completer
	if bin, err := exec.LookPath("omp"); err == nil {
		completer = &Completer{Bin: bin, Model: completeModel, Dir: repo.Root, Context: repo.PromptContext()}
	}

	_, tmuxErr := exec.LookPath("tmux")
	env := &Env{
		Repo:         repo,
		Agents:       agents,
		WorkmuxAgent: wmAgent,
		Models:       <-modelsCh,
		Completer:    completer,
		HasTmux:      tmuxErr == nil,
	}
	cfg := &Config{
		Task:      strings.Join(argv, " "),
		Agent:     agents[0],
		Launch:    LaunchHere,
		Confirmed: true, // enter through the form starts the session
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
