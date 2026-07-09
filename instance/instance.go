package instance

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type pgrepRunner func(name string, args ...string) ([]byte, error)

// HasOtherProcess reports whether any process with the given name exists
// besides selfPID. Used by the daemon guard to block launches when any
// cardbot instance is running.
func HasOtherProcess(processName string, selfPID int) (bool, error) {
	return hasOtherProcessWithRunner(processName, selfPID, runPgrep)
}

func hasOtherProcessWithRunner(processName string, selfPID int, run pgrepRunner) (bool, error) {
	processName = strings.TrimSpace(processName)
	if processName == "" {
		return false, fmt.Errorf("process name is required")
	}

	out, err := run("pgrep", "-x", processName)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("pgrep -x %s failed: %w", processName, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		if pid != selfPID {
			return true, nil
		}
	}

	return false, nil
}

// HasOtherInteractiveProcess reports whether any non-daemon cardbot process
// exists besides selfPID. Daemon processes (those with --daemon in their
// command line) are excluded so the interactive guard doesn't block manual
// launches when the background daemon is running.
func HasOtherInteractiveProcess(processName string, selfPID int) (bool, error) {
	return hasOtherInteractiveProcessWithRunner(processName, selfPID, runPgrep)
}

func hasOtherInteractiveProcessWithRunner(processName string, selfPID int, run pgrepRunner) (bool, error) {
	processName = strings.TrimSpace(processName)
	if processName == "" {
		return false, fmt.Errorf("process name is required")
	}

	out, err := run("pgrep", "-x", processName)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("pgrep -x %s failed: %w", processName, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		if pid == selfPID {
			continue
		}
		// Check if this PID is a daemon process.
		cmdOut, cmdErr := run("ps", "-p", strconv.Itoa(pid), "-o", "args=")
		if cmdErr != nil {
			// Can't inspect — treat as interactive to be safe.
			return true, nil
		}
		cmdLine := strings.TrimSpace(string(cmdOut))
		if strings.Contains(cmdLine, "--daemon") {
			continue // skip daemon processes
		}
		return true, nil
	}

	return false, nil
}

func runPgrep(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// ProcessMatches reports whether pid's executable command name matches the
// expected executable. It is used to reject stale PID files whose PID has been
// reused by an unrelated process.
func ProcessMatches(pid int, expectedName string) (bool, error) {
	return processMatchesWithRunner(pid, expectedName, runPgrep)
}

func processMatchesWithRunner(pid int, expectedName string, run pgrepRunner) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("pid must be positive")
	}
	expectedName = filepath.Base(strings.TrimSpace(expectedName))
	if expectedName == "" || expectedName == "." {
		return false, fmt.Errorf("process name is required")
	}
	out, err := run("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return false, err
	}
	actual := filepath.Base(strings.TrimSpace(string(out)))
	return actual == expectedName, nil
}
