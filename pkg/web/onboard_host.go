package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vanderheijden86/b9s/internal/bdrun"
	"github.com/vanderheijden86/b9s/internal/onboard"
)

// LocalHost is the OnboardHost of the computer b9s web runs on. The folder
// picker and Terminal need macOS; the checks and bd init work everywhere.
type LocalHost struct{}

var (
	errNotMac   = errors.New("this needs macOS: run the command in a terminal yourself")
	errTestMode = errors.New("B9S_TEST_MODE is set: b9s opens no windows in a test")
)

// windowsAllowed reports why this host may not open a dialog or a Terminal
// window, or nil when it may.
func windowsAllowed() error {
	if os.Getenv("B9S_TEST_MODE") != "" {
		return errTestMode
	}
	if runtime.GOOS != "darwin" {
		return errNotMac
	}
	return nil
}

// PickFolder asks the frontmost app, normally the browser, to show the
// picker, so the dialog opens in front of the page that asked for it.
func (LocalHost) PickFolder(ctx context.Context) (string, error) {
	if err := windowsAllowed(); err != nil {
		return "", err
	}
	script := `tell application (path to frontmost application as text) to POSIX path of (choose folder with prompt "Choose the folder that holds the code")`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	if err != nil {
		var exit *exec.ExitError
		// osascript reports Cancel as error -128 with exit status 1.
		if errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "-128") {
			return "", nil
		}
		return "", fmt.Errorf("folder picker: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), "/"), nil
}

func (h LocalHost) StartAgent(ctx context.Context, dir string, agent onboard.Agent, prompt string) error {
	bin, ok := onboard.FindAgent(agent)
	if !ok {
		return fmt.Errorf("%s is not installed on this computer", agent.Label())
	}
	// Terminal runs each line it is given, so the prompt travels as one.
	oneLine := strings.Join(strings.Fields(prompt), " ")
	return h.terminal(ctx, fmt.Sprintf("cd %s && PATH=%s:\"$PATH\" %s %s", shellQuote(dir), shellQuote(toolPath()), shellQuote(bin), shellQuote(oneLine)))
}

func (h LocalHost) OpenTerminal(ctx context.Context, dir string, b9s bool) error {
	command := fmt.Sprintf("cd %s && PATH=%s:\"$PATH\"", shellQuote(dir), shellQuote(toolPath()))
	if b9s {
		command += " && b9s"
	}
	return h.terminal(ctx, command)
}

func (LocalHost) terminal(ctx context.Context, command string) error {
	if err := windowsAllowed(); err != nil {
		return err
	}
	script := fmt.Sprintf("tell application \"Terminal\"\nactivate\ndo script %s\nend tell", appleString(command))
	if out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("Terminal: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InitBeads runs bd init with the settings a shell profile may hold for a
// Dolt server removed: the project is always a local embedded one.
func (LocalHost) InitBeads(ctx context.Context, dir string) error {
	bd, ok := bdrun.Resolve()
	if !ok {
		return errors.New("bd is not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bd, "init", "--non-interactive", "--skip-agents", "--skip-hooks")
	cmd.Dir = dir
	cmd.Env = append(embeddedEnv(os.Environ()), "BD_NON_INTERACTIVE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd init: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (LocalHost) Check(ctx context.Context, dir string, agent onboard.Agent) onboard.Report {
	return onboard.Check(ctx, dir, agent, onboard.Options{})
}

func (LocalHost) AgentInstalled(agent onboard.Agent) bool {
	_, ok := onboard.FindAgent(agent)
	return ok
}

// embeddedEnv drops the variables that point bd at a Dolt server or another
// workspace.
func embeddedEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "BEADS_") || strings.HasPrefix(name, "BD_") || strings.HasPrefix(name, "DOLT_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// toolPath puts the running b9s and the bd it uses first on the agent's
// PATH: the brief tells the agent to run both, and a terminal's own PATH may
// hold neither.
func toolPath() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if bd, ok := bdrun.Resolve(); ok {
		dirs = append(dirs, filepath.Dir(bd))
	}
	return strings.Join(dirs, ":")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
