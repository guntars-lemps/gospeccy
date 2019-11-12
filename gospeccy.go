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

// GoSpeccy
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"github.com/guntars-lemps/gospeccy/env"
	"github.com/guntars-lemps/gospeccy/formats"
	"github.com/guntars-lemps/gospeccy/interpreter"
	"github.com/guntars-lemps/gospeccy/output/sdl"
	"github.com/guntars-lemps/gospeccy/spectrum"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type handler_SIGTERM struct {
	app *spectrum.Application
}

func (h *handler_SIGTERM) HandleSignal(s os.Signal) {
	switch ss := s.(type) {
	case syscall.Signal:
		switch ss {
		case syscall.SIGTERM, syscall.SIGINT:
			if h.app.Verbose {
				h.app.PrintfMsg("%v", ss)
			}

			h.app.RequestExit()
		}
	}
}

func newApplication(verbose bool) *spectrum.Application {
	app := spectrum.NewApplication()
	app.Verbose = verbose
	env.Publish(app)
	return app
}

func newEmulationCore(app *spectrum.Application, acceleratedLoad bool) (*spectrum.Spectrum48k, error) {
	romPath, err := spectrum.SystemRomPath("48.rom")
	if err != nil {
		return nil, err
	}

	rom, err := spectrum.ReadROM(romPath)
	if err != nil {
		return nil, err
	}

	speccy := spectrum.NewSpectrum48k(app, *rom)
	if acceleratedLoad {
		speccy.TapeDrive().AcceleratedLoad = true
	}

	env.Publish(speccy)

	return speccy, nil
}

func ftpget_choice(app *spectrum.Application, matches []string, freeware []bool) (string, error) {
	switch len(matches) {
	case 0:
		return "", nil

	case 1:
		if freeware[0] {
			return matches[0], nil
		} else {
			// Not freeware - We want the user to make the choice
		}
	}

	app.PrintfMsg("")
	fmt.Printf("Select a number from the above list (press ENTER to exit GoSpeccy): ")
	in := bufio.NewReader(os.Stdin)

	input, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}

	id, err := strconv.Atoi(input)
	if err != nil {
		return "", err
	}
	if (id < 0) || (id >= len(matches)) {
		return "", errors.New("Invalid selection")
	}

	url := matches[id]
	if app.Verbose {
		app.PrintfMsg("You've selected %s", url)
	}
	return url, nil
}

func wait(app *spectrum.Application) {
	<-app.HasTerminated

	if app.Verbose {
		var memstats runtime.MemStats
		runtime.ReadMemStats(&memstats)
		app.PrintfMsg("GC: %d garbage collections, %s total pause time",
			memstats.NumGC, time.Nanosecond*time.Duration(memstats.PauseTotalNs))
	}

	// Stop host-CPU profiling
	if *cpuProfile != "" {
		pprof.StopCPUProfile() // flushes profile to disk
	}
}

func exit(app *spectrum.Application) {
	app.RequestExit()
	wait(app)
}

var (
	help            = flag.Bool("help", false, "Show usage")
	acceleratedLoad = flag.Bool("accelerated-load", false, "Accelerated tape loading")
	fps             = flag.Float64("fps", spectrum.DefaultFPS, "Frames per second")
	verbose         = flag.Bool("verbose", false, "Enable debugging messages")
	cpuProfile      = flag.String("hostcpu-profile", "", "Write host-CPU profile to the specified file (for 'pprof')")
	wos             = flag.String("wos", "", "Download from WorldOfSpectrum; you must provide a query regex (ex: -wos=jetsetwilly)")
)

func main() {
	var init_waitGroup sync.WaitGroup
	env.PublishName("init WaitGroup", &init_waitGroup)

	// Handle options

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ZX Spectrum 128k Emulator\n")
		fmt.Fprintf(os.Stderr, "Usage:\n\n")
		fmt.Fprintf(os.Stderr, "\tgospeccy [options] [image.sna]\n\n")
		fmt.Fprintf(os.Stderr, "Options are:\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *help == true {
		flag.Usage()
		return
	}

	app := newApplication(*verbose)

	// Use at least 2 OS threads.
	// This helps to prevent audio buffer underflows
	// in case rendering is consuming too much CPU.
	if (os.Getenv("GOMAXPROCS") == "") && (runtime.GOMAXPROCS(-1) < 2) {
		runtime.GOMAXPROCS(2)
	}
	if app.Verbose {
		app.PrintfMsg("using %d OS threads", runtime.GOMAXPROCS(-1))
	}

	// Install SIGTERM handler
	handler := handler_SIGTERM{app}
	spectrum.InstallSignalHandler(&handler)

	speccy, err := newEmulationCore(app, *acceleratedLoad)
	if err != nil {
		app.PrintfMsg("%s", err)
		exit(app)
		return
	}

	interpreter.Init(app, flag.Arg(0), speccy)

	if app.TerminationInProgress() || app.Terminated() {
		exit(app)
		return
	}

	// Optional: Read and categorize the contents
	//           of the file specified on the command-line
	var program_orNil interface{} = nil
	var programName string
	if flag.Arg(0) != "" {
		file := flag.Arg(0)
		programName = file

		var err error
		path, err := spectrum.ProgramPath(file)
		if err != nil {
			app.PrintfMsg("%s", err)
			exit(app)
			return
		}

		program_orNil, err = formats.ReadProgram(path)
		if err != nil {
			app.PrintfMsg("%s", err)
			exit(app)
			return
		}
	}

	// Wait until modules are initialized
	init_waitGroup.Wait()

	// Init SDL
	go sdl_output.Main()

	// Begin speccy emulation
	go speccy.EmulatorLoop()

	// Set the FPS
	speccy.CommandChannel <- spectrum.Cmd_SetFPS{float32(*fps), nil}

	// Optional: Load the program specified on the command-line
	if program_orNil != nil {
		program := program_orNil

		if _, isTAP := program.(*formats.TAP); isTAP {
			romLoaded := make(chan (<-chan bool))
			speccy.CommandChannel <- spectrum.Cmd_Reset{romLoaded}
			<-(<-romLoaded)
		}

		errChan := make(chan error)
		speccy.CommandChannel <- spectrum.Cmd_Load{programName, program, errChan}
		err := <-errChan
		if err != nil {
			app.PrintfMsg("%s", err)
			exit(app)
			return
		}
	}

	wait(app)
}
