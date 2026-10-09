# fooplay

Shows the track currently playing in foobar2000 on a Raspberry Pi
**without a desktop environment** (Raspberry Pi OS Lite).

Data source is the foobar2000 plugin **beefweb** on a remote machine
(default: `http://10.168.1.1:8880`). Output goes either to:

* **Waveshare 7.5" e-paper (800x480, black/white)** - directly via SPI/GPIO
* **regular LCD screens** - via the Linux framebuffer `/dev/fb0`
  (HDMI or official DSI touchscreen, no X11 needed)

Only dependency: golang.org/x/image (embedded Go font).
Run `go mod tidy` once before building (needs internet).

## AI

This tool was completely written by AI (Kimi). Even the README.md – except this passage. And even this was corrected by AI.

I always wanted a display showing the song currently playing in foobar2000 while I'm gaming or otherwise unable to look at the foobar2000 window. But I suffered from a complete lack of skills. And although I'm not a friend of AI, I had the feeling that someday I would have to try it out. And that's where fooplay comes in.

It was a weird experience, and I still don't know what to think about AI – especially about outsourcing (programming) work to it. Nevertheless, after days of work I got this little tool running, which I'm happy about. But it's not the "yeah, I did this by myself" feeling I always had in pre-AI times, when I was forced to think things through and actually learn something.

## What is shown

Album cover (served by beefweb, otherwise a vinyl placeholder),
frameless on the left, with artist, album and year stacked to its
right; the song title spans the full width below (long titles wrap onto
up to two lines). Font sizes are relative to the screen height (title
~15 % of H, artist ~10 %), so the layout works on 800x480 as well as on
Full-HD. There is deliberately no progress bar and no playback icon:
e-paper refreshes are slow and wear the panel.

The screen only changes when the track changes. Display states:

| foobar2000 state | display behavior |
|---|---|
| playing | song screen; one full refresh per track change |
| paused | switched off completely (panel supply cut / LCD blanking) |
| stopped / connection error | blank screen in background color, then off |

Everything turns back on automatically when playback resumes.

**Theme:** light by default (white background, black text). Pass
`-invert` for a dark theme (black background, white text).

**Console:** completely silent. Only errors and warnings are logged
(connection problems to beefweb, missing covers (WARN), display errors
(ERROR)). Add `-debug` to log every display decision.

**E-paper conversion:** the cover is converted with ordered Bayer
dithering (preserves gray tones); text and shapes use a hard threshold
at luminance 127 followed by a 1-pixel dilation - razor sharp and bold
without gray-halo fringing.

## Wiring e-paper (Waveshare 7.5" HAT on Raspberry Pi)

The plug-on HAT version stacks directly onto the 40-pin GPIO header
(no jumper wires needed). Signal mapping used by fooplay:

| Signal | GPIO | Phys. pin |
|--------|------|-----------|
| VCC    | 3V3  | 1 |
| GND    | GND  | 6 |
| DIN    | MOSI (GPIO10) | 19 |
| CLK    | SCLK (GPIO11) | 23 |
| CS     | CE0 (GPIO8)   | 24 |
| DC     | GPIO25 | 22 |
| RST    | GPIO17 | 11 |
| BUSY   | GPIO24 | 18 |
| PWR EN | GPIO18 | 12 |

Notes:
* **PWR EN** (GPIO18) switches the panel supply via a MOSFET on newer
  HATs. fooplay drives it high at startup and low on shutdown; on HATs
  without power control use `-pin-pwr 0`.
