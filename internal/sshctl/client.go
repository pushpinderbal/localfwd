package sshctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/localfwd/localfwd/internal/discovery"
)

type Config struct {
	Path        string
	Destination string
	Options     []string
	Stdin       io.Reader
	Stderr      io.Writer
}

type Client struct {
	config    Config
	tempDir   string
	socket    string
	master    *exec.Cmd
	done      chan error
	closeOnce sync.Once
}

func New(config Config) *Client {
	return &Client{config: config}
}

func (c *Client) Start(ctx context.Context, timeout time.Duration) error {
	tempDir, err := os.MkdirTemp("", "localfwd-")
	if err != nil {
		return fmt.Errorf("create control socket directory: %w", err)
	}
	c.tempDir = tempDir
	c.socket = filepath.Join(tempDir, "ssh.sock")

	args := c.baseArgs()
	args = append(args,
		"-M", "-S", c.socket,
		"-o", "ControlMaster=yes",
		"-o", "ControlPersist=no",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "RemoteCommand=none",
		"-N", c.config.Destination,
	)
	c.master = exec.CommandContext(ctx, c.config.Path, args...)
	c.master.Stdin = c.config.Stdin
	c.master.Stderr = c.config.Stderr
	c.done = make(chan error, 1)
	if err := c.master.Start(); err != nil {
		c.cleanup()
		return fmt.Errorf("start ssh: %w", err)
	}
	go func() { c.done <- c.master.Wait() }()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-c.done:
			c.cleanup()
			if err == nil {
				return errors.New("ssh connection closed during startup")
			}
			return fmt.Errorf("ssh connection failed: %w", err)
		case <-deadline.C:
			_ = c.Close()
			return fmt.Errorf("SSH connection did not become ready within %s", timeout)
		case <-ticker.C:
			if c.check(ctx) == nil {
				return nil
			}
		case <-ctx.Done():
			_ = c.Close()
			return ctx.Err()
		}
	}
}

func (c *Client) Discover(ctx context.Context) ([]discovery.Listener, error) {
	args := []string{"-S", c.socket, "-o", "RemoteCommand=none", "-T", c.config.Destination, "sh", "-s"}
	cmd := exec.CommandContext(ctx, c.config.Path, args...)
	cmd.Stdin = strings.NewReader(discovery.Script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, commandError("discover remote ports", err, stderr.String())
	}
	listeners, err := discovery.Parse(stdout.String())
	if err != nil {
		return nil, err
	}
	return listeners, nil
}

func (c *Client) AddForward(ctx context.Context, bind string, localPort int, remoteHost string, remotePort int) error {
	spec := forwardSpec(bind, localPort, remoteHost, remotePort)
	return c.control(ctx, "forward", "-L", spec)
}

func (c *Client) RemoveForward(ctx context.Context, bind string, localPort int, remoteHost string, remotePort int) error {
	spec := forwardSpec(bind, localPort, remoteHost, remotePort)
	return c.control(ctx, "cancel", "-L", spec)
}

func (c *Client) Done() <-chan error { return c.done }

func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		if c.socket != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := c.control(ctx, "exit"); err != nil && c.master != nil && c.master.Process != nil {
				closeErr = c.master.Process.Kill()
			}
		}
		c.cleanup()
	})
	return closeErr
}

func (c *Client) baseArgs() []string {
	args := make([]string, 0, len(c.config.Options)*2)
	for _, option := range c.config.Options {
		args = append(args, "-o", option)
	}
	return args
}

func (c *Client) check(ctx context.Context) error {
	return c.control(ctx, "check")
}

func (c *Client) control(ctx context.Context, operation string, extra ...string) error {
	args := []string{"-S", c.socket, "-O", operation}
	args = append(args, extra...)
	args = append(args, c.config.Destination)
	cmd := exec.CommandContext(ctx, c.config.Path, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError("ssh control operation "+operation, err, stderr.String())
	}
	return nil
}

func (c *Client) cleanup() {
	if c.tempDir != "" {
		_ = os.RemoveAll(c.tempDir)
	}
}

func forwardSpec(bind string, localPort int, remoteHost string, remotePort int) string {
	bind = bracketIPv6(bind)
	remoteHost = bracketIPv6(remoteHost)
	return fmt.Sprintf("%s:%d:%s:%d", bind, localPort, remoteHost, remotePort)
}

func bracketIPv6(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func commandError(action string, err error, stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %s: %w", action, message, err)
}

// AvailablePort returns preferred when it can be bound, otherwise an ephemeral port.
func AvailablePort(bind string, preferred int) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(bind, fmt.Sprint(preferred)))
	if err == nil {
		_ = listener.Close()
		return preferred, nil
	}

	listener, err = net.Listen("tcp", net.JoinHostPort(bind, "0"))
	if err != nil {
		return 0, fmt.Errorf("find available local port on %s: %w", bind, err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
