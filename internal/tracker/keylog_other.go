//go:build !linux

package tracker

// startNativeKeyCapture is a no-op outside Linux. Keystroke statistics are
// still collected through the desktop frontend and the /api/tracker/input API.
func startNativeKeyCapture(_ chan<- int, _ <-chan struct{}) {}
