package log

import (
	logrusr "github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	"github.com/sirupsen/logrus"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// InitControllerRuntimeLogger initializes the controller-runtime logger
// to suppress warnings when external libraries create Kubernetes clients.
// If name is provided, it will be set as the logger name.
// Returns the initialized logger for use by callers.
func InitControllerRuntimeLogger(name string) logr.Logger {
	ctrlLogger := logrus.New()
	logger := logrusr.New(ctrlLogger)
	if name != "" {
		logger = logger.WithName(name)
	}
	ctrllog.SetLogger(logger)
	return logger
}
