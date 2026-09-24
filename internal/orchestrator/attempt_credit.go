package orchestrator

import (
	"errors"
	"fmt"
)

// resumeCreditField marks a task whose last implementation attempt ended
// without failing: an agent turn parked it in waiting/, or its direct
// completion was rejected only on completion_summary format. The next claim
// continues that attempt instead of starting a new one, then deletes the mark.
// `attempt` drives the roster escalation ladder and the fleet attempt cap, so
// a park that spends it promotes work up the model ladder for nothing
// (2026-09-24: 30 of 40 ops tasks with attempt>1 climbed through a park or a
// restart, five through a genuine model failure).
//
// Provenance: Spynel writes the mark itself while reconciling the transition
// a turn made, and removes it from every other transition it reconciles, so a
// turn cannot credit its own requeue. A park made outside a turn (the attempt
// cap parks a task straight out of todo/ after its attempt already ended) is
// never marked. Resume paths — Spynel's scheduled wake and external scripts
// moving waiting/ back to todo/ — only have to keep the field; every claim
// removes it, so it is honoured at most once.
const resumeCreditField = "resume_credit"

// formatCreditAttemptField records the attempt that already used its one
// format-only repair, so a summary the same attempt cannot fix still spends it.
const formatCreditAttemptField = "format_credit_attempt"

// claimAttempt returns the attempt a claim records for field and whether a
// resume credit was honoured. It always removes the credit from frontMatter.
// Only the task implementation attempt is credited: goal planning and review
// counters feed neither the ladder nor the cap, and a goal may resume into a
// different phase than the one that parked, which would reuse an unrelated
// dispatch's session.
func claimAttempt(frontMatter map[string]any, field string) (int, bool) {
	current := numberValue(frontMatter[field])
	credit, _ := frontMatter[resumeCreditField].(bool)
	delete(frontMatter, resumeCreditField)
	if credit && field == phaseAttemptField(phaseTaskImplementation) && current >= 1 {
		return current, true
	}
	return current + 1, false
}

func resumeCreditNote(attempt int) string {
	return fmt.Sprintf("Spynel continued attempt %d instead of starting attempt %d: the previous turn parked this task or was rejected only on completion_summary format, and neither is a failed attempt; attempt not spent.", attempt, attempt+1)
}

// setResumeCredit writes or removes the mark under the provider-turn lock. It
// leaves updated_at alone: this is accounting, not a content edit, and a
// direct completion must keep completed_at equal to updated_at.
func setResumeCredit(path string, credit bool) error {
	lock, err := lockProviderTurn(path)
	if err != nil {
		return err
	}
	defer unlockProviderTurn(lock)
	document, err := ReadDocument(path)
	if err != nil {
		return err
	}
	_, present := document.FrontMatter[resumeCreditField]
	if credit {
		if value, _ := document.FrontMatter[resumeCreditField].(bool); value {
			return nil
		}
		document.FrontMatter[resumeCreditField] = true
	} else {
		if !present {
			return nil
		}
		delete(document.FrontMatter, resumeCreditField)
	}
	return WriteDocument(path, document)
}

// completionContentMissing marks a direct-completion rejection where the
// summary lacks required content (no summary, or an absent or empty verdict,
// outcome, evidence, or uncertainty). That is unrecorded work, not a
// misshaped record of finished work, and still spends the attempt.
type completionContentMissing struct{ message string }

func (e completionContentMissing) Error() string { return e.message }

func missingCompletionContent(format string, args ...any) error {
	return completionContentMissing{message: fmt.Sprintf(format, args...)}
}

// completionFormatOnly reports whether a validateDirectCompletionEvidence
// rejection concerns only the shape of a recorded summary.
func completionFormatOnly(err error) bool {
	var missing completionContentMissing
	return err != nil && !errors.As(err, &missing)
}
