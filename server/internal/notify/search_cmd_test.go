package notify

import (
	"strings"
	"testing"

	"server/internal/config"
)

func TestBotWelcomeAndHelpIncludeProjectURL(t *testing.T) {
	if config.ProjectURL == "" {
		t.Fatal("config.ProjectURL should not be empty")
	}
	welcome := botWelcomeHTML()
	help := botHelpHTML()
	if !strings.Contains(welcome, config.ProjectURL) {
		t.Fatalf("welcome should include project url, got %q", welcome)
	}
	if !strings.Contains(help, config.ProjectURL) {
		t.Fatalf("help should include project url, got %q", help)
	}
	line := botProjectHTML()
	if !strings.Contains(line, `href="`+config.ProjectURL+`"`) {
		t.Fatalf("project line should be a clickable href, got %q", line)
	}
}
