package cli

import (
	"strings"
	"testing"
)

func TestWrapANSISplitsAtWidth(t *testing.T) {
	rows := wrapANSI("abcdefghij", 4)
	if len(rows) != 3 || rows[0] != "abcd" || rows[1] != "efgh" || rows[2] != "ij" {
		t.Fatalf("%q", rows)
	}
	colored := "\033[32mab\033[0mcd"
	rows = wrapANSI(colored, 2)
	if len(rows) != 2 {
		t.Fatalf("%d %q", len(rows), rows)
	}
	if visibleLen(rows[0]) != 2 || visibleLen(rows[1]) != 2 {
		t.Fatalf("visible %q %q", rows[0], rows[1])
	}
	if !strings.Contains(rows[0], "ab") {
		t.Fatalf("%q", rows[0])
	}
}

func TestPasteRunesNewlinesBecomeSpaces(t *testing.T) {
	if got := string(pasteRunes("ab\r\ncd\te")); got != "ab cd e" {
		t.Fatalf("%q", got)
	}
	buf, cur := insertRunes([]rune("ac"), 1, []rune("b"))
	if string(buf) != "abc" || cur != 2 {
		t.Fatalf("%q %d", buf, cur)
	}
}

func TestParseSGRMouseAndRect(t *testing.T) {
	ev, ok := parseSGRMouse("<0;4;8", 'M')
	if !ok || ev.btn != 0 || ev.x != 4 || ev.y != 8 || ev.release || ev.motion {
		t.Fatalf("%+v %v", ev, ok)
	}
	ev, ok = parseSGRMouse("<32;5;9", 'M')
	if !ok || !ev.motion || ev.btn != 0 || ev.x != 5 {
		t.Fatalf("%+v", ev)
	}
	ev, ok = parseSGRMouse("<0;4;8", 'm')
	if !ok || !ev.release {
		t.Fatalf("%+v", ev)
	}
	rows := []string{"abcdef", "ghijkl"}
	got := extractScreenRect(rows, 1, 2, 1, 3, 1)
	if got != "bc" {
		t.Fatalf("%q", got)
	}
	ev, ok = parseSGRMouse("<64;10;5", 'M')
	if !ok || ev.wheel != -1 {
		t.Fatalf("wheel up %+v", ev)
	}
	ev, ok = parseSGRMouse("<65;10;5", 'M')
	if !ok || ev.wheel != 1 {
		t.Fatalf("wheel down %+v", ev)
	}
	ev, ok = parseSGRMouse("<68;10;5", 'M')
	if !ok || ev.wheel != -1 {
		t.Fatalf("shift+wheel %+v", ev)
	}
	if parseCSI("5", '~').kind != keyPageUp || parseCSI("6", '~').kind != keyPageDown {
		t.Fatal("page keys")
	}
	ed := &runLineEditor{Prompt: "> ", regionTop: 1, regionBot: 6}
	ed.Capture = &lineBuf{}
	for _, ln := range []string{"a", "b", "c", "d", "e", "f"} {
		_, _ = ed.Capture.Write([]byte(ln + "\n"))
	}
	chat, _, chatH := ed.visibleChatAndInput(nil)
	if chatH != 5 {
		t.Fatalf("chatH %d", chatH)
	}
	if strings.Join(chat, ",") != "b,c,d,e,f" {
		t.Fatalf("bottom window %q", chat)
	}
	ed.scrollOff = 2
	chat, _, _ = ed.visibleChatAndInput(nil)
	if strings.Join(chat, ",") != "a,b,c,d," && strings.Join(chat, ",") != "a,b,c,d,e" {
		if chat[0] != "a" {
			t.Fatalf("scrolled window %q", chat)
		}
	}
}

func TestParseCSIArrowsAndPaste(t *testing.T) {
	if parseCSI("", 'A').kind != keyUp {
		t.Fatal("up")
	}
	if parseCSI("200", '~').kind != keyPasteStart {
		t.Fatal("paste")
	}
	if parseCSI("201", '~').kind != keyPasteEnd {
		t.Fatal("paste end")
	}
	ev := parseCSI("10;20", 'R')
	if ev.kind != keyCPR || ev.cprY != 10 || ev.cprX != 20 {
		t.Fatalf("%+v", ev)
	}
}
