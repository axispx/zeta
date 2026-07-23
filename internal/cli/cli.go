package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axispx/zeta/internal/version"
)

// Options is the parsed CLI invocation.
type Options struct {
	Help     bool
	Version  bool
	Resume   bool   // --resume was set
	ResumeID string // set when --resume=<id>
}

// resumeFlag accepts bare --resume (picker) or --resume=<id> (open session).
type resumeFlag struct {
	set bool
	id  string
}

func (f *resumeFlag) String() string {
	if f == nil {
		return ""
	}
	return f.id
}

func (f *resumeFlag) Set(s string) error {
	f.set = true
	if s != "true" {
		f.id = s
	}
	return nil
}

func (f *resumeFlag) IsBoolFlag() bool { return true }

// Parse parses args (typically os.Args[1:]).
// On success with Help or Version set, the caller should print and exit 0.
func Parse(args []string) (Options, error) {
	var opts Options
	var resume resumeFlag

	fs := flag.NewFlagSet("zeta", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&resume, "resume", "")
	fs.BoolVar(&opts.Help, "help", false, "")
	fs.BoolVar(&opts.Help, "h", false, "")
	fs.BoolVar(&opts.Version, "version", false, "")
	fs.BoolVar(&opts.Version, "v", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			opts.Help = true
			return opts, nil
		}
		return Options{}, err
	}
	if fs.NArg() > 0 {
		return Options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	opts.Resume = resume.set
	opts.ResumeID = resume.id
	return opts, nil
}

// WriteUsage writes the help text to w.
func WriteUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: zeta [flags]

Flags:
  -h, --help           show help
  -v, --version        print version
      --resume         open session list
      --resume=<id>    open session by id
`)
}

// WriteVersion writes the version line to w.
func WriteVersion(w io.Writer) {
	fmt.Fprintf(w, "zeta %s\n", strings.TrimPrefix(version.Version, "v"))
}

// ExitUsage prints usage to stderr and exits 2 (bad invocation).
func ExitUsage(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "zeta: %v\n", err)
	}
	WriteUsage(os.Stderr)
	os.Exit(2)
}
