package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/pushpinderbal/localfwd/internal/discovery"
	"github.com/pushpinderbal/localfwd/internal/ports"
	"github.com/pushpinderbal/localfwd/internal/sshctl"
)

const (
	EventConnected = "connected"
	EventForwarded = "forwarded"
	EventDisabled  = "disabled"
	EventRemoved   = "removed"
	EventScanError = "scan_error"
)

type Event struct {
	Type        string
	Destination string
	Bind        string
	RemotePort  int
	LocalPort   int
	Process     string
	Message     string
}

type SSH interface {
	Start(context.Context, time.Duration) error
	Discover(context.Context) ([]discovery.Listener, error)
	AddForward(context.Context, string, int, string, int) error
	RemoveForward(context.Context, string, int, string, int) error
	Done() <-chan error
	Close() error
}

type Action struct {
	RemotePort int
}

type Runner struct {
	SSH            SSH
	Destination    string
	Bind           string
	Interval       time.Duration
	ConnectTimeout time.Duration
	MinPort        int
	Include        ports.Set
	Exclude        ports.Set
	MissingScans   int
	Notify         func(Event)
	Actions        <-chan Action

	active    map[int]*forward
	disabled  map[int]bool
	available map[int]discovery.Listener
}

type forward struct {
	localPort  int
	remoteHost string
	missing    int
}

// Connect establishes the SSH control connection without starting discovery.
// This keeps authentication prompts outside the full-screen terminal interface.
func (r *Runner) Connect(ctx context.Context) error {
	if r.SSH == nil {
		return errors.New("SSH client is required")
	}
	if r.Notify == nil {
		r.Notify = func(Event) {}
	}
	return r.SSH.Start(ctx, r.ConnectTimeout)
}

// RunConnected performs discovery on an already established SSH connection.
func (r *Runner) RunConnected(ctx context.Context) error {
	defer r.SSH.Close()
	r.active = make(map[int]*forward)
	r.disabled = make(map[int]bool)
	r.available = make(map[int]discovery.Listener)
	r.emit(Event{Type: EventConnected, Destination: r.Destination})

	if err := r.scan(ctx); err != nil {
		r.emit(Event{Type: EventScanError, Message: err.Error()})
	}
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	actions := r.Actions

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-r.SSH.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil {
				return errors.New("SSH connection closed")
			}
			return fmt.Errorf("SSH connection closed: %w", err)
		case <-ticker.C:
			if err := r.scan(ctx); err != nil {
				r.emit(Event{Type: EventScanError, Message: err.Error()})
			}
		case action, ok := <-actions:
			if !ok {
				actions = nil
				continue
			}
			r.toggle(ctx, action.RemotePort)
		}
	}
}

func (r *Runner) scan(ctx context.Context) error {
	listeners, err := r.SSH.Discover(ctx)
	if err != nil {
		return err
	}
	r.available = make(map[int]discovery.Listener, len(listeners))
	seen := make(map[int]struct{}, len(listeners))
	for _, listener := range listeners {
		if !r.allowed(listener.Port) {
			continue
		}
		seen[listener.Port] = struct{}{}
		r.available[listener.Port] = listener
		if r.disabled[listener.Port] {
			continue
		}
		if existing, ok := r.active[listener.Port]; ok {
			existing.missing = 0
			continue
		}

		localPort, err := sshctl.AvailablePort(r.Bind, listener.Port)
		if err != nil {
			r.emit(Event{Type: EventScanError, RemotePort: listener.Port, Message: err.Error()})
			continue
		}
		remoteHost := remoteTarget(listener.Address)
		if err := r.SSH.AddForward(ctx, r.Bind, localPort, remoteHost, listener.Port); err != nil {
			r.emit(Event{Type: EventScanError, RemotePort: listener.Port, Message: err.Error()})
			continue
		}
		r.active[listener.Port] = &forward{localPort: localPort, remoteHost: remoteHost}
		r.emit(Event{Type: EventForwarded, Bind: r.Bind, RemotePort: listener.Port, LocalPort: localPort, Process: listener.Process})
	}

	if r.MissingScans == 0 {
		return nil
	}
	var remotePorts []int
	for remotePort := range r.active {
		remotePorts = append(remotePorts, remotePort)
	}
	sort.Ints(remotePorts)
	for _, remotePort := range remotePorts {
		if _, ok := seen[remotePort]; ok {
			continue
		}
		current := r.active[remotePort]
		current.missing++
		if current.missing < r.MissingScans {
			continue
		}
		if err := r.SSH.RemoveForward(ctx, r.Bind, current.localPort, current.remoteHost, remotePort); err != nil {
			r.emit(Event{Type: EventScanError, RemotePort: remotePort, Message: err.Error()})
			continue
		}
		delete(r.active, remotePort)
		r.emit(Event{Type: EventRemoved, Bind: r.Bind, RemotePort: remotePort, LocalPort: current.localPort})
	}
	return nil
}

func (r *Runner) toggle(ctx context.Context, remotePort int) {
	if current, ok := r.active[remotePort]; ok {
		if err := r.SSH.RemoveForward(ctx, r.Bind, current.localPort, current.remoteHost, remotePort); err != nil {
			r.emit(Event{Type: EventScanError, RemotePort: remotePort, Message: err.Error()})
			return
		}
		delete(r.active, remotePort)
		r.disabled[remotePort] = true
		r.emit(Event{Type: EventDisabled, Bind: r.Bind, RemotePort: remotePort, LocalPort: current.localPort})
		return
	}

	listener, ok := r.available[remotePort]
	if !ok {
		r.emit(Event{Type: EventScanError, RemotePort: remotePort, Message: fmt.Sprintf("remote port %d is not currently listening", remotePort)})
		return
	}
	localPort, err := sshctl.AvailablePort(r.Bind, remotePort)
	if err != nil {
		r.emit(Event{Type: EventScanError, RemotePort: remotePort, Message: err.Error()})
		return
	}
	remoteHost := remoteTarget(listener.Address)
	if err := r.SSH.AddForward(ctx, r.Bind, localPort, remoteHost, remotePort); err != nil {
		r.emit(Event{Type: EventScanError, RemotePort: remotePort, Message: err.Error()})
		return
	}
	delete(r.disabled, remotePort)
	r.active[remotePort] = &forward{localPort: localPort, remoteHost: remoteHost}
	r.emit(Event{Type: EventForwarded, Bind: r.Bind, RemotePort: remotePort, LocalPort: localPort, Process: listener.Process})
}

func remoteTarget(address string) string {
	switch address {
	case "", "*", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	default:
		return address
	}
}

func (r *Runner) allowed(port int) bool {
	if port < r.MinPort || r.Exclude.Contains(port) {
		return false
	}
	return len(r.Include) == 0 || r.Include.Contains(port)
}

func (r *Runner) emit(event Event) {
	r.Notify(event)
}
