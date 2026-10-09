package main

// epaper.go - driver for the Waveshare 7.5" e-paper (800x480, V2,
// black/white). SPI via /dev/spidev0.0 (ioctls, no bindings),
// GPIO via /sys/class/gpio. Init sequences and refresh logic follow
// the official Waveshare C demo (EPD_7in5_V2).

import (
	"fmt"
	"image"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// ---------- SPI via ioctl ----------

type spiIOCTransfer struct {
	txBuf       uint64
	rxBuf       uint64
	length      uint32
	speedHz     uint32
	delayUsecs  uint16
	bitsPerWord uint8
	csChange    uint8
	txNbits     uint8
	rxNbits     uint8
	pad         uint16
}

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | typ<<8 | nr | size<<16
}

var (
	spiIOCMessage1   = ioc(1, 'k', 0, unsafe.Sizeof(spiIOCTransfer{}))
	spiIOCWriteMode  = ioc(1, 'k', 1, 1)
	spiIOCWriteBits  = ioc(1, 'k', 3, 1)
	spiIOCWriteSpeed = ioc(1, 'k', 4, 4)
)

// ---------- GPIO ----------
// Legacy path: /sys/class/gpio (Pi 1-4, Zero).
// Modern path: /dev/gpiochipN character device, v1 line API (Pi 5,
// where sysfs export fails with EINVAL). openGPIO tries sysfs first
// and falls back to the gpiochip device automatically.

type gpio struct {
	value  *os.File // sysfs value file (legacy path)
	lineFd *os.File // gpiochip line handle
	v2     bool     // true when the v2 API is in use
}

func openGPIO(pin int, out bool, chip string) (*gpio, error) {
	g, err := openGPIOSysfs(pin, out)
	if err == nil {
		return g, nil
	}
	gc, errc := openGPIOChipV1(pin, out, chip)
	if errc == nil {
		return gc, nil
	}
	gv2, errv2 := openGPIOChipV2(pin, out, chip)
	if errv2 == nil {
		return gv2, nil
	}
	return nil, fmt.Errorf("GPIO %d: sysfs failed (%v), gpiochip v1 failed (%v), gpiochip v2 failed (%v)",
		pin, err, errc, errv2)
}

