// Copyright (c) 2026 The Wekan Team
// SPDX-License-Identifier: MIT
package termbox

import (
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"github.com/gdamore/tcell/v3/vt"
)

// v3 removed NewSimulationScreen. Its own vt.MockTerm exercises the actual
// tcell screen, escape encoder/decoder, Unicode cells and terminal lifecycle
// without opening a host terminal.
func mockScreen(t *testing.T) (tcell.Screen, vt.MockTerm) {
	t.Helper()
	Close()
	term := vt.NewMockTerm(vt.MockOptSize{X: 40, Y: 12}, vt.MockOptColors(256))
	screen, err := tcell.NewTerminfoScreenFromTty(term)
	if err != nil {
		t.Fatal(err)
	}
	if err = initScreen(func() (tcell.Screen, error) { return screen, nil }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	return screen, term
}

func awaitEvent(t *testing.T, typ EventType) Event {
	t.Helper()
	result := make(chan Event, 1)
	go func() {
		for {
			ev := PollEvent()
			if ev.Type == typ || ev.Type == EventError {
				result <- ev
				return
			}
		}
	}()
	select {
	case ev := <-result:
		if ev.Type != typ {
			t.Fatalf("unexpected event %+v", ev)
		}
		return ev
	case <-time.After(3 * time.Second):
		Close()
		t.Fatal("terminal event timed out")
		return Event{}
	}
}

func TestStylesUnicodeCursorAndRedraw(t *testing.T) {
	screen, term := mockScreen(t)
	if err := Clear(ColorDefault, ColorDefault); err != nil {
		t.Fatal(err)
	}
	SetCell(2, 1, '界', ColorWhite|AttrBold|AttrUnderline, ColorBlack)
	SetCell(5, 1, 'é', ColorBlack, ColorWhite)
	SetCell(-1, -1, 'x', ColorWhite, ColorBlack)
	SetCursor(5, 1)
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	str, style, width := screen.Get(2, 1)
	if str != "界" || width != 2 || !style.HasBold() || !style.HasUnderline() || style.GetForeground() != color.PaletteColor(7) || style.GetBackground() != color.PaletteColor(0) {
		t.Fatalf("logical cell %q width=%d style=%+v", str, width, style)
	}
	// Show/Sync write to the mock emulator; inspect the displayed cell as well
	// as the logical screen buffer, so missing flushes cannot pass this check.
	if err := term.Drain(); err != nil {
		t.Fatal(err)
	}
	cell := term.GetCell(vt.Coord{X: 2, Y: 1})
	if cell.C != "界" || cell.S.Attr()&(vt.Bold|vt.Underline) != (vt.Bold|vt.Underline) {
		t.Fatalf("displayed cell %+v", cell)
	}
	if !term.Backend().GetCursor().IsVisible() {
		t.Fatal("cursor hidden after SetCursor")
	}
	SetCursor(-1, -1)
	if err := Sync(); err != nil {
		t.Fatal(err)
	}
	if err := term.Drain(); err != nil {
		t.Fatal(err)
	}
	if term.Backend().GetCursor().IsVisible() {
		t.Fatal("negative cursor coordinates did not hide cursor")
	}
	if err := Clear(ColorBlack, ColorWhite); err != nil {
		t.Fatal(err)
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	str, style, _ = screen.Get(2, 1)
	if str != " " || style.GetBackground() != color.PaletteColor(7) {
		t.Fatalf("clear %q %+v", str, style)
	}
}

func TestKeyResizeAndInterruptEvents(t *testing.T) {
	screen, term := mockScreen(t)
	tests := []struct {
		input *tcell.EventKey
		key   Key
		ch    rune
		mod   Modifier
	}{
		{tcell.NewEventKey(tcell.KeyUp, "", tcell.ModNone), KeyArrowUp, 0, 0},
		{tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone), KeyArrowDown, 0, 0},
		{tcell.NewEventKey(tcell.KeyLeft, "", tcell.ModNone), KeyArrowLeft, 0, 0},
		{tcell.NewEventKey(tcell.KeyRight, "", tcell.ModNone), KeyArrowRight, 0, 0},
		{tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone), KeyEsc, 0, 0},
		{tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl), KeyCtrlC, 0, 0},
		{tcell.NewEventKeyEx(tcell.KeyRune, "c", tcell.ModCtrl, true, 0, 1), KeyCtrlC, 0, 0},
		{tcell.NewEventKey(tcell.KeyRune, " ", tcell.ModNone), KeySpace, 0, 0},
		{tcell.NewEventKey(tcell.KeyRune, "界", tcell.ModAlt), 0, '界', ModAlt},
	}
	for _, tt := range tests {
		screen.EventQ() <- tt.input
		ev := awaitEvent(t, EventKey)
		if ev.Key != tt.key || ev.Ch != tt.ch || ev.Mod != tt.mod {
			t.Fatalf("key %+v want %+v", ev, tt)
		}
	}
	screen.EventQ() <- tcell.NewEventKey(tcell.KeyRune, "e\u0301", tcell.ModNone)
	for _, want := range []rune{'e', '\u0301'} {
		if ev := awaitEvent(t, EventKey); ev.Ch != want {
			t.Fatalf("grapheme rune %+v want %q", ev, want)
		}
	}
	// A physical key traverses the maintained mock's escape protocol and tcell
	// input parser, instead of only injecting pre-parsed synthetic events.
	term.KeyTap(vt.KeyQ)
	if ev := awaitEvent(t, EventKey); ev.Ch != 'q' {
		t.Fatalf("physical q %+v", ev)
	}
	term.SetSize(vt.Coord{X: 55, Y: 17})
	for {
		ev := awaitEvent(t, EventResize)
		if ev.Width == 55 && ev.Height == 17 {
			break
		}
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	if w, h := Size(); w != 55 || h != 17 {
		t.Fatalf("screen size %dx%d", w, h)
	}
	Interrupt()
	awaitEvent(t, EventInterrupt)
}

func TestCloseAndInitializationFailure(t *testing.T) {
	Close()
	expected := errors.New("test tty unavailable")
	if err := initScreen(func() (tcell.Screen, error) { return nil, expected }); !errors.Is(err, expected) {
		t.Fatalf("init failure %v", err)
	}
	screen, _ := mockScreen(t)
	if err := initScreen(func() (tcell.Screen, error) { t.Fatal("repeated init opened another terminal"); return nil, expected }); err != nil {
		t.Fatal(err)
	}
	screen.EventQ() <- tcell.NewEventInterrupt(nil)
	awaitEvent(t, EventInterrupt)
	pending := make(chan Event, 1)
	go func() {
		for {
			ev := PollEvent()
			if ev.Type == EventInterrupt || ev.Type == EventError {
				pending <- ev
				return
			}
		}
	}()
	Close()
	select {
	case ev := <-pending:
		if ev.Type != EventInterrupt && ev.Type != EventError {
			t.Fatalf("close event %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close left poll blocked")
	}
	Close()
	if err := Flush(); err == nil {
		t.Fatal("closed flush succeeded")
	}
	if err := Clear(ColorDefault, ColorDefault); err == nil {
		t.Fatal("closed clear succeeded")
	}
	if err := Sync(); err == nil {
		t.Fatal("closed sync succeeded")
	}
	if ev := PollEvent(); ev.Type != EventError || ev.Err == nil {
		t.Fatalf("closed poll %+v", ev)
	}
}
