package app

import (
	"context"
	"testing"
	"time"

	"github.com/localfwd/localfwd/internal/discovery"
	"github.com/localfwd/localfwd/internal/ports"
)

type recordedForward struct {
	bind       string
	localPort  int
	remoteHost string
	remotePort int
}

type fakeSSH struct {
	discoveries [][]discovery.Listener
	added       []recordedForward
	removed     []recordedForward
	done        chan error
}

func (f *fakeSSH) Start(context.Context, time.Duration) error { return nil }
func (f *fakeSSH) Close() error                               { return nil }
func (f *fakeSSH) Done() <-chan error                         { return f.done }
func (f *fakeSSH) Discover(context.Context) ([]discovery.Listener, error) {
	result := f.discoveries[0]
	f.discoveries = f.discoveries[1:]
	return result, nil
}
func (f *fakeSSH) AddForward(_ context.Context, bind string, localPort int, remoteHost string, remotePort int) error {
	f.added = append(f.added, recordedForward{bind, localPort, remoteHost, remotePort})
	return nil
}
func (f *fakeSSH) RemoveForward(_ context.Context, bind string, localPort int, remoteHost string, remotePort int) error {
	f.removed = append(f.removed, recordedForward{bind, localPort, remoteHost, remotePort})
	return nil
}

func TestScanAddsFiltersAndRemovesForwards(t *testing.T) {
	include, err := ports.ParseSet("3000,5173")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeSSH{discoveries: [][]discovery.Listener{
		{
			{Port: 22, Address: "0.0.0.0", Process: "sshd"},
			{Port: 3000, Address: "127.0.0.1", Process: "node"},
			{Port: 5173, Address: "::", Process: "vite"},
			{Port: 9000, Address: "0.0.0.0", Process: "other"},
		},
		{},
	}}
	var events []Event
	runner := Runner{
		SSH:          fake,
		Bind:         "127.0.0.1",
		MinPort:      1024,
		Include:      include,
		MissingScans: 1,
		Notify:       func(event Event) { events = append(events, event) },
		active:       make(map[int]*forward),
	}

	if err := runner.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.added) != 2 {
		t.Fatalf("added %d forwards, want 2: %#v", len(fake.added), fake.added)
	}
	if fake.added[0].remotePort != 3000 || fake.added[0].remoteHost != "127.0.0.1" {
		t.Errorf("unexpected first forward: %#v", fake.added[0])
	}
	if fake.added[1].remotePort != 5173 || fake.added[1].remoteHost != "::1" {
		t.Errorf("unexpected second forward: %#v", fake.added[1])
	}

	if err := runner.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.removed) != 2 {
		t.Fatalf("removed %d forwards, want 2: %#v", len(fake.removed), fake.removed)
	}
	if len(events) != 4 {
		t.Fatalf("emitted %d events, want 4", len(events))
	}
}

func TestRemoteTarget(t *testing.T) {
	tests := map[string]string{
		"":          "127.0.0.1",
		"*":         "127.0.0.1",
		"0.0.0.0":   "127.0.0.1",
		"::":        "::1",
		"::1":       "::1",
		"10.0.0.15": "10.0.0.15",
	}
	for input, want := range tests {
		if got := remoteTarget(input); got != want {
			t.Errorf("remoteTarget(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestToggleDisablesAndReenablesForward(t *testing.T) {
	fake := &fakeSSH{}
	var events []Event
	runner := Runner{
		SSH:       fake,
		Bind:      "127.0.0.1",
		Notify:    func(event Event) { events = append(events, event) },
		active:    map[int]*forward{32123: {localPort: 32123, remoteHost: "127.0.0.1"}},
		disabled:  make(map[int]bool),
		available: map[int]discovery.Listener{32123: {Port: 32123, Address: "127.0.0.1", Process: "server"}},
	}

	runner.toggle(context.Background(), 32123)
	if !runner.disabled[32123] || len(fake.removed) != 1 {
		t.Fatalf("forward was not disabled: disabled=%v removed=%#v", runner.disabled, fake.removed)
	}
	if events[len(events)-1].Type != EventDisabled {
		t.Fatalf("last event = %q, want %q", events[len(events)-1].Type, EventDisabled)
	}

	runner.toggle(context.Background(), 32123)
	if runner.disabled[32123] || len(fake.added) != 1 {
		t.Fatalf("forward was not re-enabled: disabled=%v added=%#v", runner.disabled, fake.added)
	}
	if events[len(events)-1].Type != EventForwarded {
		t.Fatalf("last event = %q, want %q", events[len(events)-1].Type, EventForwarded)
	}
}
