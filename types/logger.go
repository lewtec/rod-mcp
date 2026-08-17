package types

import (
	"github.com/aliwatters/rod-mcp/utils"
	"github.com/charmbracelet/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LoggerConfig represents the configuration of the logger
type LoggerConfig struct {
	// Available logger level：
	// "fatal"
	// "error"
	// "warn"
	// "info"
	// "debug"
	LoggerLevel          string `yaml:"loggerLevel" json:"loggerLevel"`
	LoggerFileName       string `yaml:"loggerFileName" json:"loggerFileName"`
	LoggerFileMaxSize    int    `yaml:"loggerFileMaxSize" json:"loggerFileMaxSize"`
	LoggerFileMaxBackups int    `yaml:"loggerFileMaxBackups" json:"loggerFileMaxBackups"`
	LoggerFileMaxAge     int    `yaml:"loggerFileMaxAge" json:"loggerFileMaxAge"`
	LoggerPrefix         string `yaml:"loggerPrefix" json:"loggerPrefix"`
}

var DefaultLoggerConfig = LoggerConfig{
	LoggerLevel:          "info",
	LoggerFileName:       "",
	LoggerFileMaxBackups: 5,
	LoggerFileMaxSize:    50,
	LoggerFileMaxAge:     30,
	LoggerPrefix:         "RodMCP 🦅",
}

// InitLogger init logger
func InitLogger(config LoggerConfig) {
	level, _ := log.ParseLevel(config.LoggerLevel)
	if config.LoggerLevel != "" {
		log.SetLevel(level)
	}
	log.SetTimeFormat(utils.DefaultTimeFormat)
	log.SetReportCaller(true)

	if config.LoggerFileName != "" {
		loggerFile := lumberjack.Logger{
			Filename:   config.LoggerFileName,
			MaxSize:    config.LoggerFileMaxSize,
			MaxAge:     config.LoggerFileMaxAge,
			MaxBackups: config.LoggerFileMaxBackups,
		}

		log.SetOutput(&loggerFile)

	}

	if config.LoggerPrefix != "" {
		log.SetPrefix(config.LoggerPrefix)
	} else {
		log.SetPrefix(DefaultLoggerConfig.LoggerPrefix)
	}
}
