package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/monofs/pkg/display"
)

type Printer struct {
	Format                  cliformat.Format
	Writer                  io.Writer
	DisplayProfile          string
	DisplayKeys             map[string]string
	DisplaySurfaces         map[string]map[string]string
	DisplayIncludeCanonical bool
}

func (p *Printer) PrintMutation(r cliformat.MutationResult) {
	if p.Format == cliformat.FormatJSON {
		p.PrintJSON(r)
		return
	}
	status := "OK"
	if !r.Success {
		status = "ERROR"
	}
	p.PrintText("%s %s version=%s batch=%s correlation=%s\n", status, r.LogicalPath, r.VersionID, r.BatchRevisionID, r.CorrelationID)
}

func (p *Printer) PrintJSON(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	if len(p.DisplayKeys) > 0 {
		mapped, mapErr := display.MapJSONWithOptions(raw, &display.CompiledProfile{
			ID:               p.DisplayProfile,
			Keys:             p.DisplayKeys,
			Surfaces:         p.DisplaySurfaces,
			IncludeCanonical: p.DisplayIncludeCanonical,
		}, display.MapOptions{})
		if mapErr == nil {
			raw = mapped
		}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return
	}
	buf.WriteByte('\n')
	_, _ = p.writer().Write(buf.Bytes())
}

func (p *Printer) PrintText(format string, args ...any) {
	_, _ = fmt.Fprintf(p.writer(), format, args...)
}

func (p *Printer) PrintTable(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(p.writer(), 0, 2, 2, ' ', 0)
	if len(headers) > 0 {
		for i, header := range headers {
			if i > 0 {
				_, _ = fmt.Fprint(tw, "\t")
			}
			_, _ = fmt.Fprint(tw, header)
		}
		_, _ = fmt.Fprint(tw, "\n")
	}
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				_, _ = fmt.Fprint(tw, "\t")
			}
			_, _ = fmt.Fprint(tw, cell)
		}
		_, _ = fmt.Fprint(tw, "\n")
	}
	_ = tw.Flush()
}

func (p *Printer) writer() io.Writer {
	if p.Writer != nil {
		return p.Writer
	}
	return os.Stdout
}
