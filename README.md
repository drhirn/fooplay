# fooplay

Now-playing display for **foobar2000**, driven directly by a Raspberry Pi -
no desktop environment, no X11.

Polls the foobar2000 plugin **[beefweb](https://github.com/hyperblast/beefweb)**
over the network and renders cover, artist, title, album and year on:

* a **Waveshare 7.5" e-paper** (800×480, SPI) - one full refresh per track change,
  panel powers off when idle
* any **LCD screen** via the Linux framebuffer (`/dev/fb0`, HDMI/DSI)

Written in Go, single dependency (`golang.org/x/image`), console stays
silent, logs only errors/warnings.

Full documentation here: [docs](/docs/README.md)

<!-- SCREENSHOTS -->
<!-- E-Paper: photograph of the 7.5" panel showing cover + artist/album/year + title -->
<!-- LCD:      photo or capture of the HDMI screen with the same layout -->

## AI
This tool was completely written by AI (Kimi). Even the README.md – except this passage. And even this was corrected by AI.

I always wanted a display showing the song currently playing in foobar2000 while I'm gaming or otherwise unable to look at the foobar2000 window. But I suffered from a complete lack of skills. And although I'm not a friend of AI, I had the feeling that someday I would have to try it out. And that's where fooplay comes in.

It was a weird experience, and I still don't know what to think about AI – especially about outsourcing (programming) work to it. Nevertheless, after days of work I got this little tool running, which I'm happy about. But it's not the "yeah, I did this by myself" feeling I always had in pre-AI times, when I was forced to think things through and actually learn something.

## Features

* Cover art from beefweb (`/api/artwork/current`), vinyl placeholder as fallback
* Light theme by default (`-invert` for dark)
* Refresh only on track change - no periodic refreshes, e-paper-friendly
* Pause / stop / connection loss: display switches off completely
  (panel supply cut via PWR pin / LCD blanking), wakes on next track
* Robust GPIO: sysfs, gpiochip v1 and v2 APIs auto-detected
  (works on Pi 1-5, old and new kernels)
* E-paper + LCD simultaneously (`-display both`)
* Runs as a systemd service

## Quick start

```bash
sudo raspi-config        # enable SPI (e-paper) 
cd fooplay
go mod tidy && go build -o fooplay .
./fooplay                # auto: e-paper, else LCD
```

Wiring (plug-on HAT, signal mapping - all pins configurable):

| Signal | GPIO | Pin | | Signal | GPIO | Pin |
|--------|------|-----|---|--------|------|-----|
| DIN    | 10   | 19  | | DC     | 25   | 22  |
| CLK    | 11   | 23  | | RST    | 17   | 11  |
| CS     | 8    | 24  | | BUSY   | 24   | 18  |
|        |      |     | | PWR EN | 18   | 12  |

## Usage

```
./fooplay -addr http://10.168.1.1:8880 -display epaper -logfile fooplay.log
```

Common flags: `-display auto|epaper|fb|both`, `-poll 2s`, `-invert`,
`-pin-*`, `-logfile`, `-debug`. See [README.md](README.md) for the full
documentation (driver details, systemd unit, logrotate, troubleshooting).

## License

MIT (or your choice - see LICENSE)
