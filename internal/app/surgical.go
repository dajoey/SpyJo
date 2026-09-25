package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SurgicallyUpdateFrontMatterField updates or inserts a single top-level field in
// markdown YAML front matter while preserving 100% of all other bytes outside
// the modified line, including comments, multi-line values, indentation, and body.
func SurgicallyUpdateFrontMatterField(content string, field string, formattedValue string) (string, error) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", errors.New("document does not start with YAML front matter")
	}
	delim := "\n"
	if strings.HasPrefix(content, "---\r\n") {
		delim = "\r\n"
	}

	start := len("---" + delim)
	endMarker := delim + "---" + delim
	endIdx := strings.Index(content[start:], endMarker)
	if endIdx < 0 {
		// Try without trailing newline
		endMarkerAlt := delim + "---"
		if strings.HasSuffix(content[start:], endMarkerAlt) {
			endIdx = len(content[start:]) - len(endMarkerAlt)
			endMarker = endMarkerAlt
		} else {
			return "", errors.New("document front matter is not closed with '---'")
		}
	}

	frontMatter := content[start : start+endIdx]
	bodyAndEnd := content[start+endIdx:]

	// Look for top-level field in front matter: line starting with field:
	pattern := `(?m)^(` + regexp.QuoteMeta(field) + `:\s*).*$`
	re := regexp.MustCompile(pattern)

	newLine := field + ": " + formattedValue
	if re.MatchString(frontMatter) {
		updatedFrontMatter := re.ReplaceAllString(frontMatter, newLine)
		return content[:start] + updatedFrontMatter + bodyAndEnd, nil
	}

	// Field not present; insert before closing delimiter
	var updatedFrontMatter string
	if strings.HasSuffix(frontMatter, delim) {
		updatedFrontMatter = frontMatter + newLine + delim
	} else {
		updatedFrontMatter = frontMatter + delim + newLine + delim
	}
	return content[:start] + updatedFrontMatter + bodyAndEnd, nil
}

// SurgicallyDeleteFrontMatterField removes a single top-level field from front
// matter while leaving every other byte untouched.
func SurgicallyDeleteFrontMatterField(content string, field string) (string, error) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", errors.New("document does not start with YAML front matter")
	}
	delim := "\n"
	if strings.HasPrefix(content, "---\r\n") {
		delim = "\r\n"
	}

	start := len("---" + delim)
	endMarker := delim + "---" + delim
	endIdx := strings.Index(content[start:], endMarker)
	if endIdx < 0 {
		endMarkerAlt := delim + "---"
		if strings.HasSuffix(content[start:], endMarkerAlt) {
			endIdx = len(content[start:]) - len(endMarkerAlt)
			endMarker = endMarkerAlt
		} else {
			return "", errors.New("document front matter is not closed with '---'")
		}
	}

	frontMatter := content[start : start+endIdx]
	bodyAndEnd := content[start+endIdx:]

	// Match the line with trailing newline
	pattern := `(?m)^` + regexp.QuoteMeta(field) + `:\s*.*(?:\r?\n|$)`
	re := regexp.MustCompile(pattern)

	updatedFrontMatter := re.ReplaceAllString(frontMatter, "")
	return content[:start] + updatedFrontMatter + bodyAndEnd, nil
}

// SurgicallyUpdateYAMLField navigates a hierarchical YAML structure by path
// (e.g. ["staff", "cursor", "runner"]) and updates the target key's value in place,
// preserving comments, multi-line blocks, and indentation everywhere else.
func SurgicallyUpdateYAMLField(yamlContent string, path []string, formattedValue string) (string, error) {
	if len(path) == 0 {
		return yamlContent, errors.New("empty path")
	}

	lines := strings.Split(yamlContent, "\n")
	pathIdx := 0
	targetIndents := make([]int, len(path))

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count leading spaces
		indent := len(line) - len(strings.TrimLeft(line, " "))
		keyPart := trimmed
		if idx := strings.Index(trimmed, ":"); idx >= 0 {
			keyPart = strings.TrimSpace(trimmed[:idx])
		}

		if pathIdx < len(path) && keyPart == path[pathIdx] {
			// Verify indentation matches hierarchy
			if pathIdx > 0 && indent <= targetIndents[pathIdx-1] {
				// Dedented back before matching path; reset
				pathIdx = 0
			}
			targetIndents[pathIdx] = indent
			if pathIdx == len(path)-1 {
				// Target key found! Replace value while preserving leading indent and any inline comments
				leading := line[:indent]
				comment := ""
				if colIdx := strings.Index(line, ":"); colIdx >= 0 {
					remainder := line[colIdx+1:]
					if hashIdx := strings.Index(remainder, "#"); hashIdx >= 0 {
						comment = " " + strings.TrimSpace(remainder[hashIdx:])
					}
				}
				lines[i] = leading + keyPart + ": " + formattedValue + comment
				return strings.Join(lines, "\n"), nil
			}
			pathIdx++
		} else if pathIdx > 0 && indent <= targetIndents[pathIdx-1] {
			pathIdx = 0
		}
	}

	return "", fmt.Errorf("path %v not found in YAML content", path)
}

// SurgicallyAppendProgress appends a timestamped note to ## Progress in the markdown body.
func SurgicallyAppendProgress(content string, note string, now time.Time) string {
	stamp := now.UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("- %s %s", stamp, note)
	if strings.Contains(content, "## Progress") {
		clean := strings.TrimRight(content, "\r\n")
		return clean + "\n" + entry + "\n"
	}
	clean := strings.TrimRight(content, "\r\n")
	return clean + "\n\n## Progress\n\n" + entry + "\n"
}
