package main

import (
	"encoding/json"
	"fmt"
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

type ResolveConfig struct {
	DNS string `json:"dns"`
	IP  string `json:"ip"`
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
	Resolve       ResolveConfig `json:"resolve"`
	Headers       HeadersConfig `json:"headers"`
}

// URL path前缀转发规则
type TransitRoutes map[string]TransitRule

type Config struct {
	Server ServerConfig `json:"server"`
	Log    LogConfig    `json:"log"`

	// URL域名转发
	TransitMap map[string]TransitRoutes `json:"transit_map"`
}

func (r ResolveConfig) String() string {
	if r.IP != "" {
		return fmt.Sprintf("IP: %s", r.IP)
	} else if r.DNS != "" {
		return fmt.Sprintf("DNS: %s", r.DNS)
	}
	return ""
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

	for host, routes := range config.TransitMap {
		for pathPrefix, rule := range routes {
			// 验证路径前缀
			if pathPrefix == "" {
				return nil, fmt.Errorf("空路径前缀 (host: %s)", host)
			}
			if !strings.HasPrefix(pathPrefix, "/") {
				return nil, fmt.Errorf("路径前缀必须以 / 开头 (host: %s, prefix: %s)", host, pathPrefix)
			}

			// 规范化 backend_base URL
			if !strings.HasPrefix(rule.BackendBase, "http://") && !strings.HasPrefix(rule.BackendBase, "https://") {
				rule.BackendBase = fmt.Sprintf("http://%s", rule.BackendBase)
			}

			// 记录路由配置
			if resolveInfo := rule.Resolve.String(); resolveInfo != "" {
				log.Infof("转发路由: %s%s -> %s%s (解析%s)", host, pathPrefix, rule.BackendBase, rule.BackendPrefix, resolveInfo)
			} else {
				log.Infof("转发路由: %s%s -> %s%s", host, pathPrefix, rule.BackendBase, rule.BackendPrefix)
			}

			// 构建 removes 映射以便高效过滤 header
			if len(rule.Headers.Remove) > 0 {
				rule.Headers.removes = make(map[string]struct{})
				for _, remove := range rule.Headers.Remove {
					rule.Headers.removes[strings.ToLower(remove)] = struct{}{}
				}
			}

			// 更新规则到 map（必需，因为我们修改了 rule）
			routes[pathPrefix] = rule
		}
	}

	return &config, nil
}
