package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/liam-od/trawl/internal/fs"
	"github.com/liam-od/trawl/internal/ui"
)

var version = "0.0.0-dev"

// main is a temporary M3 launcher: it opens two local panels so the TUI shell
// can be exercised with `make run`. M5 (CLI entry) replaces this with argument
// parsing and a real local+remote (SFTP) wiring.
func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}

	local := fs.NewLocal()
	model := ui.New(local, local, home, "/tmp")

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "trawl: %v\n", err)
		os.Exit(1)
	}
}
