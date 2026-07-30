package main

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/cli"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/tui"
)

func main() {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		cli.ExitUsage(err)
	}
	if opts.Help {
		cli.WriteUsage(os.Stdout)
		return
	}
	if opts.Version {
		cli.WriteVersion(os.Stdout)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		if path := config.Path(); path != "" {
			fmt.Fprintf(os.Stderr, "Fix %s or delete it, then run zeta again.\n", path)
		}
		os.Exit(1)
	}

	// Folder trust before workspace/session load (AGENTS.md, project sessions).
	if err := cli.EnsureTrusted(); err != nil {
		if errors.Is(err, cli.ErrTrustDeclined) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "trust: %v\n", err)
		os.Exit(1)
	}

	tuiOpts := tui.Options{}
	if opts.Resume {
		if opts.ResumeID != "" {
			tuiOpts.ResumeID = opts.ResumeID
		} else {
			tuiOpts.Picker = true
		}
	}

	model, err := tui.New(cfg, tuiOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
	if m, ok := final.(tui.Model); ok {
		if id := m.PersistedSessionID(); id != "" {
			fmt.Printf("To resume this session: zeta --resume=%s\n", id)
		}
	}
}
