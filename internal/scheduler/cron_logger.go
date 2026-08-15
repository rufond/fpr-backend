package scheduler

import (
	"time"

	"github.com/rs/zerolog/log"
)

type CronLogger struct{}

func (CronLogger) Info(msg string, keysAndValues ...any) {
	log.Debug().
		Fields(cronFields(keysAndValues...)).
		Msg(msg)
}

func (CronLogger) Error(err error, msg string, keysAndValues ...any) {
	log.Error().
		Err(err).
		Fields(cronFields(keysAndValues...)).
		Msg(msg)
}

func cronFields(keysAndValues ...any) map[string]any {
	fields := make(map[string]any, len(keysAndValues)/2)

	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			continue
		}

		value := keysAndValues[i+1]
		if timestamp, ok := value.(time.Time); ok {
			value = timestamp.UTC().Format(time.RFC3339Nano)
		}

		fields[key] = value
	}

	return fields
}
