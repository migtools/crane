package flags

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type GlobalFlags struct {
	Debug bool
}

func (g *GlobalFlags) ApplyFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&g.Debug, "debug", false, "Debug the command by printing more information")
}

func (g *GlobalFlags) GetLogger() *logrus.Logger {
	log := logrus.New()
	if g.Debug {
		log.SetLevel(logrus.DebugLevel)
	}
	return log
}
