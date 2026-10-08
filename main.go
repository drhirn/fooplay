package main

// fooplay - displays cover art and track information for the song
// currently playing in foobar2000, served by the beefweb plugin on a
// remote machine. Runs without a desktop environment, driving a
// Waveshare 7.5" e-paper display (800x480, SPI) or an LCD through the
// Linux framebuffer /dev/fb0 directly.
// The console stays completely silent; only errors and warnings are
// written to a log file (see -logfile).

import (
	"flag"
	"image"
	"io"
	"log"
	"os"
	"time"
)

// Display is the output abstraction.
type Display interface {
	Name() string
	Bounds() (int, int)
	// Push hands over a rendered frame. structuralChange = true means
	// track/state changed -> e-paper performs a full refresh.
	Push(img *image.RGBA, dither image.Rectangle, structuralChange bool) error
	// Sleep powers the display down (e-paper deep sleep; no-op on LCD).
	Sleep() error
	// PowerOff switches the display off entirely (e-paper panel power
	// cut via PWR pin; LCD blanking).
	PowerOff() error
	Close()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func main() {
	addr := flag.String("addr", "http://10.168.1.1:8880", "beefweb base URL (e.g. http://10.168.1.1:8880)")
	displayFlag := flag.String("display", "auto", "display type: auto|epaper|fb|both")
	poll := flag.Duration("poll", 2*time.Second, "polling interval for player data")
	eSleep := flag.Bool("epaper-sleep", false, "put the e-paper into deep sleep between updates")
	invert := flag.Bool("invert", false, "dark theme (default: white background)")
	spiDev := flag.String("spi", "/dev/spidev0.0", "SPI device for the e-paper")
	gpioChip := flag.String("gpiochip", "/dev/gpiochip0", "GPIO character device (Pi 5 / fallback when sysfs GPIO is unavailable)")
	fbDev := flag.String("fb", "/dev/fb0", "framebuffer device for LCD")
	pinRST := flag.Int("pin-rst", 17, "GPIO pin: RST")
	pinDC := flag.Int("pin-dc", 25, "GPIO pin: DC")
	pinBUSY := flag.Int("pin-busy", 24, "GPIO pin: BUSY")
	pinPWR := flag.Int("pin-pwr", 18, "GPIO pin: panel power enable (0 = disabled, for older HATs)")
	logFile := flag.String("logfile", "fooplay.log", "log file for errors/warnings (empty = no logging)")
	debug := flag.Bool("debug", false, "log every display decision (diagnostics)")
	flag.Parse()

	// Console stays completely silent; only errors and warnings are
	// logged.
	if *logFile == "" {
		log.SetOutput(io.Discard)
	} else {
		lf, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("opening log file %s: %v", *logFile, err)
		}
		log.SetOutput(lf)
		defer lf.Close()
	}

	var displays []Display
	openEPaper := func() (Display, error) {
		return OpenEPaper(*spiDev, *gpioChip, *pinRST, *pinDC, *pinBUSY, *pinPWR, *eSleep)
	}
	add := func(d Display, err error) {
		if err != nil {
			log.Printf("WARN display unavailable: %v", err)
			return
		}
		displays = append(displays, d)
	}
	switch *displayFlag {
	case "epaper":
		add(openEPaper())
	case "fb":
		add(OpenFBDev(*fbDev))
	case "both":
		// Drive e-paper and LCD simultaneously.
		add(openEPaper())
		add(OpenFBDev(*fbDev))
	case "auto":
		if fileExists(*spiDev) {
			// /dev/spidev0.0 exists as soon as SPI is enabled, even
			// without a panel attached - so try the e-paper and fall
			// back to the LCD if no panel responds.
			d, err := openEPaper()
			if err != nil {
				log.Printf("WARN e-paper unavailable (%v), falling back to LCD", err)
				add(OpenFBDev(*fbDev))
			} else {
				displays = append(displays, d)
			}
		} else {
			add(OpenFBDev(*fbDev))
		}
	default:
		log.Fatalf("unknown display type: %q", *displayFlag)
	}
	if len(displays) == 0 {
		log.Fatal("no display available")
	}
	for i := range displays {
		defer displays[i].Close()
	}
	// All displays are assumed to share the same resolution (or the
	// first one's); the layout scales relative to height anyway.
	w, h := displays[0].Bounds()

	client := NewBeefweb(*addr)
	lastKey := ""
	lastCoverKey := ""
	lastTrackShown := ""
	lastState := ""
	var cover image.Image
	tick := time.NewTicker(*poll)
	defer tick.Stop()

	for {
		vm := client.Fetch()
		key := vm.Key()
		stateChanged := vm.State != lastState

		if vm.State == "playing" {
			// Reload the cover only on track change (TrackKey is
			// independent of volume/mode, so it is not refetched
			// constantly).
			tk := vm.TrackKey()
			if tk != lastCoverKey {
				lastCoverKey = tk
				cover = nil
				if c, ok := client.FetchCover(); ok {
					cover = c
				} else {
					log.Printf("WARN no cover for %s - %s (404 or error)", vm.Artist, vm.Title)
				}
			}
			vm.Cover = cover

			// Refresh only on track change (or when coming back from
			// another state, e.g. stop -> play).
			structural := tk != lastTrackShown || stateChanged
			if *debug {
				log.Printf("DEBUG push playing trackChanged=%v stateChanged=%v (%s)", tk != lastTrackShown, stateChanged, vm.State)
			}
			img, ditherRect := render(vm, w, h, *invert)
			for _, d := range displays {
				if err := d.Push(img, ditherRect, structural); err != nil {
					log.Printf("ERROR display: %v", err)
				}
			}
			lastTrackShown = tk
		} else if stateChanged {
			switch vm.State {
			case "paused":
				// Displays off completely.
				for _, d := range displays {
					if err := d.PowerOff(); err != nil {
						log.Printf("ERROR display power off: %v", err)
					}
				}
			default:
				// Stopped or connection error: first a blank screen in
				// the background color (white by default, black with
				// -invert), then power off completely.
				if *debug {
					log.Printf("DEBUG blank+off state=%s", vm.State)
				}
				img, _ := render(vm, w, h, *invert)
				for _, d := range displays {
					if err := d.Push(img, image.Rectangle{}, true); err != nil {
						log.Printf("ERROR display: %v", err)
					}
					if err := d.PowerOff(); err != nil {
						log.Printf("ERROR display power off: %v", err)
					}
				}
			}
		}
		if vm.State == "error" && key != lastKey {
			log.Printf("ERROR beefweb: %s", vm.ErrText)
		}
		lastState = vm.State
		lastKey = key
		<-tick.C
	}
}
