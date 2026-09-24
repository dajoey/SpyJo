package orchestrator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	notificationTitleRunes    = 120
	notificationOutcomeRunes  = 280
	notificationEvidenceRunes = 2000
)

// completionSummary is optional and deliberately bounded. Invalid presentation
// metadata is ignored so reviewed transitions cannot be held hostage by it;
// direct completion validates the same structure as required evidence.
type completionSummary struct {
	Verdict     string
	Outcome     string
	Evidence    string
	Uncertainty string
	ReviewedAt  time.Time
	CompletedAt time.Time
	ReworkCount int
	HasRework   bool
}

func parseCompletionSummary(document Document) (completionSummary, bool) {
	raw, ok := document.FrontMatter["completion_summary"].(map[string]any)
	if !ok {
		return completionSummary{}, false
	}
	for key := range raw {
		switch key {
		case "verdict", "outcome", "evidence", "uncertainty", "reviewed_at", "completed_at", "rework_count":
		default:
			return completionSummary{}, false
		}
	}
	verdict, verdictOK := requiredBoundedLine(raw, "verdict", 32)
	outcome, outcomeOK := requiredBoundedLine(raw, "outcome", notificationOutcomeRunes)
	evidence, evidenceOK := optionalBoundedLine(raw, "evidence", notificationEvidenceRunes)
	uncertainty, uncertaintyOK := optionalBoundedLine(raw, "uncertainty", notificationEvidenceRunes)
	summary := completionSummary{Verdict: verdict, Outcome: outcome, Evidence: evidence, Uncertainty: uncertainty}
	if !verdictOK || !outcomeOK || !evidenceOK || !uncertaintyOK || !validNotificationVerdict(summary.Verdict) {
		return completionSummary{}, false
	}
	if value, exists := raw["reviewed_at"]; exists {
		switch typed := value.(type) {
		case string:
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed))
			if err != nil {
				return completionSummary{}, false
			}
			summary.ReviewedAt = parsed
		case time.Time:
			summary.ReviewedAt = typed
		default:
			return completionSummary{}, false
		}
	}
	if value, exists := raw["completed_at"]; exists {
		switch typed := value.(type) {
		case string:
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed))
			if err != nil {
				return completionSummary{}, false
			}
			summary.CompletedAt = parsed
		case time.Time:
			summary.CompletedAt = typed
		default:
			return completionSummary{}, false
		}
	}
	if (summary.Verdict == "accepted" || summary.Verdict == "rejected") && summary.ReviewedAt.IsZero() {
		return completionSummary{}, false
	}
	if summary.Verdict == "completed" && summary.CompletedAt.IsZero() {
		return completionSummary{}, false
	}
	if value, exists := raw["rework_count"]; exists {
		count, ok := exactNonnegativeInt(value)
		if !ok || count > 100000 {
			return completionSummary{}, false
		}
		summary.ReworkCount = count
		summary.HasRework = true
	}
	return summary, true
}

func validNotificationVerdict(value string) bool {
	switch value {
	case "accepted", "completed", "rejected", "waiting", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func boundedLine(value any, limit int) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = cleanNotificationLine(text)
	if text == "" || utf8.RuneCountInString(text) > limit || containsAbsolutePath(text) {
		return "", false
	}
	return text, true
}

func requiredBoundedLine(raw map[string]any, key string, limit int) (string, bool) {
	value, exists := raw[key]
	if !exists {
		return "", false
	}
	return boundedLine(value, limit)
}

func optionalBoundedLine(raw map[string]any, key string, limit int) (string, bool) {
	value, exists := raw[key]
	if !exists {
		return "", true
	}
	return boundedLine(value, limit)
}

func exactNonnegativeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0
	case int64:
		return int(typed), typed >= 0
	case float64:
		integer := int(typed)
		return integer, typed >= 0 && typed == float64(integer)
	default:
		return 0, false
	}
}

