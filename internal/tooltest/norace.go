//go:build !race || !unix

package tooltest

// raceExec does nothing outside a -race build on Unix: only the race runtime sleeps at exit.
func raceExec() error { return nil }
