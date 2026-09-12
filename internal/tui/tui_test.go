package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/localfwd/localfwd/internal/app"
)

func TestEventsRenderStatuses(t *testing.T) {
	m := &model{
		destination: "devbox",
		items:       make(map[int]*item),
		height:      24,
		color:       false,
	}
	m.applyEvent(app.Event{Type: app.EventForwarded, RemotePort: 3000, LocalPort: 3000, Bind: "127.0.0.1", Process: "node"})
	m.applyEvent(app.Event{Type: app.EventDisabled, RemotePort: 5173, LocalPort: 5173, Bind: "127.0.0.1", Process: "vite"})
	m.applyEvent(app.Event{Type: app.EventRemoved, RemotePort: 8000})

	view := m.View().Content
	for _, text := range []string{"forwarded", "disabled", "unavailable", ":3000", "127.0.0.1:3000", "node"} {
		if !strings.Contains(view, text) {
			t.Errorf("view does not contain %q:\n%s", text, view)
		}
	}
}

func TestMouseClickTogglesClickedRow(t *testing.T) {
	actions := make(chan app.Action, 1)
	m := &model{
		items:   make(map[int]*item),
		actions: actions,
		height:  24,
	}
	m.applyEvent(app.Event{Type: app.EventForwarded, RemotePort: 3000, LocalPort: 3000})
	m.applyEvent(app.Event{Type: app.EventForwarded, RemotePort: 5173, LocalPort: 5173})

	_, cmd := m.Update(tea.MouseClickMsg{X: 4, Y: firstItemRow + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("mouse click did not return a toggle command")
	}
	_ = cmd()
	action := <-actions
	if action.RemotePort != 5173 {
		t.Fatalf("unexpected action: %#v", action)
	}
}
