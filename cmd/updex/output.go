package updex

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// writeLine writes one plain (non-table) line of the CLI's text output to out
// and returns the write failure instead of discarding it, so a command whose
// stdout cannot be written does not exit zero.
func writeLine(out io.Writer, line string) error {
	_, err := fmt.Fprintln(out, line)
	return err
}

// textTable renders one column-aligned table of the CLI's text output.
//
// text/tabwriter buffers the whole table and writes it to the underlying
// writer on Flush, so a header or row write rarely fails on its own; the
// failure normally surfaces from Flush. textTable records whichever comes
// first and reports it once from Flush, so callers only need to check the
// single Flush error to know the table reached the writer intact.
type textTable struct {
	w   *tabwriter.Writer
	err error
}

// newTextTable starts a table on out with the column padding every updex table
// uses.
func newTextTable(out io.Writer) *textTable {
	return &textTable{w: tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)}
}

// Rowf appends one tab-separated line (the header is just the first row).
// Rows after a failed write are dropped: the table is already incomplete, and
// re-reporting the same broken writer adds nothing.
func (t *textTable) Rowf(format string, args ...any) {
	if t.err != nil {
		return
	}
	if _, err := fmt.Fprintf(t.w, format, args...); err != nil {
		t.err = err
	}
}

// Flush finishes the table and returns the first header, row, or flush
// failure, or nil when the whole table reached the writer.
func (t *textTable) Flush() error {
	if t.err != nil {
		return t.err
	}
	return t.w.Flush()
}
