//go:build unix

package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestCancelStopsOpAndQueue(t *testing.T) {
	m := testModel()
	// sh waits on sleep, so this only ends quickly if the whole process group
	// is interrupted.
	cmd := m.start(pendingOp{title: "sleep", name: "sh", args: []string{"-c", "sleep 30; true"}})
	m.queue = []pendingOp{{title: "push"}}

	next, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = next.(Model)
	if len(m.queue) != 0 || m.op == nil || !m.op.cancelled {
		t.Fatalf("ctrl+c should cancel, op %+v queue %+v", m.op, m.queue)
	}

	done := make(chan opDoneMsg)
	go func() {
		for {
			switch msg := cmd().(type) {
			case opDoneMsg:
				done <- msg
				return
			case opLineMsg:
				cmd = waitOp(msg.ch)
			}
		}
	}()
	select {
	case msg := <-done:
		next, _ = m.finishOp(msg)
		m = next.(Model)
	case <-time.After(cancelWait / 2):
		t.Fatal("cancelled op did not exit")
	}
	if m.op != nil || m.outputState != opCancelled || m.flashErr {
		t.Fatalf("op %+v state %v flash %q", m.op, m.outputState, m.flash)
	}
}