* HAT switches (if present): *Display Config* = B (7.5"), *Interface
  Config* = 0 (4-line SPI).
* All pins are configurable: `-pin-rst`, `-pin-dc`, `-pin-busy`, `-pin-pwr`.

**GPIO on different kernels:** fooplay tries three paths automatically:
sysfs `/sys/class/gpio` (Pi 1-4 with older kernels), gpiochip **v1**
line API (Pi 5, older kernels), gpiochip **v2** API (current kernels
>= 6.12 where v1 was removed). Character device: `-gpiochip`
(default `/dev/gpiochip0`).

## E-paper driver details

* SPI via `/dev/spidev0.0` (ioctls, mode 0, 4 MHz, no bindings).
* Every byte is sent as its own SPI transaction (CS toggles per byte),
  matching the official Waveshare examples.
* Init sequence (booster soft start, power setting VDH/VDL = 0x28/0x17,
  power on, resolution, VCOM, TCON) matches the Waveshare epd7in5_V2
  reference - required for older 800x480 panel revisions.
* **Busy polling sends command 0x71 (status read) before every read** -
  without it, older panel revisions never raise the BUSY pin.
* Every refresh is a full refresh; there are no periodic/progress
  refreshes at all.

## Preparing the Pi

Enable SPI:

    sudo raspi-config   # Interface Options -> SPI -> Yes
    sudo reboot

In LCD mode (fb), `/dev/fb0` appears automatically once a screen is
attached. Console text will be in the way; optionally:

    sudo systemctl disable --now getty@tty1

fooplay clears the console and hides the blinking cursor on startup
(best effort, root required).

## Build

On the Pi:

    sudo apt install golang-go   # or current Go from go.dev
    cd fooplay
    go mod tidy                  # downloads golang.org/x/image (once)
    go build -o fooplay .

Cross-compile on a PC:

    GOOS=linux GOARCH=arm64 go build -o fooplay .   # 64-bit Pi
    GOOS=linux GOARCH=arm  go build -o fooplay .   # 32-bit Pi

## Run

    ./fooplay                          # auto: e-paper, else LCD
    ./fooplay -display epaper
    ./fooplay -display fb -invert      # LCD with dark theme
    ./fooplay -display both            # e-paper AND LCD simultaneously

## As a service (systemd)

`/etc/systemd/system/fooplay.service`:

    [Unit]
    Description=fooplay display
    After=network-online.target

    [Service]
    ExecStart=/home/pi/fooplay/fooplay -addr http://10.168.1.1:8880 -logfile /var/log/fooplay.log
    WorkingDirectory=/home/pi/fooplay
    Restart=always
    RestartSec=5

    [Install]
    WantedBy=multi-user.target

Then:

    sudo systemctl daemon-reload
    sudo systemctl enable --now fooplay

## Log rotation

`/etc/logrotate.d/fooplay`:

    /home/pi/fooplay/fooplay.log {
        weekly
        rotate 4
        compress
        delaycompress
        missingok
        notifempty
        copytruncate
        maxsize 10M
    }

(`copytruncate` is required: fooplay keeps the log file open.)

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `http://10.168.1.1:8880` | beefweb base URL |
| `-display` | `auto` | `auto`, `epaper`, `fb` or `both` (e-paper + LCD at the same time) |
| `-poll` | `2s` | polling interval for player data |
| `-epaper-sleep` | off | deep sleep the e-paper between refreshes |
| `-invert` | off | dark theme (default is light/white) |
| `-spi` | `/dev/spidev0.0` | SPI device |
| `-gpiochip` | `/dev/gpiochip0` | GPIO character device |
| `-fb` | `/dev/fb0` | framebuffer device |
| `-pin-rst` / `-pin-dc` / `-pin-busy` / `-pin-pwr` | 17 / 25 / 24 / 18 | GPIO mapping (0 disables PWR) |
| `-logfile` | `fooplay.log` | log file for errors/warnings (empty = none) |
| `-debug` | off | log every display decision |

## Files

| File | Content |
|------|---------|
| `main.go` | flags, main loop, display selection, state machine, logging |
| `beefweb.go` | REST client (`/api/query`, `/api/artwork/current`) |
| `render.go` | layout, cover, text block, theme |
| `font.go` | text engine (DejaVu Sans Bold), wrapping, fitting |
| `DejaVuSans-Bold.ttf` | embedded font (Bitstream Vera/DejaVu license, see below) |
| `epaper.go` | e-paper driver (SPI, GPIO sysfs/v1/v2, panel init) |
| `fbdev.go` | LCD driver (framebuffer, blanking) |

The queried metadata columns are defined in `beefweb.go`
(`%artist%,%title%,%album%,%date%`) and can be extended there.

## Regeneration prompt

Everything above can be reproduced with a single prompt:

```text
Write a Go program `fooplay` for Raspberry Pi OS Lite (headless, no
desktop), standard library plus golang.org/x/image only. It polls the
REST API of the foobar2000 plugin beefweb at http://10.168.1.1:8880
every 2 s (GET /api/query?player=true&trcolumns=%artist%,%title%,%album%,%date%)
and shows the now-playing info directly on a display, all via CLI flags.

Display abstraction (interface Display with Push(img, ditherRect,
structuralChange)) with two backends; in `-display auto` try the
e-paper first and fall back to the LCD with a logged warning if no
panel responds (spidev0.0 exists as soon as SPI is enabled):

1. Waveshare 7.5" e-paper 800x480:
   - SPI via /dev/spidev0.0 using raw ioctls (SPI_IOC_MESSAGE computed
     from unsafe.Sizeof, mode 0, 4 MHz). Send EVERY byte as its own SPI
     transaction (CS toggles per byte), like the official examples.
   - GPIO with three automatic fallbacks: /sys/class/gpio (legacy),
     /dev/gpiochip0 v1 line API (GPIO_GET_LINEHANDLE etc.), v2 API
     (GPIO_V2_GET_LINE nr 7, GPIO_V2_LINE_GET/SET_VALUES nr 14/15 -
     ioctl dir is _IOWR=3, magic 0xB4; struct gpio_v2_line_request is
     448 bytes: offsets[64]u32, consumer[32]byte, config{flags u64,
     num_attrs u32, padding[5]u32, attrs[4]{id u32,pad u32,val u64,
     mask u64}}, num_lines u32, event_buffer_size u32, padding[5]u32,
     fd i32). Pins: RST=17, DC=25, BUSY=24, PWR-EN=18 (drive high at
     startup, low on shutdown; panel is dead without it on newer HATs).
   - Panel init per Waveshare epd7in5_V2 reference (older panel
     revisions): reset 200/2/200 ms, cmd 0x06 data 17 17 28 17, cmd 0x01
     data 07 07 28 17 (VDH=15V, VDL=-15V; NOT 3f 3f), cmd 0x04 POWER ON,
     100 ms, then cmd 0x61 (03 20 01 E0), 0x15 (00), 0x50 (10 07),
     0x60 (22), 0x00 (1F). CRITICAL: poll BUSY only after sending status
     command 0x71 before each read - without it the BUSY pin never
     changes on older panels.
   - Full refresh ONLY: cmd 0x10 + framebuffer, cmd 0x13 + inverted
     framebuffer, cmd 0x12, wait busy. No fast refresh, no periodic
     refreshes, ever.
   - 1-bit conversion: ordered 4x4 Bayer dithering INSIDE the cover
     rectangle (passed as ditherRect), hard threshold (luminance > 100)
     everywhere else, so text is razor sharp.

2. LCD via /dev/fb0: read fb_var_screeninfo (FBIOGET_VSCREENINFO),
   support 16/24/32 bpp via the RGB bitfields, mmap, write pixels;
   disable blinking console cursor on startup (cursor_blink=0 and
   ESC[2J ESC[?25l to /dev/tty1); PowerOff() = FBIOBLANK
   FB_BLANK_POWERDOWN, Push unblanks.

Rendering (light theme by default: white background, black text;
-invert for dark): embedded Go font (gofont/goregular via x/image,
bold simulated by overdrawing), all sizes relative to screen height.
Layout: cover (from GET /api/artwork/current, cached per track,
center-cropped to square, vinyl placeholder otherwise) frameless on the
left; artist, album, year stacked to its right (artist bold);
song title spans the full width below, wrapping onto up to 2 lines.
No progress bar, no status icon.

State machine: while playing, render+Push with structuralChange only on
track change (or state change); cover fetched only on track change. On
pause: PowerOff(). On stop/connection error: render full background
color, Push, then PowerOff(). Everything wakes automatically on the
next Push.

Logging: console completely silent; errors/warnings only to -logfile
(default fooplay.log, empty = none); optional -debug logs every push
decision.

Deliver clean, commented English files (main.go, beefweb.go, render.go,
font.go, epaper.go, fbdev.go), go.mod and an English README covering
wiring (incl. PWR pin), the three GPIO fallback paths, the 0x71 busy
quirk, build (go mod tidy && go build), systemd unit and logrotate
config.
```
