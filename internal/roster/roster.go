// Package roster owns the optional workspace staffing file .spynel/roster.yaml:
// named staff (runner kind, launch arguments, daily cap, fallback) and the
// workflow-role to staff mapping. A missing file means legacy routing; a
// malformed file is an error the caller logs before falling back to legacy.
package roster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agent0ai/spynel/internal/fsx"
	"gopkg.in/yaml.v3"
)

const (
	FileName = "roster.yaml"
	// StateDirName mirrors config.StateDirectoryName for harnesses that only know the workspace root.
	StateDirName  = ".spynel"
	usageFileName = "roster-usage.json"
	// StaffPrefix marks a harness model string that names roster staff.
	StaffPrefix = "@"
	maxFallback = 8
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type Staff struct {
	Runner string   `yaml:"runner"`
	Args   []string `yaml:"args,omitempty"`
	// NotifyAt asks Joey once per day when the count reaches it; routing is unchanged.
	NotifyAt int `yaml:"notify_at,omitempty"`
	// DailyCap halts the staff for the day unless Joey answered "continue".
	DailyCap int    `yaml:"daily_cap,omitempty"`
	Fallback string `yaml:"fallback,omitempty"`
	Note     string `yaml:"note,omitempty"`
}

type Escalation struct {
	AfterAttempt int    `yaml:"after_attempt"`
	Staff        string `yaml:"staff"`
}

type Roster struct {
	Staff      map[string]Staff  `yaml:"staff"`
	Roles      map[string]string `yaml:"roles"`
	Escalation *Escalation       `yaml:"escalation,omitempty"`
}

// Path returns the roster file inside a workspace state directory.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Load reads and validates the roster. It returns (nil, nil) when no roster exists.
func Load(stateDir string) (*Roster, error) {
	data, err := os.ReadFile(Path(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Roster
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return &r, nil
}

func (r *Roster) validate() error {
	if len(r.Staff) == 0 {
		return errors.New("no staff defined")
	}
	for name, s := range r.Staff {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("staff name %q is invalid", name)
		}
		if !namePattern.MatchString(s.Runner) {
			return fmt.Errorf("staff %s: runner %q is invalid", name, s.Runner)
		}
		if s.DailyCap < 0 {
			return fmt.Errorf("staff %s: daily_cap cannot be negative", name)
		}
		if s.NotifyAt < 0 {
			return fmt.Errorf("staff %s: notify_at cannot be negative", name)
		}
		if s.NotifyAt > 0 && s.DailyCap > 0 && s.NotifyAt >= s.DailyCap {
			return fmt.Errorf("staff %s: notify_at must be below daily_cap", name)
		}
		if s.Fallback != "" {
			if _, ok := r.Staff[s.Fallback]; !ok {
				return fmt.Errorf("staff %s: fallback %q is not defined", name, s.Fallback)
			}
		}
		if s.DailyCap > 0 && s.Fallback == "" {
			return fmt.Errorf("staff %s: daily_cap needs a fallback", name)
		}
		for _, arg := range s.Args {
			if strings.ContainsAny(arg, "\n\r\x00") {
				return fmt.Errorf("staff %s: args must be single-line", name)
			}
		}
	}
	for role, name := range r.Roles {
		if _, ok := r.Staff[name]; !ok {
			return fmt.Errorf("role %s: staff %q is not defined", role, name)
		}
	}
	if r.Escalation != nil {
		if r.Escalation.AfterAttempt < 1 {
			return errors.New("escalation.after_attempt must be at least 1")
		}
		if _, ok := r.Staff[r.Escalation.Staff]; !ok {
			return fmt.Errorf("escalation: staff %q is not defined", r.Escalation.Staff)
		}
	}
	return nil
}

// Has reports whether name is defined staff.
func (r *Roster) Has(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.Staff[name]
	return ok
}

// ForRole returns the staff name mapped to a workflow role, or "".
func (r *Roster) ForRole(role string) string {
	if r == nil {
		return ""
	}
	return r.Roles[role]
}

// Model renders the harness model string for staff: the bare runner when no
// launch arguments are needed, otherwise the "@name" form a roster-aware
// harness expands.
func (r *Roster) Model(name string) string {
	s := r.Staff[name]
	if len(s.Args) == 0 {
		return s.Runner
	}
	return StaffPrefix + name
}

// Expand resolves a harness model string to a runner kind and launch arguments.
// Strings without the staff prefix pass through unchanged.
func Expand(stateDir, model string) (string, []string, error) {
	if !strings.HasPrefix(model, StaffPrefix) {
		return model, nil, nil
	}
	name := strings.TrimPrefix(model, StaffPrefix)
	r, err := Load(stateDir)
	if err != nil {
		return "", nil, err
	}
	if !r.Has(name) {
		return "", nil, fmt.Errorf("roster staff %q is not defined", name)
	}
	s := r.Staff[name]
	return s.Runner, append([]string(nil), s.Args...), nil
}

type usage struct {
	Date   string            `json:"date"`
	Counts map[string]int    `json:"counts"`
	Seen   map[string]string `json:"seen"`
	// Notified lists staff whose notify_at ask was already filed today.
	Notified map[string]bool `json:"notified,omitempty"`
}

// decisions is Joey's answer to a notify_at ask, written by the agent that
// resumes the ask task: "continue" lifts daily_cap for that staff today,
// "stop" halts the staff now. Anything else, or another date, means no answer.
type decisions struct {
	Date  string            `json:"date"`
	Staff map[string]string `json:"staff"`
}

const (
	decisionsFileName = "roster-decisions.json"
	DecisionContinue  = "continue"
	DecisionStop      = "stop"
)

func loadDecisions(stateDir, today string) map[string]string {
	var d decisions
	data, err := os.ReadFile(filepath.Join(stateDir, "runtime", decisionsFileName))
	if err != nil || json.Unmarshal(data, &d) != nil || d.Date != today {
		return nil
	}
	return d.Staff
}

var usageMu sync.Mutex

// Assign applies daily caps. A session key keeps the staff it was first given
// today, so retries and recoveries of one session are counted once. Capped
// staff fall through their fallback chain; the last link is used even when it
// is itself capped, so work is never left without a runner. Reaching notify_at
// files one ask for Joey and changes no routing; his "continue" lifts daily_cap
// for the day and his "stop" halts the staff at once.
func (r *Roster) Assign(stateDir, name, sessionKey string, now time.Time) (string, error) {
	if !r.Has(name) {
		return "", fmt.Errorf("roster staff %q is not defined", name)
	}
	usageMu.Lock()
	defer usageMu.Unlock()

	path := filepath.Join(stateDir, "runtime", usageFileName)
	today := now.Local().Format("2006-01-02")
	u := usage{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &u)
	}
	if u.Date != today || u.Counts == nil || u.Seen == nil {
		u = usage{Date: today, Counts: map[string]int{}, Seen: map[string]string{}}
	}
	if prior, ok := u.Seen[sessionKey]; ok && sessionKey != "" && r.Has(prior) {
		return prior, nil
	}

	decided := loadDecisions(stateDir, today)
	chosen := name
	for hop := 0; hop < maxFallback; hop++ {
		s := r.Staff[chosen]
		if s.Fallback == "" {
			break
		}
		halted := s.DailyCap > 0 && u.Counts[chosen] >= s.DailyCap && decided[chosen] != DecisionContinue
		if !halted && decided[chosen] != DecisionStop {
			break
		}
		chosen = s.Fallback
	}
	u.Counts[chosen]++
	if s := r.Staff[chosen]; s.NotifyAt > 0 && u.Counts[chosen] >= s.NotifyAt && !u.Notified[chosen] && decided[chosen] == "" {
		if err := writeCapAsk(stateDir, chosen, s, u.Counts[chosen], now); err == nil {
			if u.Notified == nil {
				u.Notified = map[string]bool{}
			}
			u.Notified[chosen] = true
		}
	}
	if sessionKey != "" {
		u.Seen[sessionKey] = chosen
	}
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return chosen, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return chosen, err
	}
	return chosen, fsx.AtomicWriteFile(path, data, 0o600)
}

