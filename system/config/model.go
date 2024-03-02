package config

type RootConfig struct {
	Cache      map[string]CacheConfig
	App        AppConfig
	Otel       OtelConfig
	Cdc        CdcConfig
	Stream     StreamConfig
	Auth       AuthConfig
	Poller     PollerConfig
	Reserver   ReserverConfig
	Encryptor  EncryptorConfig
	Enricher   EnricherConfig
	Ktmb       KtmbConfig
	Buyer      BuyerConfig
	Terminator TerminatorConfig
}

// Buyer Config
type BuyerConfig struct {
	BackoffLimit  int
	ContactNumber string

	Scheme string
	Host   string
	Port   string
}

// Terminator Config
type TerminatorConfig struct {
	BackoffLimit int
	QueueName    string
}

// KTMB Config
type KtmbConfig struct {
	ApiUrl           string
	AppUrl           string
	RequestSignature string
	Proxy            *string
}

// Auth Config
type AuthConfig struct {
	Descope DescopeConfig
}

type DescopeConfig struct {
	DescopeId        string
	DescopeAccessKey string
}

// Encryptor
type EncryptorConfig struct {
	Key string
}

// Reserver
type ReserverConfig struct {
	Group                  string
	BackoffLimit           int
	NormalConcurrency      int
	MaintenanceConcurrency int
	NormalAttempts         int
	MaintenanceAttempts    int
}

// Enricher
type EnricherConfig struct {
	Group        string
	BackoffLimit int

	Email    string
	Password string

	UserDataKey string
	StoreKey    string
}

// Poller
type PollerConfig struct {
	Group        string
	BackoffLimit int

	Pollee PolleeConfig
}

type PolleeConfig struct {
	Namespace string
	Image     string
	Version   string
	SecretRef string
	ConfigRef string
}

// Stream
type StreamConfig struct {
	Cdc      string
	Update   string
	Enrich   string
	Reserver string
}

// Cdc
type CdcConfig struct {
	Group string

	BackoffLimit int

	Scheme string
	Host   string
	Port   string

	Parallelism int
}

// Cache
type CacheConfig struct {
	Password  string
	Ssl       bool
	Endpoints map[int]string
}

// App
type AppConfig struct {
	Landscape string
	Platform  string
	Service   string
	Module    string
	Version   string
}

type OtelConfig struct {
	Metric MetricConfig
	Trace  TraceConfig
	Log    LogConfig
}

// Log
type LogConfig struct {
	Zerolog ZeroLogConfig
}

type ZeroLogConfig struct {
	TimeFormat           string
	DurationFieldInteger bool
	LogLevel             string

	Stacktrace bool
	Caller     bool
	Timestamp  bool
	Pretty     bool

	Fields ZeroLogFieldConfig
}

type ZeroLogFieldConfig struct {
	Caller     *string `mapstructure:",omitempty"`
	Timestamp  *string `mapstructure:",omitempty"`
	Error      *string `mapstructure:",omitempty"`
	ErrorStack *string `mapstructure:",omitempty"`
	Level      *string `mapstructure:",omitempty"`
	Message    *string `mapstructure:",omitempty"`
	TraceId    *string `mapstructure:",omitempty"`
	SpanId     *string `mapstructure:",omitempty"`
}

// Trace
type TraceConfig struct {
	Enable    bool
	Processor TraceProcessorConfig
	Exporter  TraceExporterConfig
}

type TraceProcessorConfig struct {
	ProcessorType        string                     // Sync or Batch
	BatchProcessorConfig *TraceBatchProcessorConfig `mapstructure:",omitempty"`
}

type TraceBatchProcessorConfig struct {
	BatchTimeout  *int  `mapstructure:",omitempty"`
	ExportTimeout *int  `mapstructure:",omitempty"`
	Blocking      *bool `mapstructure:",omitempty"`
	BatchSize     *int  `mapstructure:",omitempty"`
	QueueSize     *int  `mapstructure:",omitempty"`
}

type TraceExporterConfig struct {
	ExporterType string                      // OTLP or console
	Otlp         *TraceExporterOTLPConfig    `mapstructure:",omitempty"`
	Console      *TraceExporterConsoleConfig `mapstructure:",omitempty"`
}

type TraceExporterOTLPConfig struct {
	Protocol    string // GRPC or HTTP
	Endpoint    string
	Insecure    *bool              `mapstructure:",omitempty"`
	Headers     *map[string]string `mapstructure:",omitempty"`
	Compression *string            `mapstructure:",omitempty"` // None or gzip
	Timeout     *int               `mapstructure:",omitempty"`
}

type TraceExporterConsoleConfig struct {
	PrettyPrint *bool `mapstructure:",omitempty"`
	Timestamp   *bool `mapstructure:",omitempty"`
}

// Metric
type MetricConfig struct {
	Enable   bool
	Reader   MetricReaderConfig
	Exporter MetricExporterConfig
}

type MetricExporterConfig struct {
	ExporterType string                      // OTLP or console
	Otlp         *TraceExporterOTLPConfig    `mapstructure:",omitempty"`
	Console      *TraceExporterConsoleConfig `mapstructure:",omitempty"`
}

type MetricExporterOTLPConfig struct {
	Protocol    string // GRPC or HTTP
	Endpoint    string
	Insecure    *bool              `mapstructure:",omitempty"`
	Headers     *map[string]string `mapstructure:",omitempty"`
	Compression *string            `mapstructure:",omitempty"` // None or gzip
	Timeout     *int               `mapstructure:",omitempty"`
}

type MetricExporterConsoleConfig struct {
	PrettyPrint *bool `mapstructure:",omitempty"`
	Timestamp   *bool `mapstructure:",omitempty"`
}

type MetricReaderConfig struct {
	Interval *int `mapstructure:",omitempty"`
	Timeout  *int `mapstructure:",omitempty"`
}
