// +build linux freebsd

// GoSpeccy SDL interface (audio&video output, keyboard input)
package sdl_output

import (
	"errors"
	"flag"
	"fmt"
	"github.com/guntars-lemps/gospeccy/env"
	"github.com/guntars-lemps/gospeccy/interpreter"
	"github.com/guntars-lemps/gospeccy/spectrum"
	"github.com/scottferg/Go-SDL/sdl"
	"github.com/scottferg/Go-SDL/ttf"
	"reflect"
	"sync"
)

const DEFAULT_JOYSTICK_ID = 0

var (
	// Synchronizes the shutdown of SDL event loops.
	// When all SDL event loops terminate, we can call 'sdl.Quit()'.
	shutdown sync.WaitGroup

	// The application renderer
	r *SDLRenderer

	joystick *sdl.Joystick

	composer *SDLSurfaceComposer
)

type SDLSurfaceAccessor interface {
	UpdatedRectsCh() <-chan []sdl.Rect
	GetSurface() *sdl.Surface
}

type cmd_newSurface struct {
	surface SDLSurfaceAccessor
	done    chan bool
}

const (
	HIDE = iota
	SHOW
)

type SDLRenderer struct {
	app                           *spectrum.Application
	speccy                        *spectrum.Spectrum48k
	scale2x, fullscreen           bool
	consoleY                      int16
	width, height                 int
	appSurface, speccySurface     SDLSurfaceAccessor
	toggling                      bool
	appSurfaceCh, speccySurfaceCh chan cmd_newSurface

	audio     bool
	audioFreq uint
	hqAudio   bool
}

type wrapSurface struct {
	surface *sdl.Surface
}

func (s *wrapSurface) GetSurface() *sdl.Surface {
	return s.surface
}

func (s *wrapSurface) UpdatedRectsCh() <-chan []sdl.Rect {
	return nil
}

func width(scale2x, fullscreen bool) int {
	if fullscreen {
		scale2x = true
	}
	if scale2x {
		return spectrum.TotalScreenWidth * 2
	}
	return spectrum.TotalScreenWidth
}

func height(scale2x, fullscreen bool) int {
	if fullscreen {
		scale2x = true
	}
	if scale2x {
		return spectrum.TotalScreenHeight * 2
	}
	return spectrum.TotalScreenHeight
}

func newAppSurface(app *spectrum.Application, scale2x, fullscreen bool) SDLSurfaceAccessor {
	var sdlMode int64
	if fullscreen {
		scale2x = true
		sdlMode |= sdl.FULLSCREEN
		sdl.ShowCursor(sdl.DISABLE)
	} else {
		sdl.ShowCursor(sdl.ENABLE)
		sdlMode |= sdl.SWSURFACE
	}

	<-composer.ReplaceOutputSurface(nil)

	surface := sdl.SetVideoMode(int(width(scale2x, fullscreen)), int(height(scale2x, fullscreen)), 32, uint32(sdlMode))
	if app.Verbose {
		app.PrintfMsg("video surface resolution: %dx%d", surface.W, surface.H)
	}

	<-composer.ReplaceOutputSurface(surface)

	return &wrapSurface{surface}
}

func newSpeccySurface(app *spectrum.Application, speccy *spectrum.Spectrum48k, scale2x, fullscreen bool) SDLSurfaceAccessor {
	var speccySurface SDLSurfaceAccessor
	if fullscreen {
		scale2x = true
	}
	if scale2x {
		sdlScreen := NewSDLScreen2x(app)
		speccy.CommandChannel <- spectrum.Cmd_AddDisplay{sdlScreen}
		speccySurface = sdlScreen
	} else {
		sdlScreen := NewSDLScreen(app)
		speccy.CommandChannel <- spectrum.Cmd_AddDisplay{sdlScreen}
		speccySurface = sdlScreen
	}
	return speccySurface
}

func newFont(scale2x, fullscreen bool) *ttf.Font {
	if fullscreen {
		scale2x = true
	}

	var font *ttf.Font
	{
		path, err := spectrum.FontPath("VeraMono.ttf")
		if err != nil {
			panic(err.Error())
		}
		if scale2x {
			font = ttf.OpenFont(path, 12)
		} else {
			font = ttf.OpenFont(path, 10)
		}
		if font == nil {
			panic(sdl.GetError())
		}
	}

	return font
}

