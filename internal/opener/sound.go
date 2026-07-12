package opener

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// macOS system sound names (files under /System/Library/Sounds).
var macSounds = map[string]string{
	"glass":     "Glass",
	"submarine": "Submarine",
	"ping":      "Ping",
	"pop":       "Pop",
	"hero":      "Hero",
}

// PlayDoneSound plays a short completion sound, best-effort and async.
// "off"/"" is silent; "bell" rings the terminal bell everywhere; the named
// sounds use afplay on macOS and the freedesktop complete sound on Linux
// (falling back to the bell when no player is available).
func PlayDoneSound(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "off":
		return
	case "bell":
		ringBell()
		return
	}

	if runtime.GOOS == "darwin" {
		mac, ok := macSounds[name]
		if !ok {
			mac = "Glass"
		}
		exec.Command("afplay", "/System/Library/Sounds/"+mac+".aiff").Start()
		return
	}

	// Linux: any named sound maps to the freedesktop completion sound
	if _, err := exec.LookPath("paplay"); err == nil {
		exec.Command("paplay", "/usr/share/sounds/freedesktop/stereo/complete.oga").Start()
		return
	}
	ringBell()
}

// ringBell writes the terminal bell to stderr — it's non-visual, so it
// doesn't disturb the Bubble Tea renderer on stdout.
func ringBell() {
	os.Stderr.Write([]byte{7})
}
