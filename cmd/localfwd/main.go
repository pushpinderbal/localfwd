package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/localfwd/localfwd/internal/app"
	"github.com/localfwd/localfwd/internal/ports"
	"github.com/localfwd/localfwd/internal/sshctl"
	"github.com/localfwd/localfwd/internal/tui"
)

const version = "0.3.0"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("localfwd", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var sshOptions stringList
	interval := fs.Duration("interval", 2*time.Second, "remote port discovery interval")
	bind := fs.String("bind", "127.0.0.1", "local address for forwards")
	minPort := fs.Int("min-port", 1024, "ignore remote ports below this value")
	include := fs.String("include", "", "only forward these ports/ranges (for example 3000,8000-8010)")
	exclude := fs.String("exclude", "22", "exclude these ports/ranges")
	missingScans := fs.Int("missing-scans", 3, "remove a forward after this many missed scans (0 keeps it)")
	sshPath := fs.String("ssh", "ssh", "path to the OpenSSH client")
	connectTimeout := fs.Duration("connect-timeout", 30*time.Second, "time allowed to establish SSH")
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Var(&sshOptions, "ssh-option", "additional ssh -o option (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: localfwd [options] <ssh-destination>\n\n")
		fmt.Fprintf(stderr, "Discover listening TCP ports on an SSH host and forward them locally.\n")
		fmt.Fprintf(stderr, "The destination uses your normal OpenSSH config (for example devbox or user@host).\n\nOptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "localfwd %s\n", version)
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *interval <= 0 || *connectTimeout <= 0 {
		fmt.Fprintln(stderr, "error: interval and connect-timeout must be positive")
		return 2
	}
	if *minPort < 1 || *minPort > 65535 {
		fmt.Fprintln(stderr, "error: min-port must be between 1 and 65535")
		return 2
	}
	if *missingScans < 0 {
		fmt.Fprintln(stderr, "error: missing-scans cannot be negative")
		return 2
	}

	includeSet, err := ports.ParseSet(*include)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid --include: %v\n", err)
		return 2
	}
	excludeSet, err := ports.ParseSet(*exclude)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid --exclude: %v\n", err)
		return 2
	}
	if err := validateBind(*bind); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := sshctl.New(sshctl.Config{
		Path:        *sshPath,
		Destination: fs.Arg(0),
		Options:     sshOptions,
		Stdin:       os.Stdin,
		Stderr:      stderr,
	})
	runner := app.Runner{
		SSH:            client,
		Destination:    fs.Arg(0),
		Bind:           *bind,
		Interval:       *interval,
		ConnectTimeout: *connectTimeout,
		MinPort:        *minPort,
		Include:        includeSet,
		Exclude:        excludeSet,
		MissingScans:   *missingScans,
	}
	return runTUI(ctx, &runner, stderr)
}

func runTUI(ctx context.Context, runner *app.Runner, stderr io.Writer) int {
	fmt.Fprintf(stderr, "connecting to %s…\n", runner.Destination)
	if err := runner.Connect(ctx); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := make(chan app.Event, 256)
	actions := make(chan app.Action, 32)
	uiDone := make(chan struct{})
	result := make(chan error, 1)
	runner.Actions = actions
	runner.Notify = func(event app.Event) {
		select {
		case events <- event:
		case <-runCtx.Done():
		}
	}
	go func() {
		err := runner.RunConnected(runCtx)
		close(uiDone)
		result <- err
	}()

	uiErr := tui.Run(runner.Destination, events, uiDone, actions)
	cancel()
	runErr := <-result
	if uiErr != nil {
		fmt.Fprintf(stderr, "error: terminal UI: %v\n", uiErr)
		return 1
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(stderr, "error: %v\n", runErr)
		return 1
	}
	return 0
}

func validateBind(value string) error {
	if value == "" {
		return errors.New("bind address cannot be empty")
	}
	if _, err := strconv.Atoi(value); err == nil {
		return errors.New("bind must be an address, not a port")
	}
	return nil
}
