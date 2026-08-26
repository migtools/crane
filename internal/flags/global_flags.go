package flags

import (
	"io"

	"github.com/konveyor/crane/internal/audit"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type GlobalFlags struct {
	ConfigFile   string
	Debug        bool
	AuditLogPath string `mapstructure:"audit-log"`
	CmdName      string
	logger       *logrus.Logger
	fileHook     *audit.FileHook
}

func (g *GlobalFlags) ApplyFlags(cmd *cobra.Command) {
	cobra.OnInitialize(g.initConfig)
	cmd.PersistentFlags().BoolVar(&g.Debug, "debug", false, "Debug the command by printing more information")
	cmd.PersistentFlags().StringVarP(&g.ConfigFile, "flags-file", "f", "", "Path to input file which contains a yaml representation of cli flags. Explicit flags take precedence over input file values.")
	cmd.PersistentFlags().StringVar(&g.AuditLogPath, "audit-log", "audit/.crane-audit.log", "Path to the audit log file")
	viper.BindPFlags(cmd.PersistentFlags())
}

// SetCmdName sets the command name used in audit log entries. Safe to call on a nil GlobalFlags.
func (g *GlobalFlags) SetCmdName(name string) {
	if g != nil {
		g.CmdName = name
	}
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
		g.logger.SetLevel(logrus.DebugLevel)
		g.logger.SetOutput(io.Discard)
		consoleHook := audit.NewConsoleHook(g.Debug)
		fileHook, err := audit.NewFileHook(g.AuditLogPath, &g.CmdName)
		g.logger.AddHook(consoleHook)
		if err == nil {
			g.fileHook = fileHook
			g.logger.AddHook(fileHook)
		} else {
			g.logger.Warnf("Failed to open audit log file %s: %v", g.AuditLogPath, err)
		}
	}
	return g.logger
}

// Close releases the audit log file. Call this when the program exits.
func (g *GlobalFlags) Close() {
	if g.fileHook != nil {
		g.fileHook.Close()
	}
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
