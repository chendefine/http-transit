package main

import (
	"encoding/json"
	"os"
	"strings"
)

type ServerConfig struct {
	Port   int  `json:"port"`   // 监听端口
	Public bool `json:"public"` // 是否公开访问
}

type LogConfig struct {
	Level string `json:"level"`
	File  string `json:"file"`
}

type HeadersConfig struct {
	Set           map[string]string `json:"set"`
	Extra         map[string]string `json:"extra"`
	Remove        []string          `json:"remove"`
	ForwardClient bool              `json:"forward_client"`

	removes map[string]struct{} `json:"-"`
}

type TransitRule struct {
	BackendBase   string        `json:"backend_base"`
	BackendPrefix string        `json:"backend_prefix"`
	Headers       HeadersConfig `json:"headers"`
}

type Config struct {
	Server     ServerConfig           `json:"server"`
	Log        LogConfig              `json:"log"`
	TransitMap map[string]TransitRule `json:"transit_map"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}

	// 应用日志配置
	if config.Log.Level != "" || config.Log.File != "" {
		level, file := SetLogger(config.Log.Level, config.Log.File)
		if file == "" {
			log.Infof("日志级别设置为: %s", level)
		} else {
			log.Infof("日志级别设置为: %s, 日志文件设置为: %s", level, file)
		}
	}

	for host, rule := range config.TransitMap {
		log.Infof("转发路由: %s -> %s%s", host, rule.BackendBase, rule.BackendPrefix)
		if len(rule.Headers.Remove) > 0 {
			rule.Headers.removes = make(map[string]struct{})
			for _, remove := range rule.Headers.Remove {
				rule.Headers.removes[strings.ToLower(remove)] = struct{}{}
			}
		}
	}

	return &config, nil
}