func truncateLine(value string, limit int) string {
	value = cleanNotificationLine(value)
	if containsAbsolutePath(value) {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func containsAbsolutePath(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"/home/", "/etc/", "/var/", "/opt/", "/root/", "/usr/", "/tmp/", "c:\\", "d:\\", "\\\\"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

func cleanNotificationLine(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

func validateDirectCompletionEvidence(document Document) error {
	summary, ok := parseCompletionSummary(document)
	if !ok || summary.Verdict != "completed" {
		// parseCompletionSummary only reports valid/invalid. Name the exact failing
		// rule: a generic rejection makes the executor guess, and every wrong guess
		// costs another full agent turn (observed 2026-09-14..16: ~60 rejections,
		// one task bouncing 20+ times on the same summary).
		return diagnoseCompletionSummary(document)
	}
	if summary.Evidence == "" {
		return missingCompletionContent("completion_summary.evidence must record verification and the inspected boundary")
	}
	if summary.Uncertainty == "" {
		return missingCompletionContent("completion_summary.uncertainty must record remaining uncertainty")
	}
	updated, ok := timestampField(document, "updated_at")
	if !ok {
		return errors.New("updated_at must be an RFC 3339 UTC timestamp")
	}
	if !summary.CompletedAt.Equal(updated) {
		return fmt.Errorf("completion_summary.completed_at %s must exactly match updated_at %s; set both to the same UTC time in the final front-matter edit", summary.CompletedAt.Format(time.RFC3339), updated.Format(time.RFC3339))
	}
	_, completedOffset := summary.CompletedAt.Zone()
	_, updatedOffset := updated.Zone()
	if completedOffset != 0 || updatedOffset != 0 {
		return errors.New("completion_summary.completed_at and updated_at must be UTC")
	}
	return nil
}

// diagnoseCompletionSummary walks the parseCompletionSummary rules in order and
// returns the first one a direct completion summary breaks.
func diagnoseCompletionSummary(document Document) error {
	value, exists := document.FrontMatter["completion_summary"]
	if !exists {
		if strings.Contains(document.Body, "completion_summary:") {
			return errors.New("completion_summary is in the Markdown body; it must be a mapping inside the YAML front matter")
		}
		return missingCompletionContent("completion_summary is missing from the YAML front matter")
	}
	raw, isMap := value.(map[string]any)
	if !isMap || raw == nil {
		return missingCompletionContent("completion_summary must be a YAML mapping of verdict/outcome/evidence/uncertainty/completed_at, found %T", value)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "verdict", "outcome", "evidence", "uncertainty", "reviewed_at", "completed_at", "rework_count":
		default:
			return fmt.Errorf("completion_summary has unsupported key %q; allowed keys are verdict, outcome, evidence, uncertainty, completed_at", key)
		}
	}
	checks := []struct {
		key      string
		limit    int
		required bool
	}{
		{"verdict", 32, true},
		{"outcome", notificationOutcomeRunes, true},
		{"evidence", notificationEvidenceRunes, false},
		{"uncertainty", notificationEvidenceRunes, false},
	}
	for _, check := range checks {
		if err := diagnoseBoundedLine(raw, check.key, check.limit, check.required); err != nil {
			return err
		}
	}
	if verdict, _ := raw["verdict"].(string); cleanNotificationLine(verdict) != "completed" {
		return fmt.Errorf("completion_summary.verdict must be \"completed\" for a direct done, found %q", cleanNotificationLine(verdict))
	}
	for _, key := range []string{"completed_at", "reviewed_at"} {
		value, exists := raw[key]
		if !exists {
			if key == "completed_at" {
				return errors.New("completion_summary.completed_at is required and must equal updated_at")
			}
			continue
		}
		switch typed := value.(type) {
		case time.Time:
		case string:
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err != nil {
				return fmt.Errorf("completion_summary.%s %q is not an RFC 3339 timestamp like 2026-01-02T15:04:05Z", key, typed)
			}
		default:
			return fmt.Errorf("completion_summary.%s must be an RFC 3339 timestamp, found %T", key, value)
		}
	}
	if value, exists := raw["rework_count"]; exists {
		if count, ok := exactNonnegativeInt(value); !ok || count > 100000 {
			return errors.New("completion_summary.rework_count is code-managed; remove it")
		}
	}
	return errors.New("a valid completion_summary with verdict completed is required")
}

func diagnoseBoundedLine(raw map[string]any, key string, limit int, required bool) error {
	value, exists := raw[key]
	if !exists {
		if required {
			return missingCompletionContent("completion_summary.%s is required", key)
		}
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("completion_summary.%s must be a quoted string, found %T", key, value)
	}
	text = cleanNotificationLine(text)
	if text == "" {
		return missingCompletionContent("completion_summary.%s must not be empty", key)
	}
	if count := utf8.RuneCountInString(text); count > limit {
		return fmt.Errorf("completion_summary.%s is %d characters; the limit is %d", key, count, limit)
	}
	if containsAbsolutePath(text) {
		return fmt.Errorf("completion_summary.%s contains an absolute host path (/home/, /etc/, /var/, /opt/, /root/, /usr/, /tmp/, C:\\, D:\\ or a UNC share); name files by relative path instead", key)
	}
	return nil
}

// finalizeTaskCompletionSummary adds the objective rejection/rework metric
// after a review move. It never blocks or reverses the durable transition.
func (m *Manager) finalizeTaskCompletionSummary(path, status string) {
	document, err := ReadDocument(path)
	if err != nil {
		m.log("read task completion summary: " + err.Error())
		return
	}
	summary, ok := parseCompletionSummary(document)
	if !ok || (status == "done" && summary.Verdict != "accepted" && summary.Verdict != "completed") || (status == "todo" && summary.Verdict != "rejected") {
		return
	}
	reviews := numberValue(document.FrontMatter["review_attempt"])
	reworks := reviews
	if status == "done" && reworks > 0 {
		reworks--
	}
	raw := document.FrontMatter["completion_summary"].(map[string]any)
	raw["rework_count"] = max(0, reworks)
	if err := WriteDocument(path, document); err != nil {
		m.log("write task completion summary: " + err.Error())
	}
}

func timestampField(document Document, field string) (time.Time, bool) {
	value, ok := document.FrontMatter[field]
	if !ok {
		return time.Time{}, false
	}
	switch typed := value.(type) {
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	case time.Time:
		return typed, true
	default:
		return time.Time{}, false
	}
}
