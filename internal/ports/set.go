package ports

import (
	"fmt"
	"strconv"
	"strings"
)

// Set represents a user-provided collection of ports and inclusive ranges.
type Set map[int]struct{}

func ParseSet(value string) (Set, error) {
	result := make(Set)
	if strings.TrimSpace(value) == "" {
		return result, nil
	}

	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("empty port entry")
		}
		bounds := strings.Split(item, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("%q is not a port or range", item)
		}
		start, err := parsePort(bounds[0])
		if err != nil {
			return nil, err
		}
		end := start
		if len(bounds) == 2 {
			end, err = parsePort(bounds[1])
			if err != nil {
				return nil, err
			}
			if end < start {
				return nil, fmt.Errorf("range %q is reversed", item)
			}
		}
		for port := start; port <= end; port++ {
			result[port] = struct{}{}
		}
	}
	return result, nil
}

func (s Set) Contains(port int) bool {
	_, ok := s[port]
	return ok
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%q is not a valid port", value)
	}
	return port, nil
}
