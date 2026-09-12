# DeepSeek 渠道

文档核对日期：2026-09-12。渠道类型为 **DeepSeek（43）**，API Key 填写 DeepSeek 官方密钥。推荐上游地址 `https://api.deepseek.com`。

## 协议与地址

| 客户端请求网关 | DeepSeek 上游 | 支持方式 |
| --- | --- | --- |
| `POST /v1/chat/completions` | `/chat/completions` | 原生 Chat，流式与非流式 |
| `POST /v1/responses` | `/responses` | 原生 Responses，流式与非流式 |
| `POST /v1/messages` | `/anthropic/v1/messages` | 原生 Anthropic Messages，流式与非流式 |
| `POST /v1/completions` | `/beta/completions` | FIM 补全，流式与非流式 |

渠道地址支持尾部 `/v1`、`/beta`、`/anthropic`、`/anthropic/v1` 以及尾斜杠；网关会归一化这些后缀。反向代理的路径前缀会保留，例如 `https://proxy.example/deepseek/v1` 的 Responses 请求发往 `https://proxy.example/deepseek/responses`。

Chat 中包含 `prefix:true` 或 `tools[].function.strict:true` 时，自动使用 `/beta/chat/completions`；地址明确配置 `/beta` 时，Chat 同样走 Beta。判断使用参数覆盖完成后的实际请求体，包含请求体透传模式。

OpenAI SDK 的客户端 `base_url` 填写网关地址加 `/v1`。Anthropic SDK 的客户端 `base_url` 填写网关根地址，SDK 会补上 `/v1/messages`。这里的客户端配置与渠道内填写的上游地址是两个不同位置。客户端使用 new-api 令牌，网关使用渠道密钥访问 DeepSeek。

## 模型与思考模式

新增 `deepseek-flash`、`deepseek-flash-none`、`deepseek-flash-max`。现有 `deepseek-v4-flash`、`deepseek-v4-pro` 及历史模型名继续保留；是否仍由官方提供服务，以官方模型页面为准。模型目录的增加不会自动配置渠道可用模型、令牌权限或价格。

| 选择 | Chat | Messages | Responses |
| --- | --- | --- | --- |
| 无后缀 | 保留 `thinking` / `reasoning_effort` | 保留 `thinking` / `output_config.effort` | 保留 `reasoning.effort` |
| `-none` | `thinking.type=disabled` | `thinking.type=disabled` | `reasoning.effort=none` |
| `-max` | `thinking.type=enabled`、`reasoning_effort=max` | `thinking.type=enabled`、`output_config.effort=max` | `reasoning.effort=max` |

后缀在渠道模型映射之后解析，并覆盖相应思考设置；渠道参数覆盖仍在其后生效。不带后缀也不传思考参数时，采用官方默认思考模式与强度。官方支持的其他 effort 及兼容映射由上游处理。

Chat 的 `max_completion_tokens` 仅在未提供 `max_tokens` 时映射为 `max_tokens`；二者同时存在时后者优先。显式的 `0`、`false` 不会被当作缺省值丢弃，上游可能按自己的参数范围拒绝它们。

## 示例

以下 JSON 发给对应的网关入口；请求头使用 `Authorization: Bearer <new-api token>`，Messages 也可以使用 `x-api-key: <new-api token>`。

### Chat：图像与工具历史

```json
{
  "model": "deepseek-flash",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "描述这张图片"},
        {"type": "image_url", "image_url": {"url": "https://example.com/image.png", "detail": "low"}}
      ]
    }
  ],
  "thinking": {"type": "enabled"},
  "stream": true,
  "stream_options": {"include_usage": true}
}
```

图片 URL 可替换为 `data:image/png;base64,<图片数据>`。Messages 使用 `image` 内容块和 `source.type=base64/url`；Responses 使用 `input_image`。格式、角色和大小限制以官方图像文档为准，`deepseek-v4-pro` 不支持图像理解。

携带 `tools` 的思考模式多轮请求必须完整回传 assistant 的 `reasoning_content`、`content` 和 `tool_calls`，再追加匹配 `tool_call_id` 的工具结果。网关保留显式空的 `reasoning_content`，不会为缺失的历史生成内容。严格工具 Schema 原样转发，其合法性由上游校验。

### Responses：原生协议

```json
{
  "model": "deepseek-flash-max",
  "instructions": "请简洁回答",
  "input": "你好",
  "stream": true
}
```

DeepSeek 渠道允许只提供 `instructions`。工具调用、明文 reasoning item、工具结果中的图片及官方支持的 `custom` 工具 `apply_patch` 使用原生 Responses 格式。SSE 保留事件类型与 `sequence_number`，以 `response.completed`、`response.incomplete` 或 `response.failed` 结束，不追加 Chat 格式的 `[DONE]`。

### 前缀续写与 FIM

前缀续写使用 `/v1/chat/completions`，最后一条消息设为 `{"role":"assistant","content":"def add(a, b):","prefix":true}`，网关自动选择 Beta 上游。

FIM 使用 `/v1/completions`：

```json
{
  "model": "deepseek-flash",
  "prompt": "def add(a, b):\n    ",
  "suffix": "\n",
  "max_tokens": 128,
  "logprobs": 0,
  "stream": false
}
```

FIM 的 `logprobs` 是整数，Chat 的同名字段是布尔值。FIM 可传 `echo`，但官方不允许 `echo:true` 与 `suffix` 或 `logprobs` 同用。FIM 仅支持非思考补全。

## 能力边界与用量

- Responses 为官方无状态接口：调用方需回传历史；`previous_response_id`、`conversation`、`store`、后台任务等不会由网关补实现，`/responses/compact` 不支持。
- 官方 Responses 对不支持的顶层字段多数静默忽略；`parallel_tool_calls` 不控制实际并行行为。内置 `web_search`、`file_search`、`code_interpreter`、`mcp` 等工具被官方忽略，`custom` 仅支持 `apply_patch`。不要依赖未支持能力。
- 本次不提供 Files 上传、列表、查询和删除，也不提供文件 Key 绑定、文件复用路由或 Gemini 协议转换。图像示例使用 URL/base64。
- 已有请求体透传、参数覆盖、请求头覆盖、强制格式及思考转正文配置继续生效。透传模式绕过请求体改写，包括模型映射和思考后缀转换；此时使用官方模型名和显式参数。
- 用量优先取上游，保留缓存 token 和推理 token，显式零用量不会触发估算。Responses 截断结束也提取最终 usage；没有上游用量时才估算。价格和计费表达式不随此次更新调整。

## 官方参考

- [模型与价格](https://api-docs.deepseek.com/zh-cn/quick_start/pricing)
- [Chat Completions 参数](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion)
- [Responses API](https://api-docs.deepseek.com/zh-cn/guides/responses_api)
- [Anthropic API](https://api-docs.deepseek.com/zh-cn/guides/anthropic_api)
- [思考模式](https://api-docs.deepseek.com/zh-cn/guides/thinking_mode)
- [图像理解](https://api-docs.deepseek.com/zh-cn/guides/vision)
- [工具调用](https://api-docs.deepseek.com/zh-cn/guides/tool_calls)
- [前缀续写](https://api-docs.deepseek.com/zh-cn/guides/chat_prefix_completion)
- [FIM 参数](https://api-docs.deepseek.com/zh-cn/api/create-completion)
