package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

type Color int

const (
	Default Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	Gray
)

const columnGap = 2

var colorCodes = map[Color]string{
	Red:     "31",
	Green:   "32",
	Yellow:  "33",
	Blue:    "34",
	Magenta: "35",
	Cyan:    "36",
	Gray:    "90",
}

var gradientSteps = []int{51, 45, 39, 33, 69, 105, 141, 177}

type Styler struct {
	enabled bool
}

type Cell struct {
	Text  string
	Color Color
	Bold  bool
	Dim   bool
}

func New(f *os.File, noColor bool) Styler {
	return Styler{enabled: colorEnabled(f, noColor)}
}

func NewStyler(enabled bool) Styler {
	return Styler{enabled: enabled}
}

func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func colorEnabled(f *os.File, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(f)
}

func (s Styler) Enabled() bool {
	return s.enabled
}

func (s Styler) Bold(text string) string {
	return s.wrap("1", text)
}

func (s Styler) Dim(text string) string {
	return s.wrap("2", text)
}

func (s Styler) Paint(c Color, text string) string {
	code, ok := colorCodes[c]
	if !ok {
		return text
	}
	return s.wrap(code, text)
}

func (s Styler) BoldPaint(c Color, text string) string {
	code, ok := colorCodes[c]
	if !ok {
		return s.Bold(text)
	}
	return s.wrap("1;"+code, text)
}

func (s Styler) Section(title string) string {
	return s.BoldPaint(Cyan, strings.ToUpper(title))
}

func (s Styler) Gradient(text string) string {
	if !s.enabled || text == "" {
		return text
	}
	runes := []rune(text)
	var b strings.Builder
	for i, r := range runes {
		step := gradientSteps[i*len(gradientSteps)/len(runes)]
		fmt.Fprintf(&b, "\x1b[1;38;5;%dm%c", step, r)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func (s Styler) Banner(name, version, tagline string) string {
	var b strings.Builder
	b.WriteString("  " + s.Gradient("◆ "+name) + "  " + s.Dim(version) + "\n")
	b.WriteString("  " + s.Dim(tagline) + "\n")
	return b.String()
}

func StateCell(state string) Cell {
	switch state {
	case "available":
		return Cell{Text: "● available", Color: Green}
	case "unavailable":
		return Cell{Text: "○ unavailable", Color: Red}
	default:
		return Cell{Text: "· " + state, Color: Gray}
	}
}

func (s Styler) Table(w io.Writer, indent string, headers []string, rows [][]Cell) {
	columns := len(headers)
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return
	}

	widths := make([]int, columns)
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell.Text); n > widths[i] {
				widths[i] = n
			}
		}
	}

	if len(headers) > 0 {
		cells := make([]Cell, len(headers))
		for i, header := range headers {
			cells[i] = Cell{Text: strings.ToUpper(header), Dim: true}
		}
		s.row(w, indent, cells, widths)
	}
	for _, row := range rows {
		s.row(w, indent, row, widths)
	}
}

func (s Styler) row(w io.Writer, indent string, cells []Cell, widths []int) {
	var line strings.Builder
	line.WriteString(indent)
	for i, cell := range cells {
		line.WriteString(s.style(cell))
		if i < len(cells)-1 {
			padding := widths[i] - utf8.RuneCountInString(cell.Text) + columnGap
			line.WriteString(strings.Repeat(" ", padding))
		}
	}
	fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
}

func (s Styler) style(c Cell) string {
	switch {
	case c.Dim:
		return s.Dim(c.Text)
	case c.Bold:
		return s.BoldPaint(c.Color, c.Text)
	default:
		return s.Paint(c.Color, c.Text)
	}
}

func (s Styler) wrap(code, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
