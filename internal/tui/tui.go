package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/pushpinderbal/localfwd/internal/app"
)

const firstItemRow = 3

type portStatus int

const (
	statusUnavailable portStatus = iota
	statusDisabled
	statusForwarded
)

type item struct {
	remotePort int
	localPort  int
	bind       string
	process    string
	status     portStatus
}

type runnerDoneMsg struct{}

type model struct {
	destination string
	events      <-chan app.Event
	done        <-chan struct{}
	actions     chan<- app.Action
	items       map[int]*item
	ports       []int
	cursor      int
	height      int
	message     string
	color       bool
}

// Run starts the full-screen interactive port-forward manager.
func Run(destination string, events <-chan app.Event, done <-chan struct{}, actions chan<- app.Action) error {
	m := &model{
		destination: destination,
		events:      events,
		done:        done,
		actions:     actions,
		items:       make(map[int]*item),
		height:      24,
		color:       os.Getenv("NO_COLOR") == "",
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return m.waitForUpdate()
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case app.Event:
		m.applyEvent(msg)
		return m, m.waitForUpdate()
	case runnerDoneMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.keepCursorVisible()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.ports)-1 {
				m.cursor++
			}
		case "enter", "space":
			return m, m.toggleSelected()
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			start, _ := m.visibleRange()
			index := start + msg.Y - firstItemRow
			if index >= start && index < len(m.ports) && index < start+m.visibleRows() {
				m.cursor = index
				return m, m.toggleSelected()
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.ports)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	var view strings.Builder
	fmt.Fprintf(&view, " %s  %s\n\n", m.style("1;36", "localfwd"), m.destination)
	fmt.Fprintln(&view, "   STATUS       REMOTE    LOCAL              PROCESS")

	start, end := m.visibleRange()
	if len(m.ports) == 0 {
		fmt.Fprintln(&view, "   Waiting for remote TCP listeners…")
	} else {
		for index := start; index < end; index++ {
			entry := m.items[m.ports[index]]
			cursor := " "
			if index == m.cursor {
				cursor = m.style("1;36", ">")
			}
			dot, label := m.status(entry.status)
			local := "—"
			if entry.localPort != 0 {
				bind := entry.bind
				if bind == "" {
					bind = "127.0.0.1"
				}
				if strings.Contains(bind, ":") && !strings.HasPrefix(bind, "[") {
					bind = "[" + bind + "]"
				}
				local = fmt.Sprintf("%s:%d", bind, entry.localPort)
			}
			fmt.Fprintf(&view, "%s  %s %-11s :%-7d %-18s %s\n", cursor, dot, label, entry.remotePort, local, entry.process)
		}
	}

	fmt.Fprintln(&view)
	if m.message != "" {
		fmt.Fprintf(&view, " %s\n", m.message)
	}
	fmt.Fprint(&view, " Click a row or use ↑/↓ + Space/Enter to toggle • q to quit")

	v := tea.NewView(view.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "localfwd — " + m.destination
	return v
}

func (m *model) applyEvent(event app.Event) {
	switch event.Type {
	case app.EventConnected:
		m.message = "Connected; watching for listening ports"
		return
	case app.EventScanError:
		m.message = m.style("31", event.Message)
		if event.RemotePort != 0 {
			m.ensureItem(event.RemotePort)
		}
		return
	case app.EventForwarded, app.EventDisabled, app.EventRemoved:
		entry := m.ensureItem(event.RemotePort)
		if event.Process != "" {
			entry.process = event.Process
		}
		if event.LocalPort != 0 {
			entry.localPort = event.LocalPort
		}
		if event.Bind != "" {
			entry.bind = event.Bind
		}
		switch event.Type {
		case app.EventForwarded:
			entry.status = statusForwarded
			m.message = fmt.Sprintf("Forwarded remote :%d to 127.0.0.1:%d", event.RemotePort, event.LocalPort)
		case app.EventDisabled:
			entry.status = statusDisabled
			m.message = fmt.Sprintf("Disabled remote :%d", event.RemotePort)
		case app.EventRemoved:
			entry.status = statusUnavailable
			entry.localPort = 0
			m.message = fmt.Sprintf("Remote :%d is no longer listening", event.RemotePort)
		}
	}
}

func (m *model) ensureItem(port int) *item {
	if entry, ok := m.items[port]; ok {
		return entry
	}
	selectedPort := 0
	if len(m.ports) > 0 && m.cursor < len(m.ports) {
		selectedPort = m.ports[m.cursor]
	}
	entry := &item{remotePort: port}
	m.items[port] = entry
	m.ports = append(m.ports, port)
	sort.Ints(m.ports)
	if selectedPort != 0 {
		m.cursor = sort.SearchInts(m.ports, selectedPort)
	}
	return entry
}

func (m *model) toggleSelected() tea.Cmd {
	if len(m.ports) == 0 || m.cursor >= len(m.ports) {
		return nil
	}
	port := m.ports[m.cursor]
	if m.items[port].status == statusUnavailable {
		m.message = fmt.Sprintf("Remote :%d is not currently listening", port)
		return nil
	}
	return func() tea.Msg {
		m.actions <- app.Action{RemotePort: port}
		return nil
	}
}

func (m *model) waitForUpdate() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-m.events:
			return event
		case <-m.done:
			return runnerDoneMsg{}
		}
	}
}

func (m *model) visibleRows() int {
	rows := m.height - 7
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *model) visibleRange() (int, int) {
	rows := m.visibleRows()
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := start + rows
	if end > len(m.ports) {
		end = len(m.ports)
	}
	return start, end
}

func (m *model) keepCursorVisible() {
	if m.cursor >= len(m.ports) && len(m.ports) > 0 {
		m.cursor = len(m.ports) - 1
	}
}

func (m *model) status(status portStatus) (string, string) {
	switch status {
	case statusForwarded:
		return m.style("32", "●"), "forwarded"
	case statusDisabled:
		return m.style("31", "●"), "disabled"
	default:
		return m.style("90", "●"), "unavailable"
	}
}

func (m *model) style(code, value string) string {
	if !m.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
