package flags

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type GlobalFlags struct {
	ConfigFile string
	Debug      bool
}

func (g *GlobalFlags) ApplyFlags(cmd *cobra.Command) {
	cobra.OnInitialize(g.initConfig)
	cmd.PersistentFlags().BoolVar(&g.Debug, "debug", false, "Debug the command by printing more information")
	cmd.PersistentFlags().StringVarP(&g.ConfigFile, "flags-file", "f", "", "Path to input file which contains a yaml representation of cli flags. Explicit flags take precedence over input file values.")
	viper.BindPFlags(cmd.PersistentFlags())
}

func (g *GlobalFlags) GetLogger() *logrus.Logger {
	log := logrus.New()
	if g.Debug {
		log.SetLevel(logrus.DebugLevel)
	}
	return log
}

func (g *GlobalFlags) initConfig() {
	if g.ConfigFile != "" {
		viper.SetConfigFile(g.ConfigFile)
	}
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		g.GetLogger().Infof("Using config file: %v", viper.ConfigFileUsed())
	}
}