func NewSDLRenderer(app *spectrum.Application, speccy *spectrum.Spectrum48k, scale2x, fullscreen bool, audio, hqAudio bool, audioFreq uint) *SDLRenderer {
	width := width(scale2x, fullscreen)
	height := height(scale2x, fullscreen)
	r := &SDLRenderer{
		app:             app,
		speccy:          speccy,
		scale2x:         scale2x,
		fullscreen:      fullscreen,
		appSurfaceCh:    make(chan cmd_newSurface),
		speccySurfaceCh: make(chan cmd_newSurface),
		appSurface:      newAppSurface(app, scale2x, fullscreen),
		speccySurface:   newSpeccySurface(app, speccy, scale2x, fullscreen),
		width:           width,
		height:          height,
		audio:           audio,
		audioFreq:       audioFreq,
		hqAudio:         hqAudio,
	}

	composer.AddInputSurface(r.speccySurface.GetSurface(), 0, 0, r.speccySurface.UpdatedRectsCh())

	go r.loop()
	return r
}

func (r *SDLRenderer) Terminated() bool {
	return r.app.TerminationInProgress() || r.app.Terminated()
}

func (r *SDLRenderer) ResizeVideo(scale2x, fullscreen bool) {
	finished := make(chan byte)
	r.speccy.CommandChannel <- spectrum.Cmd_CloseAllDisplays{finished}
	<-finished

	if r.scale2x != scale2x {
		if scale2x {
			// 1x --> 2x
			y := int16(r.height) - r.consoleY
			r.consoleY = int16(2*r.height) - 2*y
		} else {
			// 2x --> 1x
			y := int16(r.height) - r.consoleY
			r.consoleY = int16(r.height/2) - y/2
		}
	}

	r.width = width(scale2x, fullscreen)
	r.height = height(scale2x, fullscreen)
	r.scale2x = scale2x
	r.fullscreen = fullscreen

	done := make(chan bool)
	r.appSurfaceCh <- cmd_newSurface{newAppSurface(r.app, scale2x, fullscreen), done}
	<-done

	r.speccySurfaceCh <- cmd_newSurface{newSpeccySurface(r.app, r.speccy, scale2x, fullscreen), done}
	<-done
}

func (r *SDLRenderer) ShowPaintedRegions(enable bool) {
	composer.ShowPaintedRegions(enable)
}

func (r *SDLRenderer) setAudioParameters(enable, hqAudio bool, freq uint) {
	r.audio = enable
	r.hqAudio = hqAudio
	r.audioFreq = freq

	finished := make(chan byte)
	r.speccy.CommandChannel <- spectrum.Cmd_CloseAllAudioReceivers{finished}
	<-finished

	if enable {
		audio, err := NewSDLAudio(r.app, freq, hqAudio)
		if err == nil {
			finished := make(chan byte)
			r.speccy.CommandChannel <- spectrum.Cmd_CloseAllAudioReceivers{finished}
			<-finished

			r.speccy.CommandChannel <- spectrum.Cmd_AddAudioReceiver{audio}
		} else {
			r.app.PrintfMsg("%s", err)
			return
		}
	}
}

func (r *SDLRenderer) EnableAudio(enable bool) {
	r.setAudioParameters(enable, r.hqAudio, r.audioFreq)
}

func (r *SDLRenderer) SetAudioFreq(freq uint) {
	if r.audioFreq != freq {
		r.setAudioParameters(r.audio, r.hqAudio, freq)
	}
}

func (r *SDLRenderer) SetAudioQuality(hqAudio bool) {
	if r.hqAudio != hqAudio {
		r.setAudioParameters(r.audio, hqAudio, r.audioFreq)
	}
}

func (r *SDLRenderer) loop() {

	evtLoop := r.app.NewEventLoop()

	shutdown.Add(1)
	for {
		select {
		case <-evtLoop.Pause:

			evtLoop.Pause <- 0

		case <-evtLoop.Terminate:
			// Terminate this Go routine
			if r.app.Verbose {
				r.app.PrintfMsg("frontend SDL renderer event loop: exit")
			}
			evtLoop.Terminate <- 0
			shutdown.Done()
			return

		case cmd := <-r.speccySurfaceCh:
			<-composer.RemoveAllInputSurfaces()

			r.speccySurface.GetSurface().Free()
			r.speccySurface = cmd.surface

			composer.AddInputSurface(r.speccySurface.GetSurface(), 0, 0, r.speccySurface.UpdatedRectsCh())

			cmd.done <- true

		case cmd := <-r.appSurfaceCh:
			<-composer.ReplaceOutputSurface(nil)

			r.appSurface.GetSurface().Free()
			r.appSurface = cmd.surface

			<-composer.ReplaceOutputSurface(r.appSurface.GetSurface())

			cmd.done <- true
		}
	}
}

