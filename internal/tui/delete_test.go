package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// markedModel is a review screen with one item marked, ready to act on.
func markedModel(t *testing.T) Model {
	t.Helper()
	m := reviewModel(t)
	m.suspects[0].Marked = true
	m.rebuildView()
	return m
}

func TestDeleteKeyAsksTwice(t *testing.T) {
	m := markedModel(t)

	m = press(m, runes("X"))
	if m.phase != PhaseConfirm {
		t.Fatalf("phase = %v, want the confirmation screen", m.phase)
	}
	if m.applyMode != applyDelete {
		t.Fatalf("applyMode = %v, want delete", m.applyMode)
	}
	out := m.View()
	if !strings.Contains(out, "Permanently delete") {
		t.Errorf("the screen should say this is permanent\n%s", out)
	}
	if strings.Contains(out, "Items go to ~/.Trash") {
		t.Error("the delete screen should not promise the Trash")
	}
	if !strings.Contains(out, "comes back") {
		t.Errorf("the delete screen should say nothing is recoverable\n%s", out)
	}

	// The first yes only escalates: deleting is not undoable, so it takes two.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.phase != PhaseConfirm {
		t.Fatalf("phase = %v, want to still be confirming after one yes", m.phase)
	}
	if cmd != nil {
		t.Error("the first confirmation should not start deleting")
	}
	if m.confirmStep != 1 {
		t.Errorf("confirmStep = %d, want 1", m.confirmStep)
	}
	if !strings.Contains(m.View(), "Really?") {
		t.Error("the second screen should ask again")
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.phase != PhaseApplying {
		t.Errorf("phase = %v, want to be applying after the second yes", m.phase)
	}
	if cmd == nil {
		t.Error("the second confirmation should start the work")
	}
}

// TestTrashKeyStillTakesOneConfirmation: the Trash is reversible, so the extra
// step would only be in the way.
func TestTrashKeyStillTakesOneConfirmation(t *testing.T) {
	m := markedModel(t)

	m = press(m, runes("D"))
	if m.phase != PhaseConfirm || m.applyMode != applyTrash {
		t.Fatalf("phase = %v mode = %v, want trash confirmation", m.phase, m.applyMode)
	}
	if !strings.Contains(m.View(), "Trash") {
		t.Error("the trash screen should say where things are going")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.phase != PhaseApplying {
		t.Errorf("phase = %v, want applying straight after one yes", m.phase)
	}
	if cmd == nil {
		t.Error("confirming should start the work")
	}
}

func TestDeleteCanBeBackedOutOf(t *testing.T) {
	m := markedModel(t)
	m = press(m, runes("X"))
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.phase != PhaseReview {
		t.Errorf("phase = %v, want to be back on the list", m.phase)
	}

	// And from the second screen too, which is the one that matters.
	m = press(m, runes("X"))
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.phase != PhaseReview {
		t.Errorf("phase = %v, want to be back on the list", m.phase)
	}
}

func TestDeleteWithNothingMarkedDoesNothing(t *testing.T) {
	m := reviewModel(t)
	m = press(m, runes("X"))
	if m.phase != PhaseReview {
		t.Errorf("phase = %v, want to stay on the list", m.phase)
	}
	if m.flash == "" {
		t.Error("pressing delete with nothing marked should say so")
	}
}

// TestDeleteConfirmStepResets guards against a second delete inheriting the
// escalation from the first and going straight through.
func TestDeleteConfirmStepResets(t *testing.T) {
	m := markedModel(t)
	m = press(m, runes("X"))
	m = press(m, tea.KeyMsg{Type: tea.KeyEnter}) // now at step 1
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})

	m = press(m, runes("X"))
	if m.confirmStep != 0 {
		t.Fatalf("confirmStep = %d, want a fresh confirmation", m.confirmStep)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(Model).phase != PhaseConfirm {
		t.Error("one keypress deleted after backing out of an earlier attempt")
	}
}
