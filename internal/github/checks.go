package github

import (
	"sort"
	"strings"
)

// CheckState is the normalized outcome of a check, workflow, or rollup.
type CheckState int

// States are ordered by how much attention they deserve; aggregates take the
// highest state of their members.
const (
	CheckNone CheckState = iota
	CheckSkipped
	CheckSuccess
	CheckCancelled
	CheckPending
	CheckFailure
)

// Check is a single check run or commit status.
type Check struct {
	Name  string
	URL   string
	State CheckState
}

// Workflow groups the checks produced by one Actions workflow (or one external
// status context).
type Workflow struct {
	Name   string
	URL    string
	State  CheckState
	Checks []Check
}

// Rollup is the CI summary for a PR's head commit.
type Rollup struct {
	State     CheckState
	Workflows []Workflow
}

// Counts tallies the individual checks by state.
func (r Rollup) Counts() map[CheckState]int {
	counts := map[CheckState]int{}
	for _, w := range r.Workflows {
		for _, c := range w.Checks {
			counts[c.State]++
		}
	}
	return counts
}

func checkRunState(status, conclusion string) CheckState {
	if status != "COMPLETED" {
		return CheckPending
	}
	switch conclusion {
	case "SUCCESS":
		return CheckSuccess
	case "FAILURE", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED":
		return CheckFailure
	case "CANCELLED":
		return CheckCancelled
	default: // SKIPPED, NEUTRAL, STALE
		return CheckSkipped
	}
}

func statusContextState(state string) CheckState {
	switch state {
	case "SUCCESS":
		return CheckSuccess
	case "FAILURE", "ERROR":
		return CheckFailure
	default: // PENDING, EXPECTED
		return CheckPending
	}
}

// buildRollup groups checks by workflow. A workflow that ran more than once
// on the same commit (a re-run, or a push and a pull_request trigger) would
// otherwise list each job twice, so only the latest run of each job is kept.
func buildRollup(nodes []contextNode) Rollup {
	type job struct {
		check Check
		node  contextNode
	}
	type group struct {
		name   string
		url    string
		latest contextNode
		jobs   map[string]job
		order  []string
	}
	groups := map[string]*group{}
	var groupOrder []string

	for _, n := range nodes {
		var name, runURL string
		var c Check
		switch n.Typename {
		case "CheckRun":
			name = "checks"
			if n.CheckSuite != nil {
				switch {
				case n.CheckSuite.WorkflowRun != nil:
					name = n.CheckSuite.WorkflowRun.Workflow.Name
					runURL = n.CheckSuite.WorkflowRun.URL
				case n.CheckSuite.App != nil:
					name = n.CheckSuite.App.Name
				}
			}
			c = Check{Name: n.Name, URL: n.DetailsURL, State: checkRunState(n.Status, n.Conclusion)}
		case "StatusContext":
			name, _, _ = strings.Cut(n.Context, "/")
			c = Check{Name: n.Context, URL: n.TargetURL, State: statusContextState(n.State)}
		default:
			continue
		}

		g, ok := groups[name]
		if !ok {
			g = &group{name: name, jobs: map[string]job{}}
			groups[name] = g
			groupOrder = append(groupOrder, name)
		}
		if prev, ok := g.jobs[c.Name]; ok {
			if n.StartedAt.Before(prev.node.StartedAt) {
				continue
			}
		} else {
			g.order = append(g.order, c.Name)
		}
		g.jobs[c.Name] = job{check: c, node: n}
		if runURL != "" && !n.StartedAt.Before(g.latest.StartedAt) {
			g.latest = n
			g.url = runURL
		}
	}

	var r Rollup
	for _, name := range groupOrder {
		g := groups[name]
		w := Workflow{Name: g.name, URL: g.url}
		for _, jobName := range g.order {
			c := g.jobs[jobName].check
			w.Checks = append(w.Checks, c)
			w.State = max(w.State, c.State)
		}
		if w.URL == "" && len(w.Checks) == 1 {
			w.URL = w.Checks[0].URL
		}
		sortChecks(w.Checks)
		r.Workflows = append(r.Workflows, w)
		r.State = max(r.State, w.State)
	}
	// Failing and running workflows first, since those are what you are
	// watching for; ties keep GitHub's order.
	sort.SliceStable(r.Workflows, func(i, j int) bool {
		return r.Workflows[i].State > r.Workflows[j].State
	})
	return r
}

func sortChecks(checks []Check) {
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].State > checks[j].State })
}