type interpreterAccess_t struct{}

func (i *interpreterAccess_t) Run(sourceCode string) error {
	intp := interpreter.GetInterpreter()

	err := intp.Run(sourceCode)

	return err
}

// A Go routine for processing SDL events.
func sdlEventLoop(app *spectrum.Application, speccy *spectrum.Spectrum48k, verboseInput bool) {
	evtLoop := app.NewEventLoop()

	shutdown.Add(1)
	for {
		select {
		case <-evtLoop.Pause:
			evtLoop.Pause <- 0

		case <-evtLoop.Terminate:
			// Terminate this Go routine
			if app.Verbose {
				app.PrintfMsg("SDL event loop: exit")
			}
			evtLoop.Terminate <- 0
			shutdown.Done()
			return

		case event := <-sdl.Events:
			switch e := event.(type) {
			case sdl.QuitEvent:
				if app.Verbose {
					app.PrintfMsg("SDL quit -> request[exit the application]")
				}
				app.RequestExit()

			case sdl.JoyAxisEvent:
				if verboseInput {
					app.PrintfMsg("[Joystick] Axis: %d, Value: %d", e.Axis, e.Value)
				}
				if e.Axis == 0 {
					if e.Value > 0 {
						speccy.Joystick.KempstonDown(spectrum.KEMPSTON_RIGHT)
					} else if e.Value < 0 {
						speccy.Joystick.KempstonDown(spectrum.KEMPSTON_LEFT)
					} else {
						speccy.Joystick.KempstonUp(spectrum.KEMPSTON_RIGHT)
						speccy.Joystick.KempstonUp(spectrum.KEMPSTON_LEFT)
					}
				} else if e.Axis == 1 {
					if e.Value > 0 {
						speccy.Joystick.KempstonDown(spectrum.KEMPSTON_UP)
					} else if e.Value < 0 {
						speccy.Joystick.KempstonDown(spectrum.KEMPSTON_DOWN)
					} else {
						speccy.Joystick.KempstonUp(spectrum.KEMPSTON_UP)
						speccy.Joystick.KempstonUp(spectrum.KEMPSTON_DOWN)
					}
				}

			case sdl.JoyButtonEvent:
				if verboseInput {
					app.PrintfMsg("[Joystick] Button: %d, State: %d", e.Button, e.State)
				}
				if e.Button == 0 {
					if e.State > 0 {
						speccy.Joystick.KempstonDown(spectrum.KEMPSTON_FIRE)
					} else {
						speccy.Joystick.KempstonUp(spectrum.KEMPSTON_FIRE)
					}
				}

			case sdl.KeyboardEvent:
				keyName := sdl.GetKeyName(sdl.Key(e.Keysym.Sym))

				if verboseInput {
					app.PrintfMsg("\n")
					app.PrintfMsg("%v: %v", e.Keysym.Sym, keyName)
					app.PrintfMsg("Type: %02x Which: %02x State: %02x\n", e.Type, e.Which, e.State)
					app.PrintfMsg("Scancode: %02x Sym: %08x Mod: %04x Unicode: %04x\n", e.Keysym.Scancode, e.Keysym.Sym, e.Keysym.Mod, e.Keysym.Unicode)
				}

				if (keyName == "escape") && (e.Type == sdl.KEYDOWN) {
					if app.Verbose {
						app.PrintfMsg("escape key -> request[exit the application]")
					}
					app.RequestExit()

				} else {
					sequence, haveMapping := spectrum.SDL_KeyMap[keyName]

					if haveMapping {
						switch e.Type {
						case sdl.KEYDOWN:
							// Normal order
							for i := 0; i < len(sequence); i++ {
								speccy.Keyboard.KeyDown(sequence[i])
							}
						case sdl.KEYUP:
							// Reverse order
							for i := len(sequence) - 1; i >= 0; i-- {
								speccy.Keyboard.KeyUp(sequence[i])
							}
						}
					}
				}
			}
		}
	}
}

