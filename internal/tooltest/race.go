//go:build race && unix

package tooltest

import (
	"os"
	"strings"
	"syscall"
)

// raceMark is set in a stand-in's environment as it executes itself again under -race: "unset"
// when GORACE was, else "=" and GORACE's value, so the second run restores GORACE as it was.
const raceMark = "TOOLTEST_GORACE"

// raceExec, in a stand-in built under -race, executes the stand-in again, the same pid and
// arguments, with GORACE's atexit_sleep_ms at 0, since the race runtime reads GORACE only as it
// starts and otherwise sleeps a second at every exit. The runners give a tool a fixed environment,
// which no GORACE of the test binary's reaches. In the second run it restores GORACE and removes
// its mark, so the stand-in's program, and anything it starts, sees the environment the runner
// gave. It returns only in the second run, or with the exec's error.
func raceExec() error {
	if mark, ok := os.LookupEnv(raceMark); ok {
		if err := os.Unsetenv(raceMark); err != nil {
			return err
		}
		if value, set := cutMark(mark); set {
			return os.Setenv("GORACE", value)
		}
		return os.Unsetenv("GORACE")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	mark, gorace := "unset", "atexit_sleep_ms=0"
	if value, ok := os.LookupEnv("GORACE"); ok {
		mark, gorace = "="+value, value+" atexit_sleep_ms=0"
	}
	env := []string{raceMark + "=" + mark, "GORACE=" + gorace}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GORACE=") {
			env = append(env, kv)
		}
	}
	return syscall.Exec(exe, os.Args, env)
}

// cutMark returns GORACE's value from mark, and whether GORACE was set.
func cutMark(mark string) (string, bool) {
	if len(mark) > 0 && mark[0] == '=' {
		return mark[1:], true
	}
	return "", false
}
