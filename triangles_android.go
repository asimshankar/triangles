// +build android

package main

import (
	"flag"
)

func exitOnLifecycleCrossOff() bool { return false }

func preV23Init() {
	// If not logging to stderr but logging to files, then the logging
	// system seems to cause trouble (hang the goroutine trying to log
	// after the first message has been logged). Haven't figure this out
	// yet, but as a workaround disable file-logging and just log to
	// stderr.  "adb logcat" will show anything printed out to stderr.
	// TODO: There must be a better way!
	if f := flag.Lookup("logtostderr"); f != nil {
		f.Value.Set("true")
	}
}
