# HTTP Transit - HTTP请求转发程序

一个精简的Go语言HTTP请求转发程序，支持基于配置文件的请求转发和自定义Header处理。

## 功能特性

- 基于Host头和路径前缀的请求转发
- 支持一个域名配置多个路径前缀规则
- 最长前缀匹配路由算法
- 灵活的Header处理：
  - 转发客户端Header
  - 强制设置Header
  - 添加额外Header
  - 删除指定Header
- 支持所有HTTP方法
- 智能DNS解析（支持自定义DNS服务器或直接指定IP）
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
      "/example/api/v1": {
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
      }
    },
    "cdn.example.com": {
      "/files": {
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
}
```

### 3. 启动服务

```bash
./http-transit -config config.json
```

启动时会看到连接池初始化日志：

```
2025-12-02 15:00:00 [INFO] 日志级别设置为: info, 日志文件设置为: http-transit.log
2025-12-02 15:00:00 [INFO] 转发路由: api.example.com/example/api/v1 -> https://api.real-backend.com/api/v1
2025-12-02 15:00:00 [INFO] 转发路由: cdn.example.com/files -> https://cdn.storage-service.com/files
2025-12-02 15:00:00 [INFO] 服务器地址监听: 0.0.0.0:8080
2025-12-02 15:00:00 [INFO] 创建连接池: https://api.real-backend.com (首次使用: api.example.com/example/api/v1)
2025-12-02 15:00:00 [INFO] 创建连接池: https://cdn.storage-service.com (首次使用: cdn.example.com/files)
```

## 配置说明

### 基本配置

- `server`: 服务器配置
  - `port`: 监听端口
  - `public`: 是否公开访问（true=绑定0.0.0.0，false=绑定127.0.0.1）
- `log`: 日志配置（可选）
  - `level`: 日志级别（debug/info/warn/error/dpanic/panic/fatal，默认: info）
  - `file`: 日志文件路径（可选，不设置则只输出到stderr）

### 转发规则配置

- `transit_map`: 转发映射表（域名 → 路径前缀 → 转发规则）
  - `域名`: 匹配的Host头（如: `api.example.com`）
    - `路径前缀`: 匹配的URL路径前缀（如: `/v1`, `/v2`, `/legacy`）
      - `backend_base`: 后端服务器地址（支持http/https）
      - `backend_prefix`: 转发时添加的URL前缀（可为空）
      - `resolve`: DNS解析配置（可选）
        - `dns`: 自定义DNS服务器地址（如: `8.8.8.8`）
        - `ip`: 直接指定后端IP地址（优先级高于dns）
      - `headers`: Header处理配置
        - `forward_client`: 是否转发客户端Header
        - `set`: 强制设置的Header（覆盖客户端的值）
        - `extra`: 添加的额外Header（不覆盖客户端的值）
        - `remove`: 要删除的Header列表（不区分大小写）

### 路径前缀匹配规则

1. **路径前缀必须以 `/` 开头**（如: `/api`, `/v1`, `/`）
2. **最长前缀匹配**：当多个前缀都匹配时，使用最长的那个
   - 示例：请求 `/api/v1/users` 同时匹配 `/api` 和 `/api/v1`，将使用 `/api/v1`
3. **路径剥离与重写**：
   - 匹配的前缀会从请求路径中剥离
   - 剥离后的路径会添加配置的 `backend_prefix`
   - 示例：请求 `/example/api/v1/users` 匹配前缀 `/example/api/v1`
     - 剥离后：`/users`
     - 加上 `backend_prefix` (`/api/v1`)：`/api/v1/users`
     - 最终URL：`https://backend.com/api/v1/users`

## 使用示例

### 单路径前缀转发

假设配置了 `api.example.com` 的 `/example/api/v1` 路径前缀：

```bash
# 访问转发服务
curl -H "Host: api.example.com" http://localhost:8080/example/api/v1/users

# 实际转发到
https://api.real-backend.com/api/v1/users
```

**转发过程：**
1. 匹配路径前缀：`/example/api/v1`
2. 剥离前缀，剩余路径：`/users`
3. 添加 backend_prefix：`/api/v1` + `/users` = `/api/v1/users`
4. 组合最终URL：`https://api.real-backend.com/api/v1/users`

### 多路径前缀路由

一个域名可以配置多个路径前缀，路由到不同的后端：

```json
{
  "transit_map": {
    "api.example.com": {
      "/v1": {
        "backend_base": "https://api-v1.backend.com",
        "backend_prefix": "/api/v1"
      },
      "/v2": {
        "backend_base": "https://api-v2.backend.com",
        "backend_prefix": "/api/v2"
      },
      "/legacy": {
        "backend_base": "https://old-api.backend.com",
        "backend_prefix": ""
      }
    }
  }
}
```

测试不同路径：

```bash
# 访问 v1 API
curl -H "Host: api.example.com" http://localhost:8080/v1/users
# → https://api-v1.backend.com/api/v1/users

# 访问 v2 API
curl -H "Host: api.example.com" http://localhost:8080/v2/posts
# → https://api-v2.backend.com/api/v2/posts

# 访问 legacy API
curl -H "Host: api.example.com" http://localhost:8080/legacy/data
# → https://old-api.backend.com/data
```

### 最长前缀匹配示例

当有多个前缀可以匹配时，使用最长的那个：

```json
{
  "transit_map": {
    "api.example.com": {
      "/api": {
        "backend_base": "https://api.backend.com",
        "backend_prefix": "/v1"
      },
      "/api/v2": {
        "backend_base": "https://api-v2.backend.com",
        "backend_prefix": "/v2"
      }
    }
  }
}
```

```bash
# 匹配 /api/v2（更长的前缀）
curl -H "Host: api.example.com" http://localhost:8080/api/v2/users
# → https://api-v2.backend.com/v2/users

# 匹配 /api（较短的前缀）
curl -H "Host: api.example.com" http://localhost:8080/api/status
# → https://api.backend.com/v1/status
```

### 智能连接池共享

多个路径前缀指向同一个后端主机时，会自动共享连接池：

```json
{
  "transit_map": {
    "api.example.com": {
      "/v1": {
        "backend_base": "https://backend.com",
        "backend_prefix": "/api/v1"
      },
      "/v2": {
        "backend_base": "https://backend.com",
        "backend_prefix": "/api/v2"
      }
    }
  }
}
```

启动时日志显示：
```
[INFO] 创建连接池: https://backend.com (首次使用: api.example.com/v1)
[DEBUG] 复用连接池: api.example.com/v2 -> https://backend.com (池: https://backend.com)
```

两个路径前缀共享同一个连接池，提高连接复用效率。

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

### 按后端主机分离的连接池

- **智能连接池共享**: 连接池按后端主机（`scheme://host:port`）创建，而非按域名或路径
  - 多个域名或路径前缀指向同一后端时，自动共享连接池
  - 例如：`api.example.com/v1` 和 `api.example.com/v2` 都指向 `https://backend.com`，则共享同一个连接池
- **性能隔离**: 不同后端主机使用独立的连接池，避免连接竞争
- **启动时初始化**: 程序启动时为所有后端主机创建连接池，确保运行时性能稳定
- **去重机制**: 自动检测重复的后端主机，避免创建多余的连接池

### 连接池配置

- **连接复用**: 复用TCP连接，减少TLS握手开销
- **每个后端池大小**:
  - 每个后端主机最多 20 个空闲连接
  - 每个后端主机最多 100 个总连接数
- **全局控制**: 最大 100 个全局空闲连接
- **超时控制**:
  - 600 秒请求超时
  - 5 分钟空闲连接超时
- **压缩支持**: 启用HTTP压缩减少传输开销

### 路径前缀路由性能

- **最长前缀匹配**: O(n) 时间复杂度，n 为该域名配置的路径前缀数量
  - 典型场景 n < 20，匹配开销可忽略
- **无运行时锁**: 路由配置在启动时固化，运行时只读访问
- **零拷贝路径处理**: 使用 `strings.TrimPrefix` 高效处理路径剥离

### 启动时初始化优势

- **预热连接**: 服务启动时即建立连接池，首次请求无延迟
- **配置验证**: 启动时验证所有路径前缀和后端配置，及早发现问题
  - 路径前缀必须以 `/` 开头
  - 后端 URL 格式验证
- **资源稳定**: 运行时资源占用稳定，无动态分配开销
- **连接池复用日志**: Debug 模式下可看到连接池共享情况

### 性能监控

启用 debug 日志可以观察连接池使用情况：

```json
{
  "log": {"level": "debug"}
}
```

启动时会显示：
```
[INFO] 创建连接池: https://backend.com (首次使用: api.example.com/v1)
[DEBUG] 复用连接池: api.example.com/v2 -> https://backend.com (池: https://backend.com)
[DEBUG] 连接池配置: https://backend.com (使用DNS: 8.8.8.8)
```

这些优化使得程序在多路径前缀、高并发场景下能够提供更好的性能和稳定性。

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