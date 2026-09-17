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
	Runner   string   `yaml:"runner"`
	Args     []string `yaml:"args,omitempty"`
	DailyCap int      `yaml:"daily_cap,omitempty"`
	Fallback string   `yaml:"fallback,omitempty"`
	Note     string   `yaml:"note,omitempty"`
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
}

var usageMu sync.Mutex

// Assign applies daily caps. A session key keeps the staff it was first given
// today, so retries and recoveries of one session are counted once. Capped
// staff fall through their fallback chain; the last link is used even when it
// is itself capped, so work is never left without a runner.
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

	chosen := name
	for hop := 0; hop < maxFallback; hop++ {
		s := r.Staff[chosen]
		if s.DailyCap == 0 || u.Counts[chosen] < s.DailyCap || s.Fallback == "" {
			break
		}
		chosen = s.Fallback
	}
	u.Counts[chosen]++
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