func openGPIOSysfs(pin int, out bool) (*gpio, error) {
	base := fmt.Sprintf("/sys/class/gpio/gpio%d", pin)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		if err := os.WriteFile("/sys/class/gpio/export",
			[]byte(fmt.Sprintf("%d", pin)), 0644); err != nil {
			return nil, fmt.Errorf("exporting GPIO %d: %w", pin, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	dir := "in"
	if out {
		dir = "out"
	}
	if err := os.WriteFile(base+"/direction", []byte(dir), 0644); err != nil {
		return nil, fmt.Errorf("setting GPIO %d direction: %w", pin, err)
	}
	vf, err := os.OpenFile(base+"/value", os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening GPIO %d: %w", pin, err)
	}
	return &gpio{value: vf}, nil
}

var (
	gpioGetLinehandleIOC = ioc(3, 0xB4, 3, unsafe.Sizeof(gpiohandleRequest{}))
	gpioGetLinevaluesIOC = ioc(3, 0xB4, 8, unsafe.Sizeof(gpiohandleData{}))
	gpioSetLinevaluesIOC = ioc(3, 0xB4, 9, unsafe.Sizeof(gpiohandleData{}))
)

type gpiohandleRequest struct {
	lineOffsets   [64]uint32
	flags         uint32
	defaultValues [64]uint8
	consumerLabel [32]uint8
	lines         uint32
	fd            int32
}

type gpiohandleData struct {
	values [64]uint8
}

// ---------- GPIO character device, v2 API ----------
// Required on Raspberry Pi 5 with kernel >= 6.12, where the v1
// line-handle API was removed.

var (
	gpioV2GetLineIOC       = ioc(3, 0xB4, 7, unsafe.Sizeof(gpioV2LineRequest{}))
	gpioV2LineGetValuesIOC = ioc(3, 0xB4, 0x0e, unsafe.Sizeof(gpioV2LineValues{}))
	gpioV2LineSetValuesIOC = ioc(3, 0xB4, 0x0f, unsafe.Sizeof(gpioV2LineValues{}))
)

const (
	gpioV2FlagInput  = 1 << 2 // GPIO_V2_LINE_FLAG_INPUT
	gpioV2FlagOutput = 1 << 3 // GPIO_V2_LINE_FLAG_OUTPUT
)

// Layout exactly as in include/uapi/linux/gpio.h (kernel >= 5.10).
type gpioV2LineValues struct {
	bits uint64
	mask uint64
}

type gpioV2LineAttribute struct {
	id      uint32
	padding uint32
	value   uint64 // flags | values | debounce_period_us
}

type gpioV2LineConfigAttribute struct {
	attr gpioV2LineAttribute // 16 bytes
	mask uint64
}

type gpioV2LineConfig struct {
	flags    uint64 // GPIO_V2_LINE_FLAG_*
	numAttrs uint32
	padding  [5]uint32
	attrs    [4]gpioV2LineConfigAttribute
}

type gpioV2LineRequest struct {
	offsets         [64]uint32
	consumer        [32]byte
	config          gpioV2LineConfig
	numLines        uint32
	eventBufferSize uint32
	padding         [5]uint32
	fd              int32
}

func openGPIOChipV2(pin int, out bool, chip string) (*gpio, error) {
	cf, err := os.OpenFile(chip, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", chip, err)
	}
	defer cf.Close()
	var req gpioV2LineRequest
	req.offsets[0] = uint32(pin)
	req.numLines = 1
	copy(req.consumer[:], "fooplay")
	if out {
		req.config.flags = gpioV2FlagOutput
	} else {
		req.config.flags = gpioV2FlagInput
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, cf.Fd(),
		gpioV2GetLineIOC, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return nil, fmt.Errorf("GPIO_V2_GET_LINE: %v", errno)
	}
	return &gpio{
		lineFd: os.NewFile(uintptr(req.fd), fmt.Sprintf("gpio%d", pin)),
		v2:     true,
	}, nil
}

func openGPIOChipV1(pin int, out bool, chip string) (*gpio, error) {
	cf, err := os.OpenFile(chip, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", chip, err)
	}
	defer cf.Close()
	var req gpiohandleRequest
	req.lineOffsets[0] = uint32(pin)
	req.lines = 1
	copy(req.consumerLabel[:], "fooplay")
	if out {
		req.flags = 2 // GPIOHANDLE_REQUEST_OUTPUT
	} else {
		req.flags = 1 // GPIOHANDLE_REQUEST_INPUT
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, cf.Fd(),
		gpioGetLinehandleIOC, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return nil, fmt.Errorf("GPIO_GET_LINEHANDLE: %v", errno)
	}
	return &gpio{lineFd: os.NewFile(uintptr(req.fd), fmt.Sprintf("gpio%d", pin))}, nil
}

func (g *gpio) Set(v bool) error {
	if g.v2 {
		var d gpioV2LineValues
		d.mask = 1
		if v {
			d.bits = 1
		}
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.lineFd.Fd(),
			gpioV2LineSetValuesIOC, uintptr(unsafe.Pointer(&d)))
		if errno != 0 {
			return fmt.Errorf("GPIO_V2_LINE_SET_VALUES: %v", errno)
		}
		return nil
	}
	if g.lineFd != nil {
		var d gpiohandleData
		if v {
			d.values[0] = 1
		}
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.lineFd.Fd(),
			gpioSetLinevaluesIOC, uintptr(unsafe.Pointer(&d)))
		if errno != 0 {
			return fmt.Errorf("GPIOHANDLE_SET_LINE_VALUES: %v", errno)
		}
		return nil
	}
	g.value.Seek(0, 0)
	b := []byte{'0'}
	if v {
		b[0] = '1'
	}
	_, err := g.value.Write(b)
	return err
}

func (g *gpio) Get() (bool, error) {
	if g.v2 {
		var d gpioV2LineValues
		d.mask = 1
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.lineFd.Fd(),
			gpioV2LineGetValuesIOC, uintptr(unsafe.Pointer(&d)))
		if errno != 0 {
			return false, fmt.Errorf("GPIO_V2_LINE_GET_VALUES: %v", errno)
		}
		return d.bits&1 != 0, nil
	}
	if g.lineFd != nil {
		var d gpiohandleData
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.lineFd.Fd(),
			gpioGetLinevaluesIOC, uintptr(unsafe.Pointer(&d)))
		if errno != 0 {
			return false, fmt.Errorf("GPIOHANDLE_GET_LINE_VALUES: %v", errno)
		}
		return d.values[0] != 0, nil
	}
	g.value.Seek(0, 0)
	buf := make([]byte, 1)
	if _, err := g.value.Read(buf); err != nil {
		return false, err
	}
	return buf[0] == '1', nil
}

func (g *gpio) Close() {
	if g.lineFd != nil {
		g.lineFd.Close()
		return
	}
	g.value.Close()
}

// ---------- e-paper ----------

