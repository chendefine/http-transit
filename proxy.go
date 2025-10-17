package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/miekg/dns"
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

// resolveHostWithDNS 使用指定的DNS服务器直接查询域名，完全绕过系统hosts文件
func resolveHostWithDNS(host, dnsServer string) (string, error) {
	// 添加默认DNS端口
	if ip := net.ParseIP(dnsServer); ip != nil {
		dnsServer = net.JoinHostPort(dnsServer, "53")
	}

	c := dns.Client{Timeout: 5 * time.Second}

	// 先尝试A记录 (IPv4)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(host), dns.TypeA)
	m.RecursionDesired = true

	r, _, err := c.Exchange(m, dnsServer)
	if err != nil {
		return "", fmt.Errorf("DNS查询失败 (使用 %s): %v", dnsServer, err)
	} else if r.Rcode != dns.RcodeSuccess {
		return "", fmt.Errorf("DNS查询返回错误码: %d (使用 %s)", r.Rcode, dnsServer)
	}

	// 提取A记录
	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			ip := a.A.String()
			log.Debugf("DNS解析: %s -> %s (DNS server: %s)", host, ip, dnsServer)
			return ip, nil
		}
	}

	// 如果没有A记录，尝试AAAA记录 (IPv6)
	m.SetQuestion(dns.Fqdn(host), dns.TypeAAAA)
	r, _, err = c.Exchange(m, dnsServer)
	if err != nil {
		return "", fmt.Errorf("DNS查询IPv6失败 (DNS server: %s): %v", dnsServer, err)
	} else if r.Rcode != dns.RcodeSuccess {
		return "", fmt.Errorf("DNS查询返回错误码: %d (使用 %s)", r.Rcode, dnsServer)
	}

	for _, ans := range r.Answer {
		if aaaa, ok := ans.(*dns.AAAA); ok {
			ip := aaaa.AAAA.String()
			log.Debugf("DNS解析: %s -> %s (DNS server: %s)", host, ip, dnsServer)
			return ip, nil
		}
	}

	return "", fmt.Errorf("未找到IP地址: %s (DNS server: %s)", host, dnsServer)
}

// createIPDialer 创建一个直接使用IP地址连接的拨号器
func createIPDialer(ip string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 提取端口
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// 直接使用配置的IP地址连接
		resolvedAddr := net.JoinHostPort(ip, port)
		d := net.Dialer{Timeout: 30 * time.Second}
		log.Debugf("使用配置的IP地址连接: %s -> %s", addr, resolvedAddr)
		return d.DialContext(ctx, network, resolvedAddr)
	}
}

// createDnsResolvDialer creates a dialer that uses a custom DNS resolver, bypassing system hosts file
func createDnsResolvDialer(dnsServer string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	// Return a custom dialer function
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Extract host and port from addr
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// 如果已经是IP地址，直接连接
		if net.ParseIP(host) != nil {
			d := net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, network, addr)
		}

		// 使用自定义DNS服务器解析，完全绕过系统hosts文件
		ip, err := resolveHostWithDNS(host, dnsServer)
		if err != nil {
			return nil, err
		}

		// 使用解析后的IP地址连接
		resolvedAddr := net.JoinHostPort(ip, port)
		d := net.Dialer{Timeout: 30 * time.Second}
		log.Debugf("使用自定义DNS解析器: %s -> %s", host, resolvedAddr)
		return d.DialContext(ctx, network, resolvedAddr)
	}
}

// 初始化所有域名的连接池
func (p *ProxyHandler) initializeClientPools() {
	for host, rule := range p.config.TransitMap {
		transport := &http.Transport{
			MaxIdleConns:        100,             // 降低全局最大空闲连接数
			MaxIdleConnsPerHost: 20,              // 增加每个主机的最大空闲连接数
			MaxConnsPerHost:     100,             // 增加每个主机的最大连接数
			IdleConnTimeout:     5 * time.Minute, // 空闲连接超时时间
			DisableCompression:  false,           // 启用压缩
		}

		// 按优先级设置拨号器：IP > DNS > 系统默认
		if rule.Resolve.IP != "" {
			// 优先级最高：直接使用IP连接
			transport.DialContext = createIPDialer(rule.Resolve.IP)
		} else if rule.Resolve.DNS != "" {
			// 优先级次之：使用指定DNS服务器
			transport.DialContext = createDnsResolvDialer(rule.Resolve.DNS)
		}
		// 否则使用系统默认解析方式

		client := &http.Client{
			Transport: transport,
			Timeout:   600 * time.Second, // 请求超时时间
		}

		p.clients[host] = client
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	trace := p.forwardRequest(w, r, host)
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

	return headers
}

func (p *ProxyHandler) forwardRequest(w http.ResponseWriter, r *http.Request, host string) *ProxyTrace {
	trace := &ProxyTrace{StartTime: time.Now(), RequestURL: fmt.Sprintf("%s%s", host, r.URL.Path), Method: r.Method, RequestHeaders: r.Header}

	rule, ok := p.config.TransitMap[host]
	if !ok {
		trace.Error = fmt.Errorf("未找到转发规则: %s", host)
		return trace
	}
	client, ok := p.clients[host]
	if !ok {
		trace.Error = fmt.Errorf("服务器未连接: %s", host)
		return trace
	}

	targetURL, err := p.buildTransitBackendURL(rule, r)
	if err != nil {
		trace.Error = fmt.Errorf("构建目标URL失败: %w", err)
		return trace
	}

	trace.BackendURL = targetURL

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		trace.Error = fmt.Errorf("读取请求体失败: %w", err)
		return trace
	}
	defer r.Body.Close()
	trace.RequestBody = reqBody

	req, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		trace.Error = fmt.Errorf("创建请求失败: %w", err)
		return trace
	}

	req.Header = p.processHeaders(r, rule)
	trace.TransitHeaders = req.Header

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
