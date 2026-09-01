package log

import (
	logrusr "github.com/bombsimon/logrusr/v3"
	"github.com/sirupsen/logrus"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// InitControllerRuntimeLogger initializes the controller-runtime logger
// to suppress warnings when external libraries create Kubernetes clients.
// If name is provided, it will be set as the logger name.
func InitControllerRuntimeLogger(name string) {
	ctrlLogger := logrus.New()
	logger := logrusr.New(ctrlLogger)
	if name != "" {
		logger = logger.WithName(name)
	}
	ctrllog.SetLogger(logger)
}
