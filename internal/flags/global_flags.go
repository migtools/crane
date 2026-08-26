package flags

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type GlobalFlags struct {
	ConfigFile string
	Debug      bool
	logger     *logrus.Logger
}

func (g *GlobalFlags) ApplyFlags(cmd *cobra.Command) {
	cobra.OnInitialize(g.initConfig)
	cmd.PersistentFlags().BoolVar(&g.Debug, "debug", false, "Debug the command by printing more information")
	cmd.PersistentFlags().StringVarP(&g.ConfigFile, "flags-file", "f", "", "Path to input file which contains a yaml representation of cli flags. Explicit flags take precedence over input file values.")
	viper.BindPFlags(cmd.PersistentFlags())
}

// GetLoggerOrDefault returns the configured logger, or logrus.StandardLogger() if GlobalFlags is nil.
func (g *GlobalFlags) GetLoggerOrDefault() *logrus.Logger {
	if g == nil {
		return logrus.StandardLogger()
	}
	return g.GetLogger()
}

func (g *GlobalFlags) GetLogger() *logrus.Logger {
	if g.logger == nil {
		g.logger = logrus.New()
		if g.Debug {
			g.logger.SetLevel(logrus.DebugLevel)
		}
	}
	return g.logger
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
