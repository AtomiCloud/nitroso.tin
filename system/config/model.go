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
	Recoverer  RecovererConfig
	Buffer     BufferConfig
	Pool       PoolConfig
}

// Credential is a single KTMB account login.
type Credential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Pool Config (multi-account userData pool for helium pollee jobs).
// Separate from the reserver/enricher single-session loginer.
type PoolConfig struct {
	// Logins is a JSON array of {email,password}; supplied as one secret via
	// ATOMI_POOL__LOGINS (Viper cannot auto-bind a list-of-structs from env).
	Logins string
	// Key is the Redis HASH (field=email, value=encrypted userData) holding the pool.
	Key string
}

// Buffer Config
type BufferConfig struct {
	// Number of minutes before reserver and poller stops working
	Closing int
}

// Buyer Config
type BuyerConfig struct {
	BackoffLimit  int
	ContactNumber string

	SleepBuffer int

	Scheme string
	Host   string
	Port   string

	// CompleteRetries is how many times the buyer retries reporting a captured
	// ticket to zinc before parking the booking for recovery
	CompleteRetries int
	// ParkRetries is how many times the buyer retries each parking step (the
	// recover-queue push and the Buying -> Recovering transition); the queue
	// push is the sole durable store of a captured ticket's identifiers
	ParkRetries int
	// ConflictPatterns are case-insensitive substrings of KTMB SetPassenger
	// error messages that mean "this passenger already holds a ticket"
	ConflictPatterns []string
}

// Terminator Config
type TerminatorConfig struct {
	BackoffLimit int
	QueueName    string
}

// Recoverer Config
type RecovererConfig struct {
	QueueName string
	// DrainCron is how often the recover queue is drained (robfig/cron syntax,
	// e.g. '@every 15m') — the fast path for freshly-parked bookings.
	DrainCron string
	// SweepCron is how often zinc is reconciled for Recovering bookings whose
	// queue item was lost (e.g. '@every 1h'); each sweep also drains first.
	SweepCron string
	// MaxAttempts is how many drain cycles an item may fail before it is
	// parked as RequireManualIntervention
	MaxAttempts int
}

// KTMB Config
type KtmbConfig struct {
	ApiUrl           string
	AppUrl           string
	RequestSignature string
	LoginKey         string
	Proxy            *string

	// WarmPoolSize is the number of KTMB connections kept warm per host (and the
	// per-host idle pool size). 0 disables the warmer + DNS cache entirely
	// (plain pooled client). Set this only where latency matters (the reserver).
	WarmPoolSize int
	// WarmIntervalMs is how often the warmer re-pings to keep connections hot
	// (default 30000 when WarmPoolSize > 0).
	WarmIntervalMs int
	// DnsRefreshMs is how often the background resolver re-resolves KTMB hosts
	// (default 60000 when WarmPoolSize > 0).
	DnsRefreshMs int
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

	// Delay is the pause (in milliseconds) between launching each per-slot
	// enrichment request. With X-Real-IP rotation defeating the rate limiter
	// this can be ~1ms; it was 16000 (16s) before rotation existed.
	Delay int
}

// Poller
type PollerConfig struct {
	Group        string
	BackoffLimit int

	// ShardSize is the max number of streams (date-direction targets) per helium
	// pod. Targets are chunked into groups of this size, one pod per chunk.
	// <= 0 means no sharding (all targets in a single pod).
	ShardSize int

	// MaxStreams caps the total streams (date-direction targets) polled per run:
	// targets are sorted by date ascending and the first MaxStreams are kept
	// (e.g. 42 = ~3 weeks across both directions). <= 0 means no cap.
	MaxStreams int

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
