package telemetry

import (
	"errors"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"os"
	"strings"
	"sync"
	"time"
)

var once sync.Once
var log zerolog.Logger

type LoggerFactory struct {
	Cfg config.LogConfig
}

func (l LoggerFactory) TimeFormat(s string) (string, error) {
	switch strings.ToLower(s) {
	case "unix":
		return zerolog.TimeFormatUnix, nil
	case "unixms":
		return zerolog.TimeFormatUnixMs, nil
	case "unixmicro":
		return zerolog.TimeFormatUnixMicro, nil
	case "unixnano":
		return zerolog.TimeFormatUnixNano, nil
	case "rfc3339":
		return time.RFC3339, nil
	case "rfc3339nano":
		return time.RFC3339Nano, nil
	case "rfc822":
		return time.RFC822, nil
	case "rfc822z":
		return time.RFC822Z, nil
	case "rfc850":
		return time.RFC850, nil
	case "rfc1123":
		return time.RFC1123, nil
	case "rfc1123z":
		return time.RFC1123Z, nil
	default:
		return "", errors.New("invalid time format: " + s)
	}
}

func (l LoggerFactory) GetLevel(s string) (zerolog.Level, error) {
	switch strings.ToLower(s) {
	case "trace":
		return zerolog.TraceLevel, nil
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "panic":
		return zerolog.PanicLevel, nil
	case "none":
		return zerolog.NoLevel, nil
	default:
		return zerolog.NoLevel, errors.New("invalid log level: " + s)
	}
}

func (l LoggerFactory) Get() (zerolog.Logger, error) {
	zl := l.Cfg.Zerolog
	var err error
	once.Do(func() {

		level, err := l.GetLevel(zl.LogLevel)
		if err != nil {
			return
		}

		format, err := l.TimeFormat(zl.TimeFormat)
		if err != nil {
			return
		}

		zerolog.TimeFieldFormat = format
		zerolog.SetGlobalLevel(level)

		// fields
		if zl.Fields.Caller != nil {
			zerolog.CallerFieldName = *zl.Fields.Caller
		}
		if zl.Fields.Timestamp != nil {
			zerolog.TimestampFieldName = *zl.Fields.Timestamp
		}
		if zl.Fields.Error != nil {
			zerolog.ErrorFieldName = *zl.Fields.Error
		}
		if zl.Fields.ErrorStack != nil {
			zerolog.ErrorStackFieldName = *zl.Fields.ErrorStack
		}
		if zl.Fields.Level != nil {
			zerolog.LevelFieldName = *zl.Fields.Level
		}
		if zl.Fields.Message != nil {
			zerolog.MessageFieldName = *zl.Fields.Message
		}
		loggerContext := zerolog.New(os.Stdout).With()

		zerolog.DurationFieldInteger = zl.DurationFieldInteger
		if zl.Stacktrace {
			loggerContext = loggerContext.Stack()
			zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
		}

		if zl.Caller {
			loggerContext = loggerContext.Caller()
		}

		if zl.Timestamp {
			loggerContext = loggerContext.Timestamp()
		}

		logger := loggerContext.Logger()

		if zl.Pretty {
			logger = logger.Output(zerolog.ConsoleWriter{Out: os.Stdout})
		} else {
			logger = logger.Output(os.Stdout)
		}

		traceField := "traceId"
		spanField := "spanId"
		if zl.Fields.TraceId != nil {
			traceField = *zl.Fields.TraceId
		}
		if zl.Fields.SpanId != nil {
			spanField = *zl.Fields.SpanId
		}

		logger = logger.Hook(TracingHook{
			TraceField: traceField,
			SpanField:  spanField,
		})
		log = logger
	})
	return log, err

}

type TracingHook struct {
	TraceField string
	SpanField  string
}

func (h TracingHook) Run(e *zerolog.Event, level zerolog.Level, message string) {

	if level == zerolog.NoLevel {

		return
	}
	if !e.Enabled() {
		return
	}

	ctx := e.GetCtx()
	if ctx == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	{ // (a) adds TraceIds & spanIds to logs.
		sCtx := span.SpanContext()
		if sCtx.HasTraceID() {
			e.Str(h.TraceField, sCtx.TraceID().String())
		}
		if sCtx.HasSpanID() {
			e.Str(h.SpanField, sCtx.SpanID().String())
		}
	}

	{ // (b) adds logs to the active span as events.

		attrs := make([]attribute.KeyValue, 0)
		logSeverityKey := attribute.Key("log.severity")
		logMessageKey := attribute.Key("log.message")
		attrs = append(attrs, logSeverityKey.String(level.String()))
		attrs = append(attrs, logMessageKey.String(message))

		span.AddEvent("log", trace.WithAttributes(attrs...))
		if level >= zerolog.ErrorLevel {
			span.SetStatus(codes.Error, message)
		}
	}
}
