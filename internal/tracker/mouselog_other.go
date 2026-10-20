//go:build !linux

package tracker

// startNativeMouseCapture is a no-op outside Linux. Mouse clicks are still
// collected through the desktop frontend and the /api/tracker/input API.
func startNativeMouseCapture(_ chan<- mouseEvent, _ <-chan struct{}) {}
