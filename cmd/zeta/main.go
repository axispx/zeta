package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
