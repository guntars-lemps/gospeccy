/*

Copyright (c) 2010 Andrea Fazzi

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package spectrum

import (
	"sync"
	"time"
)

type rowState struct {
	row, state byte
}

type Cmd_KeyPress struct {
	logicalKeyCode uint
	done           chan bool
}

type Cmd_SendLoad struct {
	romType RomType
}

type Keyboard struct {
	speccy    *Spectrum48k
	keyStates [8]byte
	mutex     sync.RWMutex

	CommandChannel chan interface{}
}

func NewKeyboard() *Keyboard {
	keyboard := &Keyboard{}
	keyboard.reset()

	keyboard.CommandChannel = make(chan interface{})

	return keyboard
}

func (keyboard *Keyboard) init(speccy *Spectrum48k) {
	keyboard.speccy = speccy
	go keyboard.commandLoop()
}

func (keyboard *Keyboard) delayAfterKeyDown() {
	// Sleep for 1 frame
	time.Sleep(1e9 / time.Duration(keyboard.speccy.GetCurrentFPS()))
}

func (keyboard *Keyboard) delayAfterKeyUp() {
	// Sleep for 10 frames
	time.Sleep(10 * 1e9 / time.Duration(keyboard.speccy.GetCurrentFPS()))
}

func (keyboard *Keyboard) commandLoop() {
	evtLoop := keyboard.speccy.app.NewEventLoop()
	for {
		select {

		case <-evtLoop.Pause:
			evtLoop.Pause <- 0

		case <-evtLoop.Terminate:
			// Terminate this Go routine
			if evtLoop.App().Verbose {
				evtLoop.App().PrintfMsg("keyboard command loop: exit")
			}
			evtLoop.Terminate <- 0
			return

		case untyped_cmd := <-keyboard.CommandChannel:
			switch cmd := untyped_cmd.(type) {
			case Cmd_KeyPress:
				keyboard.KeyDown(cmd.logicalKeyCode)
				keyboard.delayAfterKeyDown()
				keyboard.KeyUp(cmd.logicalKeyCode)
				keyboard.delayAfterKeyUp()
				cmd.done <- true

			case Cmd_SendLoad:
				if cmd.romType == ROM48 {
					// LOAD
					keyboard.KeyDown(KEY_J)
					keyboard.delayAfterKeyDown()
					keyboard.KeyUp(KEY_J)
					keyboard.delayAfterKeyUp()

					// " "
					keyboard.KeyDown(KEY_SymbolShift)
					{
						keyboard.KeyDown(KEY_P)
						keyboard.delayAfterKeyDown()
						keyboard.KeyUp(KEY_P)
						keyboard.delayAfterKeyUp()

						keyboard.KeyDown(KEY_P)
						keyboard.delayAfterKeyDown()
						keyboard.KeyUp(KEY_P)
						keyboard.delayAfterKeyUp()
					}
					keyboard.KeyUp(KEY_SymbolShift)

					keyboard.KeyDown(KEY_Enter)
					keyboard.delayAfterKeyDown()
					keyboard.KeyUp(KEY_Enter)
				}
			}
		}
	}

}

func (k *Keyboard) reset() {
	// Initialize 'k.keyStates'
	for row := uint(0); row < 8; row++ {
		k.SetKeyState(row, 0xff)
	}
}

func (keyboard *Keyboard) GetKeyState(row uint) byte {
	keyboard.mutex.RLock()
	keyState := keyboard.keyStates[row]
	keyboard.mutex.RUnlock()
	return keyState
}

func (keyboard *Keyboard) SetKeyState(row uint, state byte) {
	keyboard.mutex.Lock()
	keyboard.keyStates[row] = state
	keyboard.mutex.Unlock()
}

func (keyboard *Keyboard) KeyDown(logicalKeyCode uint) {
	keyCode, ok := keyCodes[logicalKeyCode]

	if ok {
		keyboard.mutex.Lock()
		keyboard.keyStates[keyCode.row] &= ^(keyCode.mask)
		keyboard.mutex.Unlock()
	}
}

func (keyboard *Keyboard) KeyUp(logicalKeyCode uint) {
	keyCode, ok := keyCodes[logicalKeyCode]

	if ok {
		keyboard.mutex.Lock()
		keyboard.keyStates[keyCode.row] |= (keyCode.mask)
		keyboard.mutex.Unlock()
	}
}

func (keyboard *Keyboard) KeyPress(logicalKeyCode uint) chan bool {
	done := make(chan bool)
	keyboard.CommandChannel <- Cmd_KeyPress{logicalKeyCode, done}
	return done
}

func (keyboard *Keyboard) KeyPressSequence(logicalKeyCodes ...uint) chan bool {
	done := make(chan bool, len(logicalKeyCodes))
	for _, keyCode := range logicalKeyCodes {
		keyboard.CommandChannel <- Cmd_KeyPress{keyCode, done}
	}
	return done
}

// Logical key codes
const (
	KEY_1 = iota
	KEY_2
	KEY_3
	KEY_4
	KEY_5
	KEY_6
	KEY_7
	KEY_8
	KEY_9
	KEY_0

	KEY_Q
	KEY_W
	KEY_E
	KEY_R
	KEY_T
	KEY_Y
	KEY_U
	KEY_I
	KEY_O
	KEY_P

	KEY_A
	KEY_S
	KEY_D
	KEY_F
	KEY_G
	KEY_H
	KEY_J
	KEY_K
	KEY_L
	KEY_Enter

	KEY_CapsShift
	KEY_Z
	KEY_X
	KEY_C
	KEY_V
	KEY_B
	KEY_N
	KEY_M
	KEY_SymbolShift
	KEY_Space
)

type keyCell struct {
	row, mask byte
}

var keyCodes = map[uint]keyCell{
	KEY_1: {row: 3, mask: 0x01},
	KEY_2: {row: 3, mask: 0x02},
	KEY_3: {row: 3, mask: 0x04},
	KEY_4: {row: 3, mask: 0x08},
	KEY_5: {row: 3, mask: 0x10},
	KEY_6: {row: 4, mask: 0x10},
	KEY_7: {row: 4, mask: 0x08},
	KEY_8: {row: 4, mask: 0x04},
	KEY_9: {row: 4, mask: 0x02},
	KEY_0: {row: 4, mask: 0x01},

	KEY_Q: {row: 2, mask: 0x01},
	KEY_W: {row: 2, mask: 0x02},
	KEY_E: {row: 2, mask: 0x04},
	KEY_R: {row: 2, mask: 0x08},
	KEY_T: {row: 2, mask: 0x10},
	KEY_Y: {row: 5, mask: 0x10},
	KEY_U: {row: 5, mask: 0x08},
	KEY_I: {row: 5, mask: 0x04},
	KEY_O: {row: 5, mask: 0x02},
	KEY_P: {row: 5, mask: 0x01},

	KEY_A:     {row: 1, mask: 0x01},
	KEY_S:     {row: 1, mask: 0x02},
	KEY_D:     {row: 1, mask: 0x04},
	KEY_F:     {row: 1, mask: 0x08},
	KEY_G:     {row: 1, mask: 0x10},
	KEY_H:     {row: 6, mask: 0x10},
	KEY_J:     {row: 6, mask: 0x08},
	KEY_K:     {row: 6, mask: 0x04},
	KEY_L:     {row: 6, mask: 0x02},
	KEY_Enter: {row: 6, mask: 0x01},

	KEY_CapsShift:   {row: 0, mask: 0x01},
	KEY_Z:           {row: 0, mask: 0x02},
	KEY_X:           {row: 0, mask: 0x04},
	KEY_C:           {row: 0, mask: 0x08},
	KEY_V:           {row: 0, mask: 0x10},
	KEY_B:           {row: 7, mask: 0x10},
	KEY_N:           {row: 7, mask: 0x08},
	KEY_M:           {row: 7, mask: 0x04},
	KEY_SymbolShift: {row: 7, mask: 0x02},
	KEY_Space:       {row: 7, mask: 0x01},
}

var SDL_KeyMap = map[string][]uint{
	"0": {KEY_0},
	"1": {KEY_1},
	"2": {KEY_2},
	"3": {KEY_3},
	"4": {KEY_4},
	"5": {KEY_5},
	"6": {KEY_6},
	"7": {KEY_7},
	"8": {KEY_8},
	"9": {KEY_9},

	"a": {KEY_A},
	"b": {KEY_B},
	"c": {KEY_C},
	"d": {KEY_D},
	"e": {KEY_E},
	"f": {KEY_F},
	"g": {KEY_G},
	"h": {KEY_H},
	"i": {KEY_I},
	"j": {KEY_J},
	"k": {KEY_K},
	"l": {KEY_L},
	"m": {KEY_M},
	"n": {KEY_N},
	"o": {KEY_O},
	"p": {KEY_P},
	"q": {KEY_Q},
	"r": {KEY_R},
	"s": {KEY_S},
	"t": {KEY_T},
	"u": {KEY_U},
	"v": {KEY_V},
	"w": {KEY_W},
	"x": {KEY_X},
	"y": {KEY_Y},
	"z": {KEY_Z},

	"return":      {KEY_Enter},
	"space":       {KEY_Space},
	"left shift":  {KEY_CapsShift},
	"right shift": {KEY_CapsShift},
	"left ctrl":   {KEY_SymbolShift},
	"right ctrl":  {KEY_SymbolShift},

	//"escape":    []uint{KEY_CapsShift, KEY_1},
	//"caps lock": []uint{KEY_CapsShift, KEY_2}, // FIXME: SDL never sends the sdl.KEYUP event
	"left":      {KEY_CapsShift, KEY_5},
	"down":      {KEY_CapsShift, KEY_6},
	"up":        {KEY_CapsShift, KEY_7},
	"right":     {KEY_CapsShift, KEY_8},
	"backspace": {KEY_CapsShift, KEY_0},

	"-": {KEY_SymbolShift, KEY_J},
	//"_": []uint{KEY_SymbolShift, KEY_0},
	"=": {KEY_SymbolShift, KEY_L},
	//"+": []uint{KEY_SymbolShift, KEY_K},
	"[": {KEY_SymbolShift, KEY_8}, // Maps to "("
	"]": {KEY_SymbolShift, KEY_9}, // Maps to ")"
	";": {KEY_SymbolShift, KEY_O},
	//":": []uint{KEY_SymbolShift, KEY_Z},
	"'": {KEY_SymbolShift, KEY_7},
	//"\"": []uint{KEY_SymbolShift, KEY_P},
	",": {KEY_SymbolShift, KEY_N},
	".": {KEY_SymbolShift, KEY_M},
	"/": {KEY_SymbolShift, KEY_V},
	//"<": []uint{KEY_SymbolShift, KEY_R},
	//">": []uint{KEY_SymbolShift, KEY_T},
	//"?": []uint{KEY_SymbolShift, KEY_C},

	// Keypad
	"[0]": {KEY_0},
	"[1]": {KEY_1},
	"[2]": {KEY_2},
	"[3]": {KEY_3},
	"[4]": {KEY_4},
	"[5]": {KEY_5},
	"[6]": {KEY_6},
	"[7]": {KEY_7},
	"[8]": {KEY_8},
	"[9]": {KEY_9},
	"[*]": {KEY_SymbolShift, KEY_B},
	"[-]": {KEY_SymbolShift, KEY_J},
	"[+]": {KEY_SymbolShift, KEY_K},
	"[/]": {KEY_SymbolShift, KEY_V},
}

func init() {
	if len(keyCodes) != 40 {
		panic("invalid keyboard specification")
	}

	// Make sure we are able to press every button on the Spectrum keyboard
	used := make(map[uint]bool)
	for logicalKeyCode := range keyCodes {
		used[logicalKeyCode] = false
	}
	for _, seq := range SDL_KeyMap {
		if len(seq) == 1 {
			used[seq[0]] = true
		}
	}
	for _, isUsed := range used {
		if !isUsed {
			panic("some key is missing in the SDL keymap")
		}
	}
}
