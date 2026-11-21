//go:build !race || !unix

package tooltest

func raceExec() error { return nil }
