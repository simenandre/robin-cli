package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// IO bundles streams + flags that affect output for a single command run.
type IO struct {
	Out      io.Writer
	Err      io.Writer
	In       io.Reader
	JSON     bool
	Quiet    bool
	NoColor  bool
	NoInput  bool
	Verbose  bool
	stdoutTTY bool
	stderrTTY bool
	stdinTTY  bool
}

func NewIO() *IO {
	io := &IO{
		Out:       os.Stdout,
		Err:       os.Stderr,
		In:        os.Stdin,
		stdoutTTY: isatty.IsTerminal(os.Stdout.Fd()),
		stderrTTY: isatty.IsTerminal(os.Stderr.Fd()),
		stdinTTY:  isatty.IsTerminal(os.Stdin.Fd()),
	}
	return io
}

func (io *IO) StdoutTTY() bool { return io.stdoutTTY }
func (io *IO) StdinTTY() bool  { return io.stdinTTY }

// applyFlags must be called once globals are parsed.
func (io *IO) applyFlags() {
	if io.NoColor || os.Getenv("NO_COLOR") != "" {
		color.NoColor = true
	} else if !io.stdoutTTY && !io.stderrTTY {
		color.NoColor = true
	}
}

func (io *IO) Printf(format string, args ...any) {
	if io.Quiet {
		return
	}
	fmt.Fprintf(io.Out, format, args...)
}

func (io *IO) Println(args ...any) {
	if io.Quiet {
		return
	}
	fmt.Fprintln(io.Out, args...)
}

func (io *IO) Status(format string, args ...any) {
	if io.Quiet {
		return
	}
	fmt.Fprintf(io.Err, format+"\n", args...)
}

func (io *IO) Debug(format string, args ...any) {
	if !io.Verbose {
		return
	}
	fmt.Fprintf(io.Err, "  "+format+"\n", args...)
}

func (io *IO) Errorf(format string, args ...any) {
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	fmt.Fprintf(io.Err, "%s %s\n", red("error:"), fmt.Sprintf(format, args...))
}

func (io *IO) Success(format string, args ...any) {
	if io.Quiet {
		return
	}
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Fprintf(io.Out, "%s %s\n", green("✓"), fmt.Sprintf(format, args...))
}

// JSONOut writes v as pretty JSON to stdout. Use for --json output.
func (io *IO) JSONOut(v any) error {
	enc := json.NewEncoder(io.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Tabular returns a tabwriter that flushes when closed.
func (io *IO) Tabular() *tabwriter.Writer {
	return tabwriter.NewWriter(io.Out, 0, 0, 2, ' ', 0)
}
