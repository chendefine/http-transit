package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

var gzipReaderPool = sync.Pool{New: func() any { return new(gzip.Reader) }}

type ProxyTrace struct {
	StartTime  time.Time
	Duration   time.Duration
	RequestURL string
	BackendURL string
	Method     string
	StatusCode int
	Error      error

	RequestHeaders  http.Header
	TransitHeaders  http.Header
	ResponseHeaders http.Header
	RequestBody     []byte
	ResponseBody    []byte
}

func decompress(body []byte) ([]byte, error) {
	reader := gzipReaderPool.Get().(*gzip.Reader)
	defer gzipReaderPool.Put(reader)

	if err := reader.Reset(bytes.NewReader(body)); err != nil {
		return nil, err
	}

	return io.ReadAll(reader)
}

func (p *ProxyTrace) String() string {
	reqHeaders := make([]string, 0, len(p.RequestHeaders))
	for key, values := range p.RequestHeaders {
		reqHeaders = append(reqHeaders, fmt.Sprintf("%s: %s", key, strings.Join(values, ",")))
	}
	sort.Strings(reqHeaders)
	reqHeaderString := strings.Join(reqHeaders, "; ")

	trsHeaders := make([]string, 0, len(p.TransitHeaders))
	for key, values := range p.TransitHeaders {
		trsHeaders = append(trsHeaders, fmt.Sprintf("%s: %s", key, strings.Join(values, ",")))
	}
	sort.Strings(trsHeaders)
	trsHeaderString := strings.Join(trsHeaders, "; ")

	reqBodyString, reqContentType := "", p.RequestHeaders.Get("Content-Type")
	if strings.Contains(strings.ToLower(reqContentType), "application/json") ||
		strings.Contains(strings.ToLower(reqContentType), "application/x-www-form-urlencoded") ||
		strings.Contains(strings.ToLower(reqContentType), "text/") {

		body := p.RequestBody
		if contentEncoding := p.RequestHeaders.Get("Content-Encoding"); strings.Contains(strings.ToLower(contentEncoding), "gzip") {
			if decompressed, err := decompress(p.RequestBody); err == nil {
				body = decompressed
			} else {
				body = []byte(fmt.Sprintf("[gzip %s %s]", reqContentType, humanize.IBytes(uint64(len(body)))))
			}
		}
		reqBodyString = string(body)

	} else if reqContentType != "" && len(p.RequestBody) > 0 {
		reqBodyString = fmt.Sprintf("[%s %s]", reqContentType, humanize.IBytes(uint64(len(p.RequestBody))))
	}

	rspHeaders := make([]string, 0, len(p.ResponseHeaders))
	for key, values := range p.ResponseHeaders {
		rspHeaders = append(rspHeaders, fmt.Sprintf("%s: %s", key, strings.Join(values, ",")))
	}
	sort.Strings(rspHeaders)
	rspHeaderString := strings.Join(rspHeaders, "; ")

	rspBodyString, rspContentType := "", p.ResponseHeaders.Get("Content-Type")
	if strings.Contains(strings.ToLower(rspContentType), "application/json") ||
		strings.Contains(strings.ToLower(rspContentType), "application/x-www-form-urlencoded") ||
		strings.Contains(strings.ToLower(rspContentType), "text/") {

		body := p.ResponseBody
		if contentEncoding := p.ResponseHeaders.Get("Content-Encoding"); strings.Contains(strings.ToLower(contentEncoding), "gzip") {
			if decompressed, err := decompress(p.ResponseBody); err == nil {
				body = decompressed
			} else {
				body = []byte(fmt.Sprintf("[gzip %s %s]", rspContentType, humanize.IBytes(uint64(len(body)))))
			}
		}
		rspBodyString = string(body)

	} else if rspContentType != "" && len(p.ResponseBody) > 0 {
		rspBodyString = fmt.Sprintf("[%s %s]", rspContentType, humanize.IBytes(uint64(len(p.ResponseBody))))
	}

	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("%s %s -> %s | 耗时: %v | 状态: %d", p.Method, p.RequestURL, p.BackendURL, p.Duration, p.StatusCode))

	if reqHeaderString != "" {
		builder.WriteString(fmt.Sprintf(" | 请求头: %s", reqHeaderString))
	}
	if trsHeaderString != "" && trsHeaderString != reqHeaderString {
		builder.WriteString(fmt.Sprintf(" | 转发头: %s", trsHeaderString))
	}
	if reqBodyString != "" {
		builder.WriteString(fmt.Sprintf(" | 请求体: %s", reqBodyString))
	}
	if rspHeaderString != "" {
		builder.WriteString(fmt.Sprintf(" | 响应头: %s", rspHeaderString))
	}
	if rspBodyString != "" {
		builder.WriteString(fmt.Sprintf(" | 响应体: %s", rspBodyString))
	}

	return builder.String()
}

type ProxyHandler struct {
	config  *Config
	clients map[string]*http.Client
}

func NewProxyHandler(config *Config) *ProxyHandler {
	handler := &ProxyHandler{
		config:  config,
		clients: make(map[string]*http.Client),
	}

	// 启动时为所有配置的域名创建连接池
	handler.initializeClientPools()
	return handler
}

