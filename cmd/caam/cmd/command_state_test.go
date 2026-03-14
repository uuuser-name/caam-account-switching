package cmd

import (
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetCommandTreeForExecute(cmd *cobra.Command) {
	resetCommandTreeState(cmd, true)
}

func resetCommandTreeIO(cmd *cobra.Command) {
	resetCommandTreeState(cmd, false)
}

func resetCommandTreeState(cmd *cobra.Command, resetFlags bool) {
	if cmd == nil {
		return
	}

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs(nil)

	if resetFlags {
		resetFlagDefaults(cmd.Flags())
		resetFlagDefaults(cmd.PersistentFlags())
	}

	for _, sub := range cmd.Commands() {
		resetCommandTreeState(sub, resetFlags)
	}
}

func setCommandTreeWriters(cmd *cobra.Command, out, err io.Writer) {
	if cmd == nil {
		return
	}

	cmd.SetOut(out)
	cmd.SetErr(err)

	for _, sub := range cmd.Commands() {
		setCommandTreeWriters(sub, out, err)
	}
}

func resetFlagDefaults(fs *pflag.FlagSet) {
	if fs == nil {
		return
	}

	fs.VisitAll(func(flag *pflag.Flag) {
		flag.Changed = false
		_ = flag.Value.Set(flag.DefValue)
		flag.Changed = false
	})
}
