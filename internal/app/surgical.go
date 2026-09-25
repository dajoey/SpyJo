package app

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// frontMatterBounds splits a document into the head (opening "---" + line
// delimiter), the front matter body (without delimiters), and the remainder
// starting at the closing delimiter.
func frontMatterBounds(content string) (head string, frontMatter string, rest string, delim string, err error) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", "", "", "", errors.New("document does not start with YAML front matter")
	}
	delim = "\n"
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
		} else {
			return "", "", "", "", errors.New("document front matter is not closed with '---'")
		}
	}
	return content[:start], content[start : start+endIdx], content[start+endIdx:], delim, nil
}

// fieldKey returns the mapping key of a top-level front-matter line, and
// whether the line is a top-level key at all (no leading whitespace).
func fieldKey(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", false
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", false
	}
	return line[:idx], true
}

// fieldBlockRange returns the half-open line range [i, j) covering a
// top-level field's own line plus its whole indented continuation block.
// A blank line is consumed only when the next non-blank line is still part of
// the block, so blank separators between top-level keys keep their bytes.
func fieldBlockRange(lines []string, field string) (int, int, bool) {
	for i, line := range lines {
		if key, ok := fieldKey(line); !ok || key != field {
			continue
		}
		j := i + 1
		for j < len(lines) {
			line := lines[j]
			if strings.TrimSpace(line) == "" {
				// Blank line: inside the block only if a later indented line follows.
				k := j + 1
				for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
					k++
				}
				if k < len(lines) && lines[k] != "" && (lines[k][0] == ' ' || lines[k][0] == '\t') {
					j = k + 1
					continue
				}
				break
			}
			if line != "" && (line[0] == ' ' || line[0] == '\t') {
				j++
				continue
			}
			break
		}
		return i, j, true
	}
	return 0, 0, false
}

// SurgicallyReplaceFrontMatterField replaces a top-level field — and its
// whole existing block when the current value spans indented continuation
// lines — with the replacement text, which must itself start with
// "field:". Every byte outside the replaced range is preserved exactly.
func SurgicallyReplaceFrontMatterField(content string, field string, replacement string) (string, error) {
	head, frontMatter, rest, delim, err := frontMatterBounds(content)
	if err != nil {
		return "", err
	}
	lines := strings.Split(frontMatter, delim)
	replacementLines := strings.Split(replacement, delim)
	if i, j, ok := fieldBlockRange(lines, field); ok {
		updated := make([]string, 0, len(lines)-(j-i)+len(replacementLines))
		updated = append(updated, lines[:i]...)
		updated = append(updated, replacementLines...)
		updated = append(updated, lines[j:]...)
		return head + strings.Join(updated, delim) + rest, nil
	}
	// Field not present; insert before the closing delimiter.
	var updated []string
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		updated = append(lines[:len(lines)-1], replacementLines...)
		updated = append(updated, lines[len(lines)-1])
	} else {
		updated = append(lines, replacementLines...)
	}
	return head + strings.Join(updated, delim) + rest, nil
}

// SurgicallyUpdateFrontMatterField updates or inserts a single top-level
// scalar field in markdown YAML front matter while preserving 100% of all
// other bytes, including comments, multi-line values, indentation, and body.
// When the field's current value is a multi-line block, the whole block is
// replaced: the old block's indented lines never survive as orphans (found
// live 2026-09-25 — a web Approve on a task carrying a prior rejected
// completion_summary produced duplicate mapping keys and an unreadable
// done/ document).
func SurgicallyUpdateFrontMatterField(content string, field string, formattedValue string) (string, error) {
	return SurgicallyReplaceFrontMatterField(content, field, field+": "+formattedValue)
}

// SurgicallyDeleteFrontMatterField removes a single top-level field — with
// its whole block when the value spans indented continuation lines — while
// leaving every other byte untouched.
func SurgicallyDeleteFrontMatterField(content string, field string) (string, error) {
	head, frontMatter, rest, delim, err := frontMatterBounds(content)
	if err != nil {
		return "", err
	}
	lines := strings.Split(frontMatter, delim)
	if i, j, ok := fieldBlockRange(lines, field); ok {
		updated := make([]string, 0, len(lines)-(j-i))
		updated = append(updated, lines[:i]...)
		updated = append(updated, lines[j:]...)
		return head + strings.Join(updated, delim) + rest, nil
	}
	return content, nil
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
