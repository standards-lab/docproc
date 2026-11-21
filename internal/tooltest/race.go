//go:build race && unix

package tooltest

import (
	"os"
	"strings"
	"syscall"
)

const raceMark = "TOOLTEST_GORACE"

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

func cutMark(mark string) (string, bool) {
	if len(mark) > 0 && mark[0] == '=' {
		return mark[1:], true
	}
	return "", false
}
