package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
)

// SchemaVersion is bumped on any breaking change to JSON output shapes.
const SchemaVersion = 1

// Output owns the stdout/stderr contract: stdout carries only the
// command's result (text or JSON); everything else — progress, logs,
// notices, prompts — goes to stderr.
type Output struct {
	Out, Err io.Writer
	In       io.Reader

	JSON    bool
	Quiet   bool
	Verbose bool
	Color   bool
	// Interactive is true only when a human can answer prompts: stdin and
	// stderr are terminals, --no-input is unset and CI is unset.
	Interactive bool

	mu sync.Mutex
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// Process streams; tests swap them.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
	stdin  io.Reader = os.Stdin
	// forceInteractive lets tests exercise prompt paths (nil = detect).
	forceInteractive *bool
)

// newOutput builds the default Output from process state and global flags.
func newOutput(jsonOut, quiet, verbose, noColor, noInput bool) *Output {
	stdoutTTY := isTerminal(os.Stdout)
	interactive := isTerminal(os.Stdin) && isTerminal(os.Stderr) && !noInput && os.Getenv("CI") == ""
	if forceInteractive != nil {
		interactive = *forceInteractive && !noInput
		stdoutTTY = false
	}
	return &Output{
		Out: stdout, Err: stderr, In: stdin,
		JSON: jsonOut, Quiet: quiet, Verbose: verbose,
		Color:       stdoutTTY && !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		Interactive: interactive,
	}
}

// Result writes a successful result: JSON envelope or the text renderer.
func (o *Output) Result(payload any, text func(w io.Writer)) {
	if o.JSON {
		o.writeJSON(true, payload, nil)
		return
	}
	if text != nil {
		text(o.Out)
	}
}

// Failure reports err in the active format. In JSON mode the error object
// goes to stdout (so scripts parse one stream); text mode uses stderr.
func (o *Output) Failure(e *Error) {
	if o.JSON {
		o.writeJSON(false, e.Data, e)
		return
	}
	fmt.Fprintf(o.Err, "%s %s\n", o.paint("31;1", "error:"), e.Message)
	if len(e.Suggestions) > 0 {
		if len(e.Suggestions) == 1 {
			fmt.Fprintf(o.Err, "\nDid you mean this?\n")
		} else {
			fmt.Fprintf(o.Err, "\nDid you mean one of these?\n")
		}
		for _, s := range e.Suggestions {
			fmt.Fprintf(o.Err, "    %s\n", s)
		}
	}
	if len(e.Available) > 0 {
		fmt.Fprintf(o.Err, "\nAvailable: %s\n", strings.Join(e.Available, ", "))
	}
	if e.Hint != "" {
		fmt.Fprintf(o.Err, "\n%s %s\n", o.paint("2", "hint:"), e.Hint)
	}
}

func (o *Output) writeJSON(ok bool, payload any, e *Error) {
	env := map[string]any{"schema_version": SchemaVersion, "ok": ok}
	if payload != nil {
		// Flatten struct/map payloads into the envelope for ergonomic `jq .repos`
		raw, err := json.Marshal(payload)
		if err == nil {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				for k, v := range m {
					env[k] = v
				}
			} else {
				env["result"] = payload
			}
		}
	}
	if e != nil {
		env["error"] = e
	}
	enc := json.NewEncoder(o.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	o.mu.Lock()
	defer o.mu.Unlock()
	enc.Encode(env)
}

// Notice prints an informational line to stderr (suppressed by --quiet).
func (o *Output) Notice(format string, a ...any) {
	if o.Quiet {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.Err, format+"\n", a...)
}

// Warn prints a warning to stderr (never suppressed).
func (o *Output) Warn(format string, a ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.Err, "%s %s\n", o.paint("33", "warning:"), fmt.Sprintf(format, a...))
}

// Log is a git.LogFunc that streams operation progress to stderr.
func (o *Output) Log(ctx, msg string, isErr bool) {
	if o.Quiet && !isErr {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	prefix := ""
	if ctx != "" {
		prefix = o.paint("2", "["+ctx+"]") + " "
	}
	if isErr {
		msg = o.paint("33", msg)
	}
	fmt.Fprintf(o.Err, "%s%s\n", prefix, msg)
}

// paint wraps s in an SGR color when color output is enabled. Color is
// decided from stdout's TTY state; stderr shares it in practice.
func (o *Output) paint(sgr, s string) string {
	if !o.Color {
		return s
	}
	return "\x1b[" + sgr + "m" + s + "\x1b[0m"
}

// Confirm asks a y/N question on stderr. Callers must only call it when
// o.Interactive is true.
func (o *Output) Confirm(question string) bool {
	fmt.Fprintf(o.Err, "%s [y/N] ", question)
	ans := strings.ToLower(readLine(o.In))
	return ans == "y" || ans == "yes"
}

// ConfirmTyped requires the user to type an exact word (e.g. DELETE).
func (o *Output) ConfirmTyped(question, word string) bool {
	fmt.Fprintf(o.Err, "%s\nType %s to confirm: ", question, word)
	return strings.TrimSpace(readLine(o.In)) == word
}

func readLine(r io.Reader) string {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
