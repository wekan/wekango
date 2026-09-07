// Copyright (c) 2026 The Wekan Team
// SPDX-License-Identifier: MIT

// Package termbox implements the terminal API used by mongo-tools' interactive
// mongostat formatter using maintained tcell/v3. This project-owned facade is
// not a general replacement for every historical termbox API. No termbox
// implementation is included; tcell owns terminal input, output and restoration.
package termbox

import (
	"errors"
	"sync"
	"unicode"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

type Attribute uint64
type Key uint16
type EventType uint8
type Modifier uint8

const (
	ColorDefault  Attribute = 0
	ColorBlack    Attribute = 1
	ColorRed      Attribute = 2
	ColorGreen    Attribute = 3
	ColorYellow   Attribute = 4
	ColorBlue     Attribute = 5
	ColorMagenta  Attribute = 6
	ColorCyan     Attribute = 7
	ColorWhite    Attribute = 8
	AttrBold      Attribute = 1 << 9
	AttrUnderline Attribute = 1 << 13
	AttrReverse   Attribute = 1 << 15
)
const (
	KeyCtrlC      Key      = 3
	KeyEsc        Key      = 27
	KeySpace      Key      = 32
	KeyArrowUp    Key      = 0xffed
	KeyArrowDown  Key      = 0xffec
	KeyArrowLeft  Key      = 0xffeb
	KeyArrowRight Key      = 0xffea
	ModAlt        Modifier = 1
)
const (
	EventKey       EventType = 0
	EventResize    EventType = 1
	EventError     EventType = 3
	EventInterrupt EventType = 4
)

type Event struct {
	Type          EventType
	Mod           Modifier
	Key           Key
	Ch            rune
	Width, Height int
	Err           error
}

var terminal struct {
	sync.Mutex
	screen    tcell.Screen
	done      chan struct{}
	interrupt chan struct{}
	pending   []Event
}
var errClosed = errors.New("terminal is not initialized")

// Init switches the process terminal to tcell's interactive screen. Repeated
// initialization while open is harmless; after Close a new screen is created.
func Init() error {
	return initScreen(func() (tcell.Screen, error) { return tcell.NewScreen() })
}
func initScreen(create func() (tcell.Screen, error)) error {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen != nil {
		return nil
	}
	screen, err := create()
	if err != nil {
		return err
	}
	if err = screen.Init(); err != nil {
		screen.Fini()
		return err
	}
	screen.HideCursor()
	terminal.screen = screen
	terminal.done = make(chan struct{})
	terminal.interrupt = make(chan struct{}, 1)
	terminal.pending = nil
	return nil
}

// Close restores the terminal and releases a pending PollEvent. Calls after the
// first Close are harmless. tcell's Fini waits for its input goroutines to exit.
func Close() {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen == nil {
		return
	}
	close(terminal.done)
	terminal.screen.Fini()
	terminal.screen = nil
	terminal.pending = nil
}

func foreground(attr Attribute) color.Color {
	n := attr & 0x1ff
	if n == 0 {
		return color.Default
	}
	return color.PaletteColor(int(n - 1))
}
func cellStyle(fg, bg Attribute) tcell.Style {
	return tcell.StyleDefault.Foreground(foreground(fg)).Background(foreground(bg)).
		Bold(fg&AttrBold != 0).Underline(fg&AttrUnderline != 0).Reverse((fg|bg)&AttrReverse != 0)
}
func Clear(fg, bg Attribute) error {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen == nil {
		return errClosed
	}
	terminal.screen.Fill(' ', cellStyle(fg, bg))
	return nil
}
func SetCell(x, y int, ch rune, fg, bg Attribute) {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen != nil {
		terminal.screen.SetContent(x, y, ch, nil, cellStyle(fg, bg))
	}
}
func SetCursor(x, y int) {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen != nil {
		terminal.screen.ShowCursor(x, y)
	}
}
func Flush() error {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen == nil {
		return errClosed
	}
	terminal.screen.Show()
	return nil
}
func Sync() error {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen == nil {
		return errClosed
	}
	terminal.screen.Sync()
	return nil
}
func Size() (int, int) {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen == nil {
		return 0, 0
	}
	return terminal.screen.Size()
}

// Interrupt wakes a pending PollEvent without changing terminal mode.
func Interrupt() {
	terminal.Lock()
	defer terminal.Unlock()
	if terminal.screen != nil {
		select {
		case terminal.interrupt <- struct{}{}:
		default:
		}
	}
}

func PollEvent() Event {
	terminal.Lock()
	if len(terminal.pending) > 0 {
		event := terminal.pending[0]
		terminal.pending = terminal.pending[1:]
		terminal.Unlock()
		return event
	}
	screen, done, interrupt := terminal.screen, terminal.done, terminal.interrupt
	terminal.Unlock()
	if screen == nil {
		return Event{Type: EventError, Err: errClosed}
	}
	for {
		select {
		case <-done:
			return Event{Type: EventInterrupt}
		case <-interrupt:
			return Event{Type: EventInterrupt}
		case input, ok := <-screen.EventQ():
			if !ok {
				return Event{Type: EventInterrupt}
			}
			switch event := input.(type) {
			case *tcell.EventKey:
				if !event.Pressed() {
					continue
				}
				out := Event{Type: EventKey}
				if event.Modifiers()&tcell.ModAlt != 0 {
					out.Mod = ModAlt
				}
				switch event.Key() {
				case tcell.KeyUp:
					out.Key = KeyArrowUp
				case tcell.KeyDown:
					out.Key = KeyArrowDown
				case tcell.KeyLeft:
					out.Key = KeyArrowLeft
				case tcell.KeyRight:
					out.Key = KeyArrowRight
				case tcell.KeyEscape:
					out.Key = KeyEsc
				case tcell.KeyCtrlC:
					out.Key = KeyCtrlC
				case tcell.KeySpace:
					out.Key = KeySpace
				case tcell.KeyRune:
					chars := []rune(event.Str())
					if len(chars) == 0 {
						continue
					}
					out.Ch = chars[0]
					// tcell reports grapheme strings; the legacy API reports one rune
					// per event. Retain combining runes for the following polls.
					if len(chars) > 1 {
						terminal.Lock()
						if terminal.screen == screen {
							for _, ch := range chars[1:] {
								terminal.pending = append(terminal.pending, Event{Type: EventKey, Mod: out.Mod, Ch: ch})
							}
						}
						terminal.Unlock()
					}
					if out.Ch == ' ' {
						out.Ch = 0
						out.Key = KeySpace
					}
					if event.Modifiers()&tcell.ModCtrl != 0 && unicode.ToLower(out.Ch) == 'c' {
						out.Ch = 0
						out.Key = KeyCtrlC
					}
				default:
					continue
				}
				return out
			case *tcell.EventResize:
				w, h := event.Size()
				return Event{Type: EventResize, Width: w, Height: h}
			case *tcell.EventInterrupt:
				return Event{Type: EventInterrupt}
			case *tcell.EventError:
				return Event{Type: EventError, Err: event}
			}
		}
	}
}
