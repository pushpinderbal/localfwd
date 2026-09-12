package discovery

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const Script = `
set -eu
if command -v ss >/dev/null 2>&1; then
  if ss -H -ltnp 2>/dev/null; then exit 0; fi
  if ss -H -ltn 2>/dev/null; then exit 0; fi
  if ss -ltn 2>/dev/null; then exit 0; fi
fi
if command -v lsof >/dev/null 2>&1; then
  lsof -nP -iTCP -sTCP:LISTEN
  exit 0
fi
if command -v netstat >/dev/null 2>&1; then
  if netstat -lntp 2>/dev/null; then exit 0; fi
  if netstat -lnt 2>/dev/null; then exit 0; fi
fi
echo "localfwd: the remote host needs ss, netstat, or lsof" >&2
exit 127
`

type Listener struct {
	Port    int
	Address string
	Process string
}

var (
	processSS   = regexp.MustCompile(`users:\(\(\"([^\"]+)\"`)
	processLsof = regexp.MustCompile(`^([^[:space:]]+)`)
)

// Parse accepts output from ss, netstat, or lsof and returns unique TCP listeners.
func Parse(output string) ([]Listener, error) {
	byPort := make(map[int]Listener)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || isHeader(line) {
			continue
		}
		listener, ok := parseLine(line)
		if !ok {
			continue
		}
		if current, exists := byPort[listener.Port]; !exists || current.Process == "" {
			byPort[listener.Port] = listener
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read listener output: %w", err)
	}

	listeners := make([]Listener, 0, len(byPort))
	for _, listener := range byPort {
		listeners = append(listeners, listener)
	}
	sort.Slice(listeners, func(i, j int) bool { return listeners[i].Port < listeners[j].Port })
	return listeners, nil
}

func parseLine(line string) (Listener, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Listener{}, false
	}

	// ss and netstat place the local endpoint before a peer endpoint. lsof places it
	// near the end followed by "(LISTEN)". Select the first plausible endpoint.
	for i, field := range fields {
		address, port, ok := parseEndpoint(strings.TrimSuffix(field, "->"))
		if !ok {
			continue
		}
		if i+1 < len(fields) && !looksLikeSocketContext(fields, i) {
			continue
		}

		process := ""
		if match := processSS.FindStringSubmatch(line); len(match) == 2 {
			process = match[1]
		} else if strings.Contains(line, "(LISTEN)") {
			if match := processLsof.FindStringSubmatch(line); len(match) == 2 {
				process = match[1]
			}
		} else if len(fields) > 6 && strings.Contains(strings.ToLower(fields[0]), "tcp") {
			process = strings.TrimSuffix(fields[len(fields)-1], "/")
		}
		return Listener{Port: port, Address: address, Process: process}, true
	}
	return Listener{}, false
}

func looksLikeSocketContext(fields []string, index int) bool {
	line := strings.Join(fields, " ")
	if strings.Contains(line, "(LISTEN)") || strings.HasPrefix(strings.ToLower(fields[0]), "tcp") {
		return true
	}
	// ss starts with LISTEN and its local address normally occupies field 3.
	return strings.EqualFold(fields[0], "LISTEN") && index >= 3
}

func parseEndpoint(value string) (string, int, bool) {
	value = strings.TrimSpace(value)
	colon := strings.LastIndex(value, ":")
	if colon < 0 || colon == len(value)-1 {
		return "", 0, false
	}
	portText := strings.TrimRight(value[colon+1:], ",")
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	address := strings.Trim(value[:colon], "[]")
	if address == "" {
		address = "*"
	}
	return address, port, true
}

func isHeader(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "proto ") || strings.HasPrefix(lower, "command ") || strings.HasPrefix(lower, "active internet")
}
