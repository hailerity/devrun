package cli

import "github.com/spf13/cobra"

var fgCmd = &cobra.Command{Use: "fg", Short: "Attach to a service (not yet implemented)"}

// runFgByName is called from startOne when --fg is used. Implemented in Task 18.
func runFgByName(socketPath, name string) error {
	// TODO: implement in Task 18
	return nil
}
