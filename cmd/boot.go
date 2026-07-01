package cmd

import (
	"fmt"
	"os"
	"strings"
)

func printLogo() {
	leftPad := " "
	logoLines := []string{
		"▄▀▀▀▀ ▄▀▀▀▄ █▀▀▀▄ █▀▀▀▄ █▀▀▀▄ ▄▀▀▀▄ ▀▀█▀▀",
		"█     █▀▀▀█ █▀▀▀▄ █   █ █▀▀▀▄ █   █   █  ",
		" ▀▀▀▀ ▀   ▀ ▀   ▀ ▀▀▀▀  ▀▀▀▀   ▀▀▀    ▀  ",
	}

	start := [3]int{255, 153, 255}
	end := [3]int{36, 114, 200}

	fmt.Println()

	colorMode := detectColorMode()

	if colorMode == colorNone {
		for _, line := range logoLines {
			fmt.Println(leftPad + line)
		}
		return
	}

	for _, line := range logoLines {
		fmt.Println(leftPad + colorizeGradient(line, start, end, colorMode))
	}
}

type colorLevel int

const (
	colorNone      colorLevel = iota
	color256                  // 256-color (Terminal.app, etc.)
	colorTrueColor            // 24-bit truecolor (iTerm2, Ghostty, etc.)
)

func detectColorMode() colorLevel {
	if os.Getenv("NO_COLOR") != "" {
		return colorNone
	}
	term := os.Getenv("TERM")
	if term == "" || strings.EqualFold(term, "dumb") {
		return colorNone
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return colorNone
	}

	ct := os.Getenv("COLORTERM")
	if strings.EqualFold(ct, "truecolor") || strings.EqualFold(ct, "24bit") {
		return colorTrueColor
	}
	return color256
}

// rgbTo256 finds the closest xterm-256 color index for an RGB value.
func rgbTo256(r, g, b int) int {
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 248 {
			return 231
		}
		return 232 + int(float64(r-8)/247.0*24.0+0.5)
	}
	ri := int(float64(r)/255.0*5.0 + 0.5)
	gi := int(float64(g)/255.0*5.0 + 0.5)
	bi := int(float64(b)/255.0*5.0 + 0.5)
	return 16 + 36*ri + 6*gi + bi
}

func colorizeGradient(line string, start, end [3]int, mode colorLevel) string {
	runes := []rune(line)
	if len(runes) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, r := range runes {
		var t float64
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		rc := lerp(start[0], end[0], t)
		gc := lerp(start[1], end[1], t)
		bc := lerp(start[2], end[2], t)

		if mode == colorTrueColor {
			sb.WriteString(fmt.Sprintf("\033[38;2;%d;%d;%dm", rc, gc, bc))
		} else {
			sb.WriteString(fmt.Sprintf("\033[38;5;%dm", rgbTo256(rc, gc, bc)))
		}
		sb.WriteRune(r)
	}
	sb.WriteString("\033[0m")
	return sb.String()
}

func lerp(a, b int, t float64) int {
	return a + int(float64(b-a)*t+0.5)
}
