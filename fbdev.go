package main

// fbdev.go - output to LCD screens via the Linux framebuffer (/dev/fb0).
// Works on Raspberry Pi OS Lite without any desktop environment or X11.
// Supports 16/24/32 bpp.

import (
	"fmt"
	"image"
	"os"
	"syscall"
	"unsafe"
)

type fbBitfield struct {
	offset, length, msbRight uint32
}

// fb_var_screeninfo (160 bytes, architecture independent)
type fbVarScreenInfo struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset uint32
	BitsPerPixel, Grayscale                              uint32
	Red, Green, Blue, Transp                             fbBitfield
	Nonstd, Activate, Height, Width, AccelFlags          uint32
	Pixclock, LeftMargin, RightMargin                    uint32
	UpperMargin, LowerMargin                             uint32
	HsyncLen, VsyncLen, Sync, Vmode, Rotate              uint32
	Colorspace                                           uint32
	Reserved                                             [4]uint32
}

const (
	fbIOGetVScreenInfo = 0x4600 // _IO('F', 0)
	fbIOBlank          = 0x4611 // _IO('F', 0x11)
	fbBlankUnblank     = 0     // FB_BLANK_UNBLANK
	fbBlankPowerdown   = 4     // FB_BLANK_POWERDOWN
)

type FBDev struct {
	f     *os.File
	mem   []byte
	v     fbVarScreenInfo
	blank bool
}

func OpenFBDev(dev string) (*FBDev, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening framebuffer %s: %w (console framebuffer enabled in boot config?)", dev, err)
	}
	var v fbVarScreenInfo
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		fbIOGetVScreenInfo, uintptr(unsafe.Pointer(&v)))
	if errno != 0 {
		f.Close()
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %v", errno)
	}
	if v.BitsPerPixel != 16 && v.BitsPerPixel != 24 && v.BitsPerPixel != 32 {
		f.Close()
		return nil, fmt.Errorf("unsupported bit depth: %d", v.BitsPerPixel)
	}
	// Disable the blinking console cursor and clear the console
	// (best effort; may need root). Errors are ignored.
	_ = os.WriteFile("/sys/class/graphics/fbcon/cursor_blink", []byte("0"), 0644)
	if tty, err := os.OpenFile("/dev/tty1", os.O_WRONLY, 0); err == nil {
		tty.WriteString("\033[2J\033[?25l") // clear screen, hide cursor
		tty.Close()
	}

	size := int(v.XresVirtual * v.YresVirtual * v.BitsPerPixel / 8)
	mem, err := syscall.Mmap(int(f.Fd()), 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("framebuffer mmap: %w", err)
	}
	return &FBDev{f: f, mem: mem, v: v}, nil
}

func (fb *FBDev) Name() string {
	return fmt.Sprintf("framebuffer %dx%d@%dbpp", fb.v.Xres, fb.v.Yres, fb.v.BitsPerPixel)
}

func (fb *FBDev) Bounds() (int, int) { return int(fb.v.Xres), int(fb.v.Yres) }

func packColor(v uint32, bf fbBitfield) uint32 {
	v >>= 8 // 16-bit channel -> 8-bit
	return (v * ((1 << bf.length) - 1) / 255) << bf.offset
}

func (fb *FBDev) Push(img *image.RGBA, _ image.Rectangle, _ bool) error {
	if err := fb.unblank(); err != nil {
		return err
	}
	w := int(fb.v.Xres)
	h := int(fb.v.Yres)
	bpp := int(fb.v.BitsPerPixel / 8)
	vw := int(fb.v.XresVirtual)
	for y := 0; y < h; y++ {
		rowBase := y * vw * bpp
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			px := packColor(r, fb.v.Red) |
				packColor(g, fb.v.Green) |
				packColor(b, fb.v.Blue)
			o := rowBase + x*bpp
			for i := 0; i < bpp; i++ {
				fb.mem[o+i] = byte(px >> uint(8*i))
			}
		}
	}
	return nil
}

// Sleep is a no-op for LCDs (the e-paper powers down instead).
func (fb *FBDev) Sleep() error { return nil }

// PowerOff blanks the screen (VESA powerdown); Push() unblanks it again.
func (fb *FBDev) PowerOff() error {
	fb.blank = true
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fb.f.Fd(),
		fbIOBlank, uintptr(fbBlankPowerdown))
	if errno != 0 {
		return fmt.Errorf("FBIOBLANK powerdown: %v", errno)
	}
	return nil
}

func (fb *FBDev) unblank() error {
	if !fb.blank {
		return nil
	}
	fb.blank = false
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fb.f.Fd(),
		fbIOBlank, uintptr(fbBlankUnblank))
	if errno != 0 {
		return fmt.Errorf("FBIOBLANK unblank: %v", errno)
	}
	return nil
}

func (fb *FBDev) Close() {
	syscall.Munmap(fb.mem)
	fb.f.Close()
}
