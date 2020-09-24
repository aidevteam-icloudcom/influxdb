package upgrade

import (
	"errors"
	"go.uber.org/zap"
)

// Generates security upgrade script.
func generateSecurityScript(v1 *influxDBv1, dbBuckets map[string][]string, log *zap.Logger) error {
	return errors.New("not implemented")
}
