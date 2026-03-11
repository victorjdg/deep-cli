package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(setupCmd)
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up local Ollama environment",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			// For setup, default to local even without config.
			cfg = &config.Config{
				UseLocal:   true,
				Model:      "deepseek-coder:6.7b",
				OllamaHost: "http://localhost:11434",
			}
		}

		if !cfg.UseLocal {
			fmt.Println("Setup is only needed for local mode.")
			fmt.Println("You're configured for cloud mode with an API key.")
			return nil
		}

		return runSetup(cfg)
	},
}

func runSetup(cfg *config.Config) error {
	bold := lipgloss.NewStyle().Bold(true)
	success := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// Step 1: Check Ollama installation
	fmt.Println(bold.Render("Checking Ollama installation..."))
	_, err := exec.LookPath("ollama")
	if err != nil {
		fmt.Println(errStyle.Render("Ollama is not installed."))
		fmt.Println()
		fmt.Println("Install Ollama:")
		fmt.Println("  macOS:   brew install ollama")
		fmt.Println("  Linux:   curl -fsSL https://ollama.ai/install.sh | sh")
		fmt.Println("  Windows: Download from https://ollama.ai/download")
		return fmt.Errorf("ollama not found")
	}
	fmt.Println(success.Render("  Ollama is installed"))

	// Step 2: Check connection
	fmt.Println(bold.Render("Checking Ollama connection..."))
	client := api.NewOllamaClient(cfg.OllamaHost, cfg.Model)
	ctx := context.Background()

	if err := client.CheckConnection(ctx); err != nil {
		fmt.Println("  Ollama is not running. Attempting to start...")
		startCmd := exec.Command("ollama", "serve")
		startCmd.Stdout = nil
		startCmd.Stderr = nil
		if err := startCmd.Start(); err != nil {
			fmt.Println(errStyle.Render("  Failed to start Ollama."))
			fmt.Println("  Please start it manually: ollama serve")
			return err
		}
		time.Sleep(3 * time.Second)

		if err := client.CheckConnection(ctx); err != nil {
			fmt.Println(errStyle.Render("  Could not connect to Ollama."))
			fmt.Println("  Please start it manually: ollama serve")
			return err
		}
	}
	fmt.Println(success.Render("  Ollama is running"))

	// Step 3: Check model
	fmt.Printf(bold.Render("Checking model %s...\n"), cfg.Model)
	models, err := client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	found := false
	for _, m := range models {
		if m == cfg.Model {
			found = true
			break
		}
	}

	if found {
		fmt.Println(success.Render("  Model is already installed"))
	} else {
		fmt.Printf("  Model %s not found. Download it? [y/N] ", cfg.Model)
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Skipping model download.")
			return nil
		}

		fmt.Printf("  Pulling %s (this may take a while)...\n", cfg.Model)
		p := tea.NewProgram(newPullModel(cfg.Model))
		result, err := p.Run()
		if err != nil {
			return err
		}
		pm := result.(pullModel)
		if pm.err != nil {
			return fmt.Errorf("failed to pull model: %w", pm.err)
		}
		fmt.Println(success.Render("  Model downloaded successfully"))
	}

	fmt.Println()
	fmt.Println(success.Render("Setup complete! Run 'deepseek' to start."))
	return nil
}

// pullModel is a minimal BubbleTea model for showing a spinner during model pull.
type pullModel struct {
	spinner spinner.Model
	model   string
	done    bool
	err     error
}

type pullDoneMsg struct{ err error }

func newPullModel(model string) pullModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return pullModel{spinner: s, model: model}
}

func (m pullModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.pull)
}

func (m pullModel) pull() tea.Msg {
	cmd := exec.Command("ollama", "pull", m.model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return pullDoneMsg{err: err}
}

func (m pullModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pullDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pullModel) View() string {
	if m.done {
		return ""
	}
	return m.spinner.View() + " Downloading model...\n"
}
