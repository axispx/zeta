package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/cli"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/policy"
	"github.com/axispx/zeta/internal/tui"
	"github.com/axispx/zeta/internal/update"
	"github.com/axispx/zeta/internal/version"
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

	rules, err := policy.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "permissions: %v\n", err)
		if path := policy.Path(); path != "" {
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

	tuiOpts := tui.Options{Rules: rules}
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
	m, ok := final.(tui.Model)
	if ok && m.UpdateRequested() {
		// /update: the TUI is gone, so apply the release here in the CLI and
		// hand the terminal back to a fresh zeta.
		if err := runUpdate(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(1)
		}
		if err := relaunch(); err != nil {
			fmt.Fprintf(os.Stderr, "restart zeta: %v\n", err)
			os.Exit(1)
		}
	}
	if ok {
		if id := m.PersistedSessionID(); id != "" {
			fmt.Printf("To resume this session: zeta --resume=%s\n", id)
		}
	}
}

// runUpdate applies the latest release in the CLI, printing what changed. Dev
// builds have no release to download, so they get a synthetic update: the same
// close → update → restart handoff minus the network.
func runUpdate(w io.Writer) error {
	fmt.Fprintln(w, "Updating zeta…")
	if update.IsDev(version.Version) {
		fmt.Fprintln(w, "Dev build: synthetic update (no release download)")
		return nil
	}
	res, err := update.Apply(context.Background(), update.Options{Current: version.Version})
	if err != nil {
		return err
	}
	if res.AlreadyLatest {
		fmt.Fprintf(w, "zeta %s is up to date\n", res.From)
		return nil
	}
	fmt.Fprintf(w, "Updated %s → %s\n", res.From, res.To)
	return nil
}

// relaunch replaces this process with a fresh zeta so the terminal is handed to
// the just-installed binary. Only returns on failure.
func relaunch() error {
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
