//go:build notray

package tray

import "fmt"

// Available reports whether tray support was compiled in.
func Available() bool { return false }

// Run is a stub when built without tray support.
func Run(demoMode ...bool) error {
	return fmt.Errorf("tray mode not available in this build — rebuild without the 'notray' tag")
}
