package sshctl

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("LOCALFWD_SSH_HELPER") == "1" {
		os.Exit(runSSHHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestForwardSpec(t *testing.T) {
	for _, test := range []struct {
		bind       string
		remoteHost string
		want       string
	}{
		{"127.0.0.1", "127.0.0.1", "127.0.0.1:3000:127.0.0.1:8000"},
		{"::1", "::1", "[::1]:3000:[::1]:8000"},
	} {
		if got := forwardSpec(test.bind, 3000, test.remoteHost, 8000); got != test.want {
			t.Errorf("forwardSpec(%q) = %q, want %q", test.bind, got, test.want)
		}
	}
}

func TestClientLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	marker := tempDir + "/connected"
	logPath := tempDir + "/operations"
	t.Setenv("LOCALFWD_SSH_HELPER", "1")
	t.Setenv("LOCALFWD_SSH_MARKER", marker)
	t.Setenv("LOCALFWD_SSH_LOG", logPath)

	client := New(Config{
		Path:        os.Args[0],
		Destination: "fake-devbox",
		Stdin:       strings.NewReader(""),
		Stderr:      io.Discard,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Start(ctx, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	listeners, err := client.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 1 || listeners[0].Port != 5173 || listeners[0].Process != "node" {
		t.Fatalf("unexpected discovery result: %#v", listeners)
	}
	if err := client.AddForward(ctx, "127.0.0.1", 5173, "127.0.0.1", 5173); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveForward(ctx, "127.0.0.1", 5173, "127.0.0.1", 5173); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	operations, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "forward 127.0.0.1:5173:127.0.0.1:5173\ncancel 127.0.0.1:5173:127.0.0.1:5173\n"
	if string(operations) != want {
		t.Fatalf("operations = %q, want %q", operations, want)
	}
}

func runSSHHelper(args []string) int {
	marker := os.Getenv("LOCALFWD_SSH_MARKER")
	if containsArg(args, "-M") {
		if err := os.WriteFile(marker, []byte("ready"), 0o600); err != nil {
			return 1
		}
		for {
			if _, err := os.Stat(marker); os.IsNotExist(err) {
				return 0
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	operation := valueAfter(args, "-O")
	switch operation {
	case "check":
		if _, err := os.Stat(marker); err != nil {
			return 1
		}
		return 0
	case "forward", "cancel":
		spec := valueAfter(args, "-L")
		file, err := os.OpenFile(os.Getenv("LOCALFWD_SSH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 1
		}
		_, err = fmt.Fprintf(file, "%s %s\n", operation, spec)
		_ = file.Close()
		if err != nil {
			return 1
		}
		return 0
	case "exit":
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return 1
		}
		return 0
	}

	if containsArg(args, "sh") {
		fmt.Println(`LISTEN 0 128 127.0.0.1:5173 0.0.0.0:* users:(("node",pid=42,fd=3))`)
		return 0
	}
	return 1
}

func containsArg(args []string, needle string) bool {
	for _, arg := range args {
		if arg == needle {
			return true
		}
	}
	return false
}

func valueAfter(args []string, needle string) string {
	for index, arg := range args {
		if arg == needle && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}
