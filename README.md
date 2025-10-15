# HTTP Transit - HTTP请求转发程序

一个精简的Go语言HTTP请求转发程序，支持基于配置文件的请求转发和自定义Header处理。

## 功能特性

- 基于Host头的请求转发
- 支持URL路径前缀
- 灵活的Header处理：
  - 转发客户端Header
  - 强制设置Header
  - 添加额外Header
  - 删除指定Header
- 支持所有HTTP方法
- 详细的请求追踪和日志记录

## 快速开始

### 1. 编译程序

```bash
go build -o http-transit
```

### 2. 配置文件

编辑 `config.json` 文件：

```json
{
  "server": {
    "port": 8080,
    "public": true
  },
  "log": {
    "level": "info",
    "file": "http-transit.log"
  },
  "transit_map": {
    "api.example.com": {
      "backend_base": "https://api.real-backend.com",
      "backend_prefix": "/api/v1",
      "headers": {
        "forward_client": true,
        "set": {
          "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
        },
        "extra": {
          "X-Extra-Header": "extra-value"
        },
        "remove": ["Authorization"]
      }
    },
    "cdn.example.com": {
      "backend_base": "https://cdn.storage-service.com",
      "backend_prefix": "/files",
      "headers": {
        "forward_client": false,
        "set": {
          "X-CDN-Token": "your-cdn-token-here"
        },
        "extra": {},
        "remove": ["Cookie", "Authorization"]
      }
    }
  }
}
```

### 3. 启动服务

```bash
./http-transit -config config.json
```

启动时会看到连接池初始化日志：

```                                                               [1s]
2025-10-16 00:42:38 [INFO] 日志级别设置为: info, 日志文件设置为: http-transit.log
2025-10-16 00:42:38 [INFO] 转发路由: api.example.com -> https://api.real-backend.com/api/v1
2025-10-16 00:42:38 [INFO] 转发路由: cdn.example.com -> https://cdn.storage-service.com/files
2025-10-16 00:42:38 [INFO] 服务器地址监听: 0.0.0.0:8080
```

## 配置说明

- `server`: 服务器配置
  - `port`: 监听端口
  - `public`: 是否公开访问（true=绑定0.0.0.0，false=绑定127.0.0.1）
- `log`: 日志配置（可选）
  - `level`: 日志级别（debug/info/warn/error/dpanic/panic/fatal，默认: info）
  - `file`: 日志文件路径（可选，不设置则只输出到stderr）
- `transit_map`: 转发映射表
  - `key`: 转发的域名（Host头）
  - `backend_base`: 目标服务器地址
  - `backend_prefix`: 转发时添加的URL前缀
  - `headers`: Header处理配置
    - `forward_client`: 是否转发客户端Header
    - `set`: 强制设置的Header（覆盖客户端的值）
    - `extra`: 添加的额外Header（不覆盖客户端的值）
    - `remove`: 要删除的Header列表

## 使用示例

### 单域名转发
假设配置了 `api.example.com` 转发到 `https://api.real-backend.com/api/v1`：

```bash
# 访问转发服务
curl -H "Host: api.example.com" http://localhost:8080/users

# 实际转发到
https://api.real-backend.com/api/v1/users
```

### 多域名独立连接池
当配置多个域名时，每个域名会有独立的连接池：

```bash
# API请求 - 使用api.real-backend.com的连接池
curl -H "Host: api.example.com" http://localhost:8080/data

# CDN请求 - 使用cdn.storage-service.com的连接池
curl -H "Host: cdn.example.com" http://localhost:8080/images/logo.png
```

这样不同域名的请求不会互相影响，提供更好的性能隔离。

## 命令行参数

- `-config`: 配置文件路径（默认: config.json）

## 技术特点

- 纯Go实现，使用uber-go/zap结构化日志
- 高性能HTTP转发
- HTTP连接池优化，减少连接建立开销
- 精简代码，易于维护
- 完整的错误处理和请求追踪

## 性能优化

程序内置了智能的HTTP连接池来提升转发性能：

### 按域名分离的连接池

- **性能隔离**: 每个后端域名使用独立的连接池，避免不同域名间的连接竞争
- **启动时初始化**: 程序启动时为所有配置的域名创建连接池，确保运行时性能稳定
- **无需并发控制**: 连接池在启动时完成初始化，运行时无需加锁，性能更优

### 连接池配置

- **连接复用**: 复用TCP连接，减少握手开销
- **每个域名池大小**: 每个域名最多20个空闲连接，100个总连接数
- **全局控制**: 最大100个全局空闲连接
- **超时控制**: 600秒请求超时，300秒空闲连接超时
- **压缩支持**: 启用HTTP压缩减少传输开销

### 启动时初始化优势

- **预热连接**: 服务启动时即建立连接池，首次请求无延迟
- **配置验证**: 启动时验证所有后端域名配置，及早发现问题
- **资源稳定**: 运行时资源占用稳定，无动态分配开销
- **简洁代码**: 无需复杂的懒加载和并发控制逻辑

这些优化使得程序在多域名、高并发场景下能够提供更好的性能和稳定性。

## 调试和诊断

### 启用详细日志

在配置文件中设置日志级别为 `debug` 可以看到详细的请求追踪信息：

```json
{
  "log": {
    "level": "debug",
    "file": "http-transit.log"
  }
}
```

Debug日志包含：
- 完整的请求和响应头
- 请求和响应体内容（文本类型）
- 二进制内容的大小和类型信息
- 精确的请求耗时

### 测试命令

```bash
# 基础转发测试
curl -H "Host: api.example.com" http://localhost:8080/some/path

# 测试Header转换（verbose模式）
curl -v -H "Host: api.example.com" http://localhost:8080/test

# 测试POST请求
curl -X POST -H "Host: api.example.com" -H "Content-Type: application/json" \
  -d '{"key":"value"}' http://localhost:8080/api/data
```