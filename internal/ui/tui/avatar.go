package tui

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// The avatar's shape (KTD4): avatarWidth pixels wide and two pixel rows to
// each of its avatarRows cells, the right avatarWidth-avatarHalf columns
// mirroring the left ones.
const (
	avatarWidth = 5
	avatarRows  = 3
	avatarHalf  = (avatarWidth + 1) / 2
	// avatarHues is how many hues an avatar takes its colour from (KTD5).
	avatarHues = 6
	// pixelRows are the pixel rows a cell draws, top and bottom.
	pixelRows = 2
	// halfBlocks draws a cell's two pixels, indexed by top + 2*bottom.
	halfBlocks = " ▀▄█"
)

// avatarSeed is what e's avatar is generated from: its name, or, on the
// "you" entry, your login once known, so each boss gets their own (KTD4).
func avatarSeed(e core.BotView) string {
	if e.You && e.Login != "" {
		return clean(e.Login)
	}
	return clean(e.Name)
}

// avatarColour is e's avatar colour: its seed's hue, or the offline grey
// for a bot that is not acting (R2).
func (s styles) avatarColour(e core.BotView) color.Color {
	if !e.You && !e.Acting {
		return s.offline
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(avatarSeed(e)))
	return s.avatars[h.Sum32()%avatarHues]
}

// avatar is seed's identicon in c: avatarRows rows of avatarWidth cells,
// each cell two pixels of a mirrored pattern from the seed's hash (KTD4).
func (s styles) avatar(seed string, c color.Color) []string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	bits := h.Sum64()
	pixel := func(row, col uint) uint {
		if col >= avatarHalf {
			col = avatarWidth - 1 - col
		}
		return uint(bits >> (row*avatarHalf + col) & 1)
	}
	blocks := []rune(halfBlocks)
	st := lipgloss.NewStyle().Foreground(c)
	rows := make([]string, avatarRows)
	for r := range uint(avatarRows) {
		var b strings.Builder
		for col := range uint(avatarWidth) {
			top, bottom := pixel(pixelRows*r, col), pixel(pixelRows*r+1, col)
			b.WriteRune(blocks[top+pixelRows*bottom])
		}
		rows[r] = st.Render(b.String())
	}
	return rows
}
