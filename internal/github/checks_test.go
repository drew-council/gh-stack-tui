package github

import (
	"testing"
	"time"
)

func checkRun(workflow, runURL, name, status, conclusion string, started time.Time) contextNode {
	run := &workflowRun{URL: runURL}
	run.Workflow.Name = workflow
	return contextNode{
		Typename:   "CheckRun",
		Name:       name,
		Status:     status,
		Conclusion: conclusion,
		StartedAt:  started,
		CheckSuite: &checkSuite{WorkflowRun: run},
	}
}

func TestBuildRollupKeepsLatestRunOfEachJob(t *testing.T) {
	t0 := time.Date(2026, 9, 22, 17, 0, 0, 0, time.UTC)
	nodes := []contextNode{
		checkRun("ci", "run/1", "test", "COMPLETED", "SKIPPED", t0),
		checkRun("ci", "run/2", "test", "COMPLETED", "FAILURE", t0.Add(time.Minute)),
		checkRun("ci", "run/2", "lint", "IN_PROGRESS", "", t0.Add(time.Minute)),
		checkRun("Check Linked Issue", "run/3", "verify", "COMPLETED", "SUCCESS", t0),
		{
			Typename:  "StatusContext",
			Context:   "buildkite/pipeline",
			State:     "SUCCESS",
			TargetURL: "bk",
		},
	}
	r := buildRollup(nodes)

	if r.State != CheckFailure {
		t.Fatalf("rollup state = %v, want failure", r.State)
	}
	if len(r.Workflows) != 3 {
		t.Fatalf("got %d workflows, want 3", len(r.Workflows))
	}
	ci := r.Workflows[0]
	if ci.Name != "ci" || ci.URL != "run/2" {
		t.Fatalf("first workflow = %q (%s), want failing ci from run/2", ci.Name, ci.URL)
	}
	if len(ci.Checks) != 2 {
		t.Fatalf("ci has %d checks, want re-run deduplicated to 2", len(ci.Checks))
	}
	if ci.Checks[0].Name != "test" || ci.Checks[0].State != CheckFailure {
		t.Errorf("ci checks not ordered failure first: %+v", ci.Checks)
	}
	if bk := r.Workflows[2]; bk.Name != "buildkite" || bk.URL != "bk" {
		t.Errorf("status context grouped as %q (%s)", bk.Name, bk.URL)
	}
}

func TestCheckRunState(t *testing.T) {
	cases := []struct {
		status, conclusion string
		want               CheckState
	}{
		{"QUEUED", "", CheckPending},
		{"COMPLETED", "SUCCESS", CheckSuccess},
		{"COMPLETED", "TIMED_OUT", CheckFailure},
		{"COMPLETED", "CANCELLED", CheckCancelled},
		{"COMPLETED", "NEUTRAL", CheckSkipped},
	}
	for _, c := range cases {
		if got := checkRunState(c.status, c.conclusion); got != c.want {
			t.Errorf("checkRunState(%s, %s) = %v, want %v", c.status, c.conclusion, got, c.want)
		}
	}
}

func TestReviewStatus(t *testing.T) {
	cases := []struct {
		pr   PR
		want ReviewStatus
	}{
		{PR{State: "OPEN"}, ReviewReady},
		{PR{State: "OPEN", ReviewDecision: "REVIEW_REQUIRED"}, ReviewReady},
		{PR{State: "OPEN", ReviewDecision: "APPROVED"}, ReviewApproved},
		{PR{State: "OPEN", Approvals: 2}, ReviewApproved},
		{
			PR{State: "OPEN", ReviewDecision: "APPROVED", ChangesRequested: 1},
			ReviewChangesRequested,
		},
		{PR{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED"}, ReviewChangesRequested},
		{PR{State: "OPEN", IsDraft: true, Approvals: 1}, ReviewNone},
		{PR{State: "MERGED", Merged: true}, ReviewNone},
	}
	for _, c := range cases {
		if got := c.pr.Review(); got != c.want {
			t.Errorf("%+v: Review() = %v, want %v", c.pr, got, c.want)
		}
	}
}
