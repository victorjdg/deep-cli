package markdown

import (
	"github.com/charmbracelet/glamour"
)

var renderer *glamour.TermRenderer

func init() {
	var err error
	renderer, err = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		// Fallback: create a renderer without auto style.
		renderer, _ = glamour.NewTermRenderer(
			glamour.WithWordWrap(100),
		)
	}
}

// Render renders markdown content to styled terminal output.
func Render(content string) (string, error) {
	if renderer == nil {
		return content, nil
	}
	return renderer.Render(content)
}
