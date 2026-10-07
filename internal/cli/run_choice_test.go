package cli

import (
	"strings"
	"testing"
)

func TestLayoutChoiceBarHits(t *testing.T) {
	opts := permChoiceOpts()
	line, hits := layoutChoiceBar(opts, 0)
	if !strings.Contains(line, "[ Allow ]") || !strings.Contains(line, "Deny") {
		t.Fatalf("%q", line)
	}
	if len(hits) != 4 {
		t.Fatalf("hits %d", len(hits))
	}
	if i := hitChoice(hits, hits[0].X0); i != 0 {
		t.Fatalf("click allow got %d", i)
	}
	if i := hitChoice(hits, hits[1].X0); i != 1 {
		t.Fatalf("click deny got %d", i)
	}
	if hitChoice(hits, 0) != -1 {
		t.Fatal("expected miss")
	}
}

func TestApplyChoiceEventKeys(t *testing.T) {
	opts := permChoiceOpts()
	_, hits := layoutChoiceBar(opts, 0)
	sel, id, done := applyChoiceEvent(keyEvent{kind: keyRight}, 0, opts, hits)
	if done || sel != 1 {
		t.Fatalf("right sel=%d done=%v", sel, done)
	}
	sel, id, done = applyChoiceEvent(keyEvent{kind: keyLeft}, 0, opts, hits)
	if done || sel != 3 {
		t.Fatalf("wrap left sel=%d", sel)
	}
	_, id, done = applyChoiceEvent(keyEvent{kind: keyRune, r: '\r'}, 0, opts, hits)
	if !done || id != "allow" {
		t.Fatalf("enter %q %v", id, done)
	}
	_, id, done = applyChoiceEvent(keyEvent{kind: keyRune, r: 'n'}, 0, opts, hits)
	if !done || id != "deny" {
		t.Fatalf("n %q", id)
	}
	_, id, done = applyChoiceEvent(keyEvent{kind: keyRune, r: 'a'}, 0, opts, hits)
	if !done || id != "always" {
		t.Fatalf("a %q", id)
	}
	_, id, done = applyChoiceEvent(keyEvent{kind: keyRune, r: 3}, 2, opts, hits)
	if !done || id != "deny" {
		t.Fatalf("ctrl-c %q", id)
	}
	clickX := hits[2].X0
	_, id, done = applyChoiceEvent(keyEvent{kind: keyMouse, mouse: mouseEv{x: clickX, y: 1, btn: 0}}, 0, opts, hits)
	if !done || id != "always" {
		t.Fatalf("click %q", id)
	}
}

func TestShortcutChoice(t *testing.T) {
	if shortcutChoice(permChoiceOpts(), 'Y') != 0 {
		t.Fatal("Y")
	}
	if shortcutChoice(resizeChoiceOpts(), 'n') != 1 {
		t.Fatal("n cancel")
	}
}
