# Rod MCP Server

<div align="center">

<img src="assets/logo2.png" alt="Rod MCP" width="300">

**通过 [Model Context Protocol](https://modelcontextprotocol.io/) 为 AI 代理提供浏览器自动化能力。**

基于 [Rod](https://github.com/go-rod/rod) 构建 — 一个快速、可靠的 Go 语言 Chromium 浏览器控制库。

[![Release](https://img.shields.io/github/v/release/aliwatters/rod-mcp)](https://github.com/aliwatters/rod-mcp/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)

</div>

---

Rod MCP 让 AI 代理（Claude、Cursor 等）拥有完整的浏览器控制能力 — 页面导航、表单填写、按钮点击、截图、生成 PDF 等。支持两种模式：

- **文本模式**（默认）：使用无障碍快照进行结构化、节省 Token 的交互
- **视觉模式**：使用截图配合坐标点击，适用于视觉 AI 模型

## 快速开始

### 安装

```bash
go install github.com/aliwatters/rod-mcp@latest
```

或下载适用于你平台的[预编译二进制文件](https://github.com/aliwatters/rod-mcp/releases)。

### 配置 MCP 客户端

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`)：

```json
{
  "mcpServers": {
    "rod-mcp": {
      "command": "rod-mcp",
      "args": ["--headless", "--no-banner", "--compact-snapshot"]
    }
  }
}
```

配置完成，你的 AI 代理现在可以浏览网页了。

## 工具列表

### 导航

| 工具 | 描述 |
|------|------|
| `rod_navigate` | 导航到指定 URL |
| `rod_go_back` | 浏览器后退 |
| `rod_go_forward` | 浏览器前进 |
| `rod_reload` | 重新加载页面 |

### 页面交互（文本模式）

| 工具 | 描述 |
|------|------|
| `rod_snapshot` | 捕获页面无障碍快照 |
| `rod_click` | 通过快照引用点击元素 |
| `rod_hover` | 悬停在元素上 |
| `rod_fill` | 向输入框输入文本 |
| `rod_selector` | 在下拉菜单中选择选项 |

### 页面交互（视觉模式）

| 工具 | 描述 |
|------|------|
| `rod_vision_click` | 在 x,y 坐标处点击 |
| `rod_vision_fill` | 在坐标处点击并输入文本 |

### 媒体

| 工具 | 描述 |
|------|------|
| `rod_screenshot` | 截取 PNG 格式截图 |
| `rod_pdf` | 生成页面 PDF |

### 浏览器控制

| 工具 | 描述 |
|------|------|
| `rod_evaluate` | 在浏览器中执行 JavaScript |
| `rod_close_browser` | 关闭浏览器 |
| `rod_set_headers` | 设置 HTTP 请求头 |
| `rod_resize` | 设置视口大小和设备模拟 |
| `rod_handle_dialog` | 处理 JavaScript 对话框 |
| `rod_configure` | 运行时切换无头模式或 CDP 端点 |

### 标签页

| 工具 | 描述 |
|------|------|
| `rod_tab_new` | 打开新标签页 |
| `rod_tab_list` | 列出所有标签页 |
| `rod_tab_select` | 切换到指定标签页 |
| `rod_tab_close` | 关闭标签页 |

### 调试

| 工具 | 描述 |
|------|------|
| `rod_wait_for` | 等待选择器或文本出现 |
| `rod_console_messages` | 捕获浏览器控制台输出 |
| `rod_network_requests` | 捕获网络请求 |

### 输入

| 工具 | 描述 |
|------|------|
| `rod_press` | 按下键盘按键 |
| `rod_file_upload` | 上传文件 |

## 配置

### 命令行参数

```
--config, -c       配置文件路径（默认：$XDG_CONFIG_HOME/rod-mcp/rod-mcp.yaml，或 ~/.config/rod-mcp/rod-mcp.yaml）
--headless, -hl    无界面模式运行浏览器
--vision, -vs      启用视觉模式（基于坐标的工具）
--compact-snapshot  压缩快照以减少 Token 使用
--output-dir       截图和 PDF 输出目录
--omit-images      不在响应中包含 base64 图片
--cdp-endpoint     通过 CDP 连接已有浏览器
--no-banner        不显示启动横幅
```

### 配置文件

在 `$XDG_CONFIG_HOME/rod-mcp/rod-mcp.yaml`（或 `~/.config/rod-mcp/rod-mcp.yaml`）创建配置文件（首次运行时会自动在该位置生成）：

```yaml
mode: text                    # text 或 vision
headless: true                # 无界面模式
browserBinPath: ""            # Chrome/Chromium 路径（自动检测）
browserTempDir: ./rod/browser # 浏览器配置目录
noSandbox: false              # 禁用 Chrome 沙箱
proxy: ""                     # 代理 URL（例如 socks5://localhost:1080）
compactSnapshot: false        # 压缩快照以减少 Token
outputDir: ""                 # 截图/PDF 输出目录（默认：系统临时目录）
imageResponses: allow         # allow 或 omit 内联 base64 图片

# 全局注入 HTTP 请求头
extraHTTPHeaders:
  Authorization: "Bearer my-token"

# 为特定域名注入请求头（支持通配符）
domainHeaders:
  "*.example.com":
    X-Custom-Header: "value"
```

### 连接已有浏览器

控制已运行的 Chrome 实例（适用于已认证的会话）：

1. 启动 Chrome 并开启远程调试：
   ```bash
   google-chrome --remote-debugging-port=9222
   ```

2. 连接 rod-mcp：
   ```bash
   rod-mcp --cdp-endpoint http://127.0.0.1:9222
   ```

## Docker

```bash
docker build -t rod-mcp .
docker run -i --rm rod-mcp
```

容器默认以无头模式运行 Chromium。挂载自定义配置：

```bash
docker run -i --rm -v ./rod-mcp.yaml:/app/rod-mcp.yaml:ro rod-mcp
```

## 从源码构建

```bash
git clone https://github.com/aliwatters/rod-mcp.git
cd rod-mcp
go build -o rod-mcp ./cmd/rod-mcp
```

### 前置要求

- Go 1.23+
- Chrome 或 Chromium

## 许可证

MIT - 详见 [LICENSE](LICENSE)