func initSDLSubSystems(app *spectrum.Application) error {
	if sdl.Init(sdl.INIT_VIDEO|sdl.INIT_AUDIO|sdl.INIT_JOYSTICK) != 0 {
		return errors.New(sdl.GetError())
	}
	if ttf.Init() != 0 {
		return errors.New(sdl.GetError())
	}
	if sdl.NumJoysticks() > 0 {
		// Open joystick
		joystick = sdl.JoystickOpen(DEFAULT_JOYSTICK_ID)
		if joystick != nil {
			if app.Verbose {
				app.PrintfMsg("Opened Joystick %d", DEFAULT_JOYSTICK_ID)
				app.PrintfMsg("Name: %s", sdl.JoystickName(DEFAULT_JOYSTICK_ID))
				app.PrintfMsg("Number of Axes: %d", joystick.NumAxes())
				app.PrintfMsg("Number of Buttons: %d", joystick.NumButtons())
				app.PrintfMsg("Number of Balls: %d", joystick.NumBalls())
			}
		} else {
			return errors.New("Couldn't open Joystick!")
		}
	}
	sdl.WM_SetCaption("GoSpeccy - ZX Spectrum Emulator", "")
	sdl.EnableUNICODE(1)
	return nil
}

var (
	enableSDL          = flag.Bool("enable-sdl", true, "Enable SDL user interface")
	Scale2x            = flag.Bool("2x", false, "2x display scaler")
	Fullscreen         = flag.Bool("fullscreen", false, "Fullscreen (enable 2x scaler by default)")
	Audio              = flag.Bool("audio", true, "Enable or disable audio")
	AudioFreq          = flag.Uint("audio-freq", PLAYBACK_FREQUENCY, "Audio playback frequency (units: Hz)")
	HQAudio            = flag.Bool("audio-hq", true, "Enable or disable higher-quality audio")
	ShowPaintedRegions = flag.Bool("show-paint", false, "Show painted display regions")
	verboseInput       = flag.Bool("verbose-input", false, "Enable debugging messages (input device events)")
)

func init() {
	uiSettings = &InitialSettings{
		scale2x:            Scale2x,
		fullscreen:         Fullscreen,
		showPaintedRegions: ShowPaintedRegions,
		audio:              Audio,
		audioFreq:          AudioFreq,
		hqAudio:            HQAudio,
	}
}

func Main() {
	var init_waitGroup *sync.WaitGroup
	init_waitGroup = env.WaitName("init WaitGroup").(*sync.WaitGroup)
	init_waitGroup.Add(1)

	var app *spectrum.Application
	app = env.Wait(reflect.TypeOf(app)).(*spectrum.Application)

	var speccy *spectrum.Spectrum48k
	speccy = env.Wait(reflect.TypeOf(speccy)).(*spectrum.Spectrum48k)

	if !*enableSDL {
		return
	}

	uiSettings = &InitialSettings{
		scale2x:            Scale2x,
		fullscreen:         Fullscreen,
		showPaintedRegions: ShowPaintedRegions,
		audio:              Audio,
		audioFreq:          AudioFreq,
		hqAudio:            HQAudio,
	}

	composer = NewSDLSurfaceComposer(app)
	composer.ShowPaintedRegions(*ShowPaintedRegions)

	// SDL subsystems init
	if err := initSDLSubSystems(app); err != nil {
		app.PrintfMsg("%s", err)
		app.RequestExit()
		return
	}

	// Setup the display
	r = NewSDLRenderer(app, speccy, *Scale2x, *Fullscreen, *Audio, *HQAudio, *AudioFreq)
	setUI(r)

	// Setup the audio
	if *Audio {
		audio, err := NewSDLAudio(app, *AudioFreq, *HQAudio)
		if err == nil {
			speccy.CommandChannel <- spectrum.Cmd_AddAudioReceiver{audio}
		} else {
			app.PrintfMsg("%s", err)
		}
	}

	// Start the SDL event loop
	go sdlEventLoop(app, speccy, *verboseInput)

	init_waitGroup.Done()

	hint := "Hint: Press F10 to invoke the built-in console.\n"
	hint += "      Input an empty line in the console to display available commands.\n"
	fmt.Print(hint)

	// Wait for all event loops to terminate, and then call 'sdl.Quit()'
	shutdown.Wait()
	if r.app.Verbose {
		r.app.PrintfMsg("SDL: sdl.Quit()")
	}
	sdl.Quit()
}
