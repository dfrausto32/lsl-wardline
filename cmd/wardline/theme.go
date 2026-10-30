package main

// windowColors are the colours of a Wardline window's title bar and border, matching the page inside it
// (the --bg, --text and --line colours in window.html and installer.html).
type windowColors struct{ caption, text, border uint32 }

// colorRef turns a 0xRRGGBB colour into the 0x00BBGGRR form Windows asks for.
func colorRef(rgb uint32) uint32 {
	return (rgb&0xff)<<16 | rgb&0xff00 | (rgb>>16)&0xff
}

func themeColors(dark bool) windowColors {
	if dark {
		return windowColors{caption: colorRef(0x141414), text: colorRef(0xf5f0e1), border: colorRef(0x3a362f)}
	}
	return windowColors{caption: colorRef(0xf5f0e1), text: colorRef(0x141414), border: colorRef(0xd8d0bb)}
}
