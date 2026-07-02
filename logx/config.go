package logx

// Config 描述 go-logx 需要的最小日志配置。
// 该结构不绑定任何业务项目的配置模型。
type Config struct {
	// Level 是主日志级别，例如 debug、info、warn、error。
	Level string `mapstructure:"level" yaml:"level" json:"level"`
	// Dir 是日志根目录。
	Dir string `mapstructure:"dir" yaml:"dir" json:"dir"`
	// FileName 用于覆盖应用日志目录名。
	// 如果为空，则使用 Init 传入的 appName。
	FileName string `mapstructure:"fileName" yaml:"fileName" json:"fileName"`
	// ConsoleEnabled 控制主日志是否写入 stdout。
	ConsoleEnabled bool `mapstructure:"consoleEnabled" yaml:"consoleEnabled" json:"consoleEnabled"`
	// MaxSize 是单个日志文件触发滚动的最大大小，单位 MB。
	// 0 表示不启用按大小滚动，使用普通追加文件。
	MaxSize int `mapstructure:"maxSize" yaml:"maxSize" json:"maxSize"`
	// MaxBackups 是滚动历史日志的最大保留数量。
	MaxBackups int `mapstructure:"maxBackups" yaml:"maxBackups" json:"maxBackups"`
	// MaxAge 是滚动历史日志的最大保留天数。
	MaxAge int `mapstructure:"maxAge" yaml:"maxAge" json:"maxAge"`
	// Compress 控制滚动历史日志是否压缩。
	Compress bool `mapstructure:"compress" yaml:"compress" json:"compress"`
	// Sinks 定义独立日志通道，通道名称由使用方自行约定。
	Sinks map[string]SinkConfig `mapstructure:"sinks" yaml:"sinks" json:"sinks"`
}

// Options 是 Config 的别名，供偏好 Options 命名的调用方使用。
type Options = Config

// SinkConfig 描述一个独立日志通道。
type SinkConfig struct {
	// Dir 用于覆盖应用日志目录下的 sink 子目录。
	// 如果为空，则直接使用应用日志目录。
	Dir string `mapstructure:"dir" yaml:"dir" json:"dir"`
	// FileName 用于覆盖 sink 日志文件名。
	// 如果为空，则使用 "<sinkName>.log"。
	FileName string `mapstructure:"fileName" yaml:"fileName" json:"fileName"`
	// ConsoleEnabled 控制当前 sink 是否写入 stdout。
	ConsoleEnabled bool `mapstructure:"consoleEnabled" yaml:"consoleEnabled" json:"consoleEnabled"`
}
