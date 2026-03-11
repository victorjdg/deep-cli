package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/config"
	"github.com/victorjdg/deep-cli/internal/markdown"
	"github.com/victorjdg/deep-cli/internal/session"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(chatCmd)
}

var chatCmd = &cobra.Command{
	Use:   "chat [prompt]",
	Short: "Send a single prompt and get a response",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		prompt := strings.Join(args, " ")
		client := api.NewClient(cfg)
		sess := session.New(cfg.Model)
		sess.AddUser(prompt)

		p := tea.NewProgram(newChatModel(client, sess))
		m, err := p.Run()
		if err != nil {
			return err
		}

		cm := m.(chatModel)
		if cm.err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", cm.err)
			os.Exit(1)
		}

		return nil
	},
}

// chatModel is a minimal BubbleTea model for the non-interactive chat command.
type chatModel struct {
	spinner  spinner.Model
	client   api.Client
	session  *session.Session
	response string
	err      error
	done     bool
}

type chatResponseMsg struct {
	content string
	err     error
}

func newChatModel(client api.Client, sess *session.Session) chatModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return chatModel{
		spinner: s,
		client:  client,
		session: sess,
	}
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchResponse)
}

func (m chatModel) fetchResponse() tea.Msg {
	content, _, err := m.client.Complete(context.Background(), m.session.Messages)
	return chatResponseMsg{content: content, err: err}
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case chatResponseMsg:
		m.done = true
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.response = msg.content
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m chatModel) View() string {
	if m.done {
		if m.err != nil {
			return ""
		}
		rendered, err := markdown.Render(m.response)
		if err != nil {
			return m.response + "\n"
		}
		return rendered
	}
	return m.spinner.View() + " Thinking...\n"
}
