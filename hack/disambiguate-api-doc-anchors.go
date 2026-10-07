package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
)

type apiVersionPair struct {
	previous string
	current  string
	suffix   string
}

var versionPairs = []apiVersionPair{
	{
		previous: "config.opendatahub.io/v1alpha1",
		current:  "config.opendatahub.io/v1alpha2",
		suffix:   "v1alpha2",
	},
	{
		previous: "datasciencecluster.opendatahub.io/v2",
		current:  "datasciencecluster.opendatahub.io/v3",
		suffix:   "v3",
	},
}

var (
	sectionHeading = regexp.MustCompile(`^## (.+?)\s*$`)
	typeHeading    = regexp.MustCompile(`^#{3,6} (.+?)\s*$`)
	anchorLink     = regexp.MustCompile(`\]\(#([^)]*)\)`)
)

type section struct {
	name       string
	start, end int
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: go run ./hack/disambiguate-api-doc-anchors.go <api-reference.md>")
	}

	path := os.Args[1]
	contents, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}

	lines := strings.Split(string(contents), "\n")
	sections := findSections(lines)
	for _, pair := range versionPairs {
		previous, ok := sections[pair.previous]
		if !ok {
			fatalf("section %q not found in %s", pair.previous, path)
		}
		current, ok := sections[pair.current]
		if !ok {
			fatalf("section %q not found in %s", pair.current, path)
		}

		previousHeadings := headingsIn(lines, previous)
		anchors := map[string]string{}
		for i := current.start + 1; i < current.end; i++ {
			match := typeHeading.FindStringSubmatch(lines[i])
			if len(match) != 2 || match[1] == "Resource Types" {
				continue
			}
			if _, shared := previousHeadings[match[1]]; !shared {
				continue
			}

			oldAnchor := slug(match[1])
			newAnchor := oldAnchor + "-" + pair.suffix
			anchors[oldAnchor] = newAnchor
			lines[i] = strings.TrimSuffix(lines[i], match[1]) + match[1] + " (" + pair.suffix + ")"
		}
		if len(anchors) == 0 {
			fatalf("no shared type headings found for %q and %q", pair.previous, pair.current)
		}

		for i := current.start + 1; i < current.end; i++ {
			lines[i] = anchorLink.ReplaceAllStringFunc(lines[i], func(link string) string {
				match := anchorLink.FindStringSubmatch(link)
				if target, exists := anchors[match[1]]; exists {
					return strings.Replace(link, "#"+match[1]+")", "#"+target+")", 1)
				}
				return link
			})
		}
	}

	updated := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func findSections(lines []string) map[string]section {
	sections := map[string]section{}
	var current *section
	for i, line := range lines {
		if match := sectionHeading.FindStringSubmatch(line); len(match) == 2 {
			if current != nil {
				current.end = i
				sections[current.name] = *current
			}
			current = &section{name: match[1], start: i}
		}
	}
	if current != nil {
		current.end = len(lines)
		sections[current.name] = *current
	}
	return sections
}

func headingsIn(lines []string, section section) map[string]struct{} {
	headings := map[string]struct{}{}
	for i := section.start + 1; i < section.end; i++ {
		if match := typeHeading.FindStringSubmatch(lines[i]); len(match) == 2 {
			headings[match[1]] = struct{}{}
		}
	}
	return headings
}

func slug(value string) string {
	var result strings.Builder
	separator := false
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(char)
			separator = false
		} else if unicode.IsSpace(char) || char == '-' {
			separator = true
		}
	}
	return result.String()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
