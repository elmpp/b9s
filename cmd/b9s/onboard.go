package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vanderheijden86/b9s/internal/onboard"
	"github.com/vanderheijden86/b9s/pkg/version"
)

// runOnboard prints the setup brief for a repository, or with --check
// reports how far the setup got. --check exits 0 only when every step is
// done, so an agent can loop on it.
func runOnboard(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("onboard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	agentFlag := flags.String("agent", "claude", "agent the setup is for: claude, codex, cursor or other")
	check := flags.Bool("check", false, "report the setup checklist instead of the brief; exit 0 when complete")
	asJSON := flags.Bool("json", false, "print the checklist as JSON (implies --check)")
	prompt := flags.Bool("prompt", false, "print the short prompt to paste into the agent")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: b9s onboard [--agent claude|codex|cursor|other] [--check] [--json] [--prompt] [dir]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(reorderFlags(args)); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return 2
	}
	agent, err := onboard.ParseAgent(*agentFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *prompt {
		fmt.Fprintln(stdout, onboard.Prompt(agent))
		return 0
	}
	dir := "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "%s is not a folder\n", dir)
		return 2
	}

	ctx := context.Background()
	report := onboard.Check(ctx, dir, agent, onboard.Options{})
	switch {
	case *asJSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	case *check:
		fmt.Fprintf(stdout, "Beads setup for %s (%s)\n", dir, agent.Label())
		for _, s := range report.Steps {
			mark := "[ ]"
			if s.Done {
				mark = "[x]"
			}
			fmt.Fprintf(stdout, "%s %-16s %s\n", mark, s.Title, s.Detail)
		}
		if report.Complete {
			fmt.Fprintln(stdout, "Complete: B9s can open this project.")
		}
	default:
		fmt.Fprint(stdout, onboard.Brief(dir, agent, onboard.Versions{B9s: version.Get().Version, Bd: onboard.BdVersion(ctx)}, report))
		return 0
	}
	if !report.Complete {
		return 1
	}
	return 0
}

// reorderFlags moves a leading folder argument behind the flags, because the
// flag package stops at the first non-flag and "b9s onboard . --check" reads
// naturally.
func reorderFlags(args []string) []string {
	var flagArgs, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			if (a == "--agent" || a == "-agent") && i+1 < len(args) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flagArgs, rest...)
}
