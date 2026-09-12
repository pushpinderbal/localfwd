package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var strictVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+].*)?$`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-release-version <version>")
		os.Exit(2)
	}

	requested := normalize(os.Args[1])
	if !strictVersion.MatchString(os.Args[1]) || !semver.IsValid(requested) {
		fmt.Fprintf(os.Stderr, "%q is not a valid semantic version\n", os.Args[1])
		os.Exit(1)
	}

	highest := ""
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		existing := normalize(strings.TrimSpace(scanner.Text()))
		if existing == "v" {
			continue
		}
		if !semver.IsValid(existing) {
			fmt.Fprintf(os.Stderr, "ignoring non-semver release %q\n", scanner.Text())
			continue
		}
		if highest == "" || semver.Compare(existing, highest) > 0 {
			highest = existing
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read existing versions: %v\n", err)
		os.Exit(1)
	}

	if highest != "" && semver.Compare(requested, highest) <= 0 {
		fmt.Fprintf(os.Stderr, "requested version %s must be greater than existing version %s\n", requested, highest)
		os.Exit(1)
	}

	if highest == "" {
		fmt.Printf("validated first release %s\n", requested)
		return
	}
	fmt.Printf("validated %s > %s\n", requested, highest)
}

func normalize(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
