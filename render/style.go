package render

// Style is the set of characters used to draw table borders.
type Style struct {
	H, V       string // horizontal and vertical lines
	TL, TM, TR string // top row: left corner, column junction, right corner
	ML, MM, MR string // separator rows
	BL, BM, BR string // bottom row
}

var (
	Rounded = Style{"─", "│", "╭", "┬", "╮", "├", "┼", "┤", "╰", "┴", "╯"}
	ASCII   = Style{"-", "|", "+", "+", "+", "+", "+", "+", "+", "+", "+"}
)

// Styles maps the names accepted by --style to their styles.
var Styles = map[string]Style{
	"rounded": Rounded,
	"ascii":   ASCII,
}