// 初始化所有域名的连接池
func (p *ProxyHandler) initializeClientPools() {
	for _, rule := range p.config.TransitMap {
		if _, ok := p.clients[rule.BackendBase]; ok {
			continue
		}

		domain := p.extractDomain(rule.BackendBase)

		transport := &http.Transport{
			MaxIdleConns:        100,             // 降低全局最大空闲连接数
			MaxIdleConnsPerHost: 20,              // 增加每个主机的最大空闲连接数
			MaxConnsPerHost:     100,             // 增加每个主机的最大连接数
			IdleConnTimeout:     5 * time.Minute, // 空闲连接超时时间
			DisableCompression:  false,           // 启用压缩
		}

		client := &http.Client{
			Transport: transport,
			Timeout:   600 * time.Second, // 请求超时时间
		}

		p.clients[domain] = client
	}
}

// 获取域名的HTTP客户端
func (p *ProxyHandler) getClientForDomain(backendBase string) *http.Client {
	domain := p.extractDomain(backendBase)
	return p.clients[domain]
}

// 从backend_base中提取域名
func (p *ProxyHandler) extractDomain(backendBase string) string {
	if !strings.HasPrefix(backendBase, "http://") && !strings.HasPrefix(backendBase, "https://") {
		backendBase = "http://" + backendBase
	}

	parsedURL, err := url.Parse(backendBase)
	if err != nil {
		return backendBase
	}
	return parsedURL.Host
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	rule, exists := p.config.TransitMap[host]
	if !exists {
		log.Infof("未找到转发规则: %s", host)
		http.Error(w, "转发规则未找到", http.StatusNotFound)
		return
	}

	targetURL, err := p.buildTransitBackendURL(rule, r)
	if err != nil {
		log.Infof("构建目标URL失败: %v", err)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}

	trace := p.forwardRequest(w, r, targetURL, rule)
	trace.Duration = time.Since(trace.StartTime)
	log.Debug(trace)
	if trace.Error != nil {
		log.Warnf("%s %s | 耗时: %v | %s", trace.Method, trace.RequestURL, trace.Duration, trace.Error)
		http.Error(w, trace.Error.Error(), http.StatusInternalServerError)
	} else {
		log.Infof("%s %s | 耗时: %v", trace.Method, trace.RequestURL, trace.Duration)
	}
}

func (p *ProxyHandler) buildTransitBackendURL(rule TransitRule, r *http.Request) (string, error) {
	backendBase := strings.TrimSuffix(rule.BackendBase, "/")
	path := rule.BackendPrefix + r.URL.Path

	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if !strings.HasPrefix(backendBase, "http://") && !strings.HasPrefix(backendBase, "https://") {
		backendBase = "http://" + backendBase
	}

	return backendBase + path, nil
}

func (p *ProxyHandler) processHeaders(r *http.Request, rule TransitRule) http.Header {
	headers := make(http.Header)

	if rule.Headers.ForwardClient {
		for key, values := range r.Header {
			if _, ok := rule.Headers.removes[strings.ToLower(key)]; ok {
				continue
			}
			headers[key] = values
		}
	}

	for key, value := range rule.Headers.Extra {
		if headers.Get(key) == "" {
			headers.Set(key, value)
		}
	}

	for key, value := range rule.Headers.Set {
		if headers.Get(key) != "" {
			headers.Del(key)
		}
		headers.Set(key, value)
	}

	headers.Set("Host", p.extractHost(rule.BackendBase))
	return headers
}

func (p *ProxyHandler) extractHost(backendBase string) string {
	parsedURL, err := url.Parse(backendBase)
	if err != nil {
		return backendBase
	}
	return parsedURL.Host
}

func (p *ProxyHandler) forwardRequest(w http.ResponseWriter, r *http.Request, targetURL string, rule TransitRule) *ProxyTrace {
	trace := &ProxyTrace{StartTime: time.Now(), RequestURL: fmt.Sprintf("%s%s", r.Host, r.URL.Path), BackendURL: targetURL, Method: r.Method, RequestHeaders: r.Header}

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		trace.Error = fmt.Errorf("读取请求体失败: %v", err)
		return trace
	}
	defer r.Body.Close()
	trace.RequestBody = reqBody

	req, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		trace.Error = fmt.Errorf("创建请求失败: %v", err)
		return trace
	}

	req.Header = p.processHeaders(r, rule)
	trace.TransitHeaders = req.Header

	// 使用域名特定的连接池中的HTTP客户端
	client := p.getClientForDomain(rule.BackendBase)
	resp, err := client.Do(req)
	if err != nil {
		trace.Error = fmt.Errorf("转发请求失败: %v", err)
		return trace
	}
	defer resp.Body.Close()
	trace.StatusCode, trace.ResponseHeaders = resp.StatusCode, resp.Header

	rspBody, err := io.ReadAll(resp.Body)
	if err != nil {
		trace.Error = fmt.Errorf("读取响应体失败: %v", err)
		return trace
	}
	trace.ResponseBody = rspBody

	for key, values := range resp.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(resp.StatusCode)

	_, err = w.Write(rspBody)
	if err != nil {
		trace.Error = fmt.Errorf("写入响应体失败: %v", err)
		return trace
	}

	return trace
}
