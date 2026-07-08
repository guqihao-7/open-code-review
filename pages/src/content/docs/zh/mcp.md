---
title: MCP 服务器
sidebar:
  order: 10
---

OCR 可以作为 **Model Context Protocol（MCP）客户端**。你把它指向一个或多个外部
MCP server，这些 server 暴露的工具就会提供给审查 agent —— 与 `file_read`、
`code_search` 等[内置工具](../tools/)并列。

## 何时使用

当审查器需要 diff 之外的上下文时，就该引入 MCP server：

- **Issue / 工单查询** —— 让 agent 拉取关联的 Jira / GitHub issue，核对变更是否
  符合声明的需求。
- **文档 / 知识库** —— 拉取内部 API 文档或编码规范，让评论引用真正的团队约定。
- **自定义分析** —— 把 linter、schema 校验器或依赖检查器暴露为工具，供审查器按需
  调用。

如果你只需要读仓库本身，内置工具就够了 —— MCP 是为了触达 checkout 之外的东西。

## 配置

#### 添加 MCP server

`ocr config set` 命令以非交互方式写入这些字段。数组字段（`args`、`env`、`headers`、`tools`）
接受 JSON 数组字符串：

```bash
# 最小 stdio 配置：只给命令
ocr config set mcp_servers.docs.command npx

# 参数
ocr config set mcp_servers.docs.args '["-y", "@acme/docs-mcp-server"]'

# 限制暴露给审查器的工具
ocr config set mcp_servers.docs.tools '["search_docs", "get_page"]'

# server 启动前运行的 setup 命令
ocr config set mcp_servers.docs.setup "npm install -g @acme/docs-mcp-server"

# 环境变量（KEY=VALUE 条目）
ocr config set mcp_servers.docs.env '["DOCS_TOKEN=secret", "DOCS_REGION=eu"]'

# Streamable HTTP server
ocr config set mcp_servers.docs.transport http
# 复制常见 MCP 配置片段时，也可以用 "type" 作为别名：
# ocr config set mcp_servers.docs.type http
ocr config set mcp_servers.docs.url https://docs.example.com/mcp
ocr config set mcp_servers.docs.headers '["Authorization=Bearer ${DOCS_TOKEN}"]'
ocr config set mcp_servers.docs.timeout_sec 180

# 旧版 SSE server
ocr config set mcp_servers.docs.transport sse
ocr config set mcp_servers.docs.url https://docs.example.com/sse
```

#### 移除 MCP server

用 `unset` 移除某个 server：

```bash
ocr config unset mcp_servers.docs
```

MCP server 配置在用户配置文件（`~/.opencodereview/config.json`）的 `mcp_servers` 键下。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `transport` | string | | 默认为 `stdio`，`http` 表示 streamable HTTP，`sse` 表示旧版 SSE。 |
| `type` | string | | `transport` 的别名，适合复用使用 `type: "http"` 的 MCP 配置片段。 |
| `command` | string | 仅 stdio | 启动 MCP server 的可执行文件（如 `npx`、`uvx`、绝对路径）。 |
| `args` | string 数组 | | 传给 stdio `command` 的参数。 |
| `env` | string 数组 | | stdio server 的额外环境变量，`KEY=VALUE` 形式。 |
| `url` | string | 仅 HTTP/SSE | MCP 端点 URL。 |
| `headers` | string 数组 | | HTTP 头，`Header=Value` 形式。值支持 `${DOCS_TOKEN}` 这类环境变量展开。 |
| `tools` | string 数组 | | 要注册的工具名白名单。为空 = 注册该 server 提供的全部工具。 |
| `setup` | string | | server 启动前运行一次的 shell 命令（如安装依赖）。在仓库根目录运行，超时 5 分钟。 |
| `timeout_sec` | integer | | 连接并列出工具的初始化超时时间。默认：`120` 秒。 |
| `disable_standalone_sse` | boolean | | streamable HTTP 下跳过可选的独立 SSE 流。 |

## 过滤工具

默认注册 server 声明的每个工具。当 server 暴露的工具超出审查器所需时，用 `tools`
设一个白名单 —— 更少、更精准的工具能让 agent 更专注，也降低 token 成本。白名单里
server 实际没有提供的名字会被跳过并给出警告，因此拼写错误会显示在 stderr 上，而不是
悄无声息地什么都不做。

## 名称冲突

MCP 工具名与内置工具共享同一个命名空间。如果某个 server 声明的工具名与**内置/保留**
工具（`file_read`、`code_search`、`task_done` 等）冲突，或与另一个 MCP server 已
注册的工具冲突，OCR 会**跳过**它并记录警告。先注册者胜出；为各 server 使用互不相同
的工具名，以免因此丢失工具。

## `setup` 命令

`setup` 在 server 子进程启动前、从仓库根目录运行一次。用它来按需安装或构建 server：

```json
"setup": "npm install -g @acme/docs-mcp-server"
```

它有 **5 分钟超时**。若非零退出，OCR 会记录命令、工作目录和输出，然后跳过该 server
并继续审查。

## 排错

所有 MCP 诊断信息都输出到 **stderr**，以 `[ocr]` 前缀标记，因此绝不会污染 stdout 上
的 `--format json` 输出：

- `Running setup for MCP server "x": …` —— 正在执行 setup 命令。
- `failed to start MCP server "x": …` —— stdio 子进程未在初始化超时内连接成功、
  `command` 不在 `PATH` 中，或 HTTP/SSE 端点拒绝了连接或请求头。
- `tool "y" conflicts with built-in tool, skipping` —— 重命名该 server 的工具，或将其
  从 `tools` 中去掉。
- `allowed tool "y" not found in server's tool list` —— `tools` 中的名字与 server 提供
  的任何工具都不匹配；检查拼写。

## 另见

- [工具](../tools/) —— MCP 工具与之并列的六个内置工具。
- [配置](../configuration/) —— 完整的配置文件与每个键。
- [CLI 参考](../cli-reference/) —— `ocr config` 与 review 参数。