// writeCapAsk parks one task on Joey (joey_ask choice). The workspace pump puts
// it on Helm and returns his answer into the same document, which resumes and
// records the decision file Assign reads.
func writeCapAsk(stateDir, name string, s Staff, count int, now time.Time) error {
	day := now.Local().Format("2006-01-02")
	id := "tasks-" + now.Local().Format("20060102") + "-roster-cap-" + name
	stamp := now.UTC().Format(time.RFC3339)
	halt := "There is no higher limit set, so it keeps going until you say stop."
	stopEffect := name + " stops for the rest of " + day + " and its work goes to " + s.Fallback + "."
	if s.DailyCap > 0 {
		halt = fmt.Sprintf("If you do not answer, %s keeps working until %d sessions today, then stops and its work goes to %s.", name, s.DailyCap, s.Fallback)
	}
	if s.Fallback == "" {
		stopEffect = name + " has no fallback staff, so choosing this changes nothing until one is set in roster.yaml."
	}
	text := fmt.Sprintf(`---
id: %[1]s
title: "Roster: %[2]s reached %[3]d sessions on %[4]s"
status: waiting
created_at: "%[5]s"
updated_at: "%[5]s"
review_required: false
attempt: 0
notify:
  enabled: false
waiting_for: "Joey's call on %[2]s usage for %[4]s"
joey_ask:
  kind: choice
  prompt: "SpyJo staff '%[2]s' (%[6]s) has used %[3]d sessions today, the point where you asked to be told. Let it keep going today, or stop it for today?"
  context: "Nothing is paused: %[2]s is still taking work while this card is open. %[7]s This card is only so you know what is going on and can overrule it."
  how_to: "Tap one option."
  options:
    - label: "Keep going today"
      value: "continue"
      effect: "%[2]s keeps working for the rest of %[4]s with no halt at the higher limit."
    - label: "Stop it for today"
      value: "stop"
      effect: "%[8]s"
---

# Roster: %[2]s reached %[3]d sessions on %[4]s

## Objective

Record Joey's answer so the roster applies it. Filed automatically by SpyJo when roster staff %[2]s reached its notify_at count.

## Scope

- Joey's answer is in the newest JOEY ANSWERED entry under Progress: the tapped value is continue or stop.
- If today's local date is still %[4]s: write the file runtime/roster-decisions.json inside this workspace's .spynel directory with exactly {"date": "%[4]s", "staff": {"%[2]s": "<continue or stop>"}}. If that file already holds date %[4]s, keep its other staff entries and set only %[2]s.
- If the date has passed, change nothing; the count already reset.
- Do nothing else. Do not edit roster.yaml.

## Acceptance criteria

- The decision file holds Joey's value for %[2]s on %[4]s, or Progress says the date had passed.

## Context

- Joey, 2026-09-17: "Instead have it escalate to me when that cap happen so I know what's going on.. but y'all keep working. And then a higher cap to halt if it's hit and I haven't responded."

## Progress

- %[5]s - Filed by the SpyJo roster: %[2]s at %[3]d sessions (notify_at %[9]d, daily_cap %[10]d).
`, id, name, count, day, stamp, s.Runner, halt, stopEffect, s.NotifyAt, s.DailyCap)
	dir := filepath.Join(stateDir, "tasks", "waiting")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fsx.AtomicWriteFile(filepath.Join(dir, id+".md"), []byte(text), 0o600)
}