type EPaper struct {
	spi          *os.File
	rst, dc      *gpio
	busy         *gpio
	pwr          *gpio // panel power enable (MOSFET on newer HATs), nil if unused
	powered      bool
	sleepBetween bool
	lastRefresh  time.Time
	asleep       bool
}

// pinPWR <= 0 disables explicit panel power control (older HATs that
// power the panel directly).
func OpenEPaper(spiDev, chip string, pinRST, pinDC, pinBUSY, pinPWR int, sleep bool) (*EPaper, error) {
	spi, err := os.OpenFile(spiDev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening SPI device %s (SPI enabled in raspi-config?): %w", spiDev, err)
	}
	mode := uint8(0) // SPI mode 0
	syscall.Syscall(syscall.SYS_IOCTL, spi.Fd(), spiIOCWriteMode, uintptr(unsafe.Pointer(&mode)))
	bits := uint8(8)
	syscall.Syscall(syscall.SYS_IOCTL, spi.Fd(), spiIOCWriteBits, uintptr(unsafe.Pointer(&bits)))
	speed := uint32(4000000)
	syscall.Syscall(syscall.SYS_IOCTL, spi.Fd(), spiIOCWriteSpeed, uintptr(unsafe.Pointer(&speed)))

	rst, err := openGPIO(pinRST, true, chip)
	if err != nil {
		return nil, err
	}
	dc, err := openGPIO(pinDC, true, chip)
	if err != nil {
		return nil, err
	}
	busy, err := openGPIO(pinBUSY, false, chip)
	if err != nil {
		return nil, err
	}
	var pwr *gpio
	if pinPWR > 0 {
		pwr, err = openGPIO(pinPWR, true, chip)
		if err != nil {
			return nil, err
		}
		if err := pwr.Set(true); err != nil { // panel power on
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}

	e := &EPaper{
		spi:          spi,
		rst:          rst,
		dc:           dc,
		busy:         busy,
		pwr:          pwr,
		powered:      pwr != nil,
		sleepBetween: sleep,
	}
	e.reset()
	if err := e.clear(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *EPaper) Name() string        { return "Waveshare 7.5 e-paper (800x480)" }
func (e *EPaper) Bounds() (int, int)  { return 800, 480 }

func (e *EPaper) transfer(tx []byte) error {
	t := spiIOCTransfer{
		txBuf:       uint64(uintptr(unsafe.Pointer(&tx[0]))),
		length:      uint32(len(tx)),
		speedHz:     4000000,
		bitsPerWord: 8,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, e.spi.Fd(),
		spiIOCMessage1, uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return fmt.Errorf("SPI transfer: %v", errno)
	}
	return nil
}

func (e *EPaper) cmd(c byte) error {
	e.dc.Set(false)
	return e.transfer([]byte{c})
}

// data sends each byte as its own SPI transaction (CS toggles per
// byte), exactly like the official Waveshare examples - some panel
// controllers rely on the CS edges.
func (e *EPaper) data(d ...byte) error {
	if err := e.dc.Set(true); err != nil {
		return err
	}
	for _, b := range d {
		if err := e.transfer([]byte{b}); err != nil {
			return err
		}
	}
	return nil
}

func (e *EPaper) reset() {
	e.rst.Set(true)
	time.Sleep(200 * time.Millisecond)
	e.rst.Set(false)
	time.Sleep(2 * time.Millisecond)
	e.rst.Set(true)
	time.Sleep(200 * time.Millisecond)
}

// waitIdle waits until BUSY goes HIGH (panel finished). Some panel
// revisions only update their status register - and therefore the BUSY
// pin - when the status command 0x71 is sent, so it is issued before
// every poll (as in the official Waveshare examples).
func (e *EPaper) waitIdle() error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		if err := e.cmd(0x71); err != nil {
			return err
		}
		v, err := e.busy.Get()
		if err != nil {
			return err
		}
		if v {
			time.Sleep(5 * time.Millisecond)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("e-paper busy timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// initFull: full-image init (best contrast, slow refresh).
func (e *EPaper) initFull() error {
	e.reset()
	e.cmd(0x06) // booster soft start (required on older panel revisions)
	e.data(0x17, 0x17, 0x28, 0x17)
	e.cmd(0x01) // POWER SETTING
	e.data(0x07, 0x07, 0x28, 0x17) // VGH=20V, VGL=-20V, VDH=15V, VDL=-15V
	e.cmd(0x04) // POWER ON
	time.Sleep(100 * time.Millisecond)
	if err := e.waitIdle(); err != nil {
		return err
	}
	e.cmd(0x00) // PANEL SETTING
	e.data(0x1F)
	e.cmd(0x61) // TRES 800x480
	e.data(0x03, 0x20, 0x01, 0xE0)
	e.cmd(0x15)
	e.data(0x00)
	e.cmd(0x50)
	e.data(0x10, 0x07)
	e.cmd(0x60) // TCON
	e.data(0x22)
	return nil
}

func (e *EPaper) refresh() error {
	e.cmd(0x12) // DISPLAY REFRESH
	time.Sleep(100 * time.Millisecond)
	return e.waitIdle()
}

func (e *EPaper) clear() error {
	if err := e.initFull(); err != nil {
		return err
	}
	white := make([]byte, 800/8)
	for i := range white {
		white[i] = 0xFF
	}
	e.cmd(0x10)
	for i := 0; i < 480; i++ {
		if err := e.data(white...); err != nil {
			return err
		}
	}
	e.cmd(0x13)
	black := make([]byte, 800/8) // already 0x00
	for i := 0; i < 480; i++ {
		if err := e.data(black...); err != nil {
			return err
		}
	}
	return e.refresh()
}

// Sleep puts the panel into deep sleep (low power consumption).
// Repeated calls are no-ops; waking happens automatically with the next
// display() call (its init performs the required reset + power-on).
func (e *EPaper) Sleep() error {
	if e.asleep {
		return nil
	}
	e.asleep = true
	e.cmd(0x50)
	e.data(0xF7)
	e.cmd(0x02) // POWER OFF
	if err := e.waitIdle(); err != nil {
		return err
	}
	e.cmd(0x07) // DEEP SLEEP
	e.data(0xA5)
	return nil
}

// display sends the 48000-byte framebuffer (1 bit/pixel, 1 = white)
// and starts the refresh. fast=true uses the fast refresh mode.
func (e *EPaper) display(buf []byte) error {
	// Re-apply panel power if it was cut by PowerOff().
	if e.pwr != nil && !e.powered {
		if err := e.pwr.Set(true); err != nil {
			return err
		}
		e.powered = true
		time.Sleep(100 * time.Millisecond)
	}
	e.asleep = false // init performs reset + power-on, which also wakes the panel
	e.lastRefresh = time.Now()
	if err := e.initFull(); err != nil {
		return err
	}
	e.cmd(0x10) // old data (old RAM)
	if err := e.data(buf...); err != nil {
		return err
	}
	e.cmd(0x13) // new data (new RAM, inverted)
	inv := make([]byte, len(buf))
	for i := range buf {
		inv[i] = ^buf[i]
	}
	if err := e.data(inv...); err != nil {
		return err
	}
	if err := e.refresh(); err != nil {
		return err
	}
	if e.sleepBetween {
		return e.Sleep()
	}
	return nil
}

func (e *EPaper) Push(img *image.RGBA, dither image.Rectangle, structuralChange bool) error {
	// The screen content only ever changes on a track change; every
	// refresh is a full refresh (no progress updates, no fast refresh).
	if !structuralChange {
		return nil
	}
	return e.display(toEPaper(img, dither))
}

// PowerOff cuts the panel supply via the PWR pin (complete shutdown).
// Without a PWR pin it falls back to deep sleep.
func (e *EPaper) PowerOff() error {
	e.powered = false
	e.asleep = true
	if e.pwr == nil {
		return e.Sleep()
	}
	return e.pwr.Set(false)
}

func (e *EPaper) Close() {
	e.PowerOff()
	e.spi.Close()
	e.rst.Close()
	e.dc.Close()
	e.busy.Close()
	if e.pwr != nil {
		e.pwr.Close()
	}
}

// bayer4 is a 4x4 ordered dithering matrix: gray values of the cover
// are distributed across black/white instead of a hard threshold.
var bayer4 = [4][4]uint8{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

// toEPaper converts an RGBA image to the 1-bit panel format
// (MSB = left pixel, 1 = white). Inside ditherRect (the cover) ordered
// Bayer dithering preserves gray tones; everywhere else a plain
// threshold renders text and shapes razor sharp without fringing.
func toEPaper(img *image.RGBA, dither image.Rectangle) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	buf := make([]byte, w*h/8)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lum := (r*299 + g*587 + b*114) / 256000 // 8-bit luminance
			white := false
			if (image.Point{X: x, Y: y}).In(dither) {
				th := uint32(bayer4[y&3][x&3])*16 + 8
				white = lum > th
			} else {
				white = lum > 165 // very thin strokes; near the practical limit
			}
			if white {
				buf[y*(w/8)+x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return buf
}
