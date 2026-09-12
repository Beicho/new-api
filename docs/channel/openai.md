# 官方 OpenAI 渠道兼容性

本文说明 new-api 的官方 OpenAI 渠道。核对日期：2026-09-13。模型可用性、参数取值和账户权限由 OpenAI 校验；网关中的模型价格、倍率和计费表达式由管理员配置，本次兼容性更新不会覆盖这些配置。

客户端的 `base_url` 使用网关地址加 `/v1`，认证使用 new-api 令牌；网关使用渠道密钥调用 OpenAI。以下示例中的模型名称须替换为渠道已配置的模型。

## 支持情况

| 接口 | 支持情况 |
| --- | --- |
| `POST /v1/chat/completions` | 普通与流式请求；音频历史、拒绝内容、函数/自定义工具；缓存配置与审核配置 |
| `POST /v1/responses` | 普通与 SSE；可省略 `input`，但网关仍要求 `model` 用于选路；支持 `background:false` |
| `POST /v1/responses/compact` | 沿用已有上下文压缩接口 |
| `GET /v1/realtime` | WebSocket GA，支持服务端 Bearer 和浏览器子协议认证 |
| `POST /v1/images/generations` | JSON；普通与 SSE 图像输出 |
| `POST /v1/images/edits` | JSON 图片引用或 multipart 文件上传；普通与 SSE 输出 |
| `POST /v1/audio/transcriptions` | multipart；普通、说话人标注和流式转写 |
| `POST /v1/audio/translations` | 沿用文件翻译，格式和模型限制由上游校验 |
| `POST /v1/audio/speech` | 内置语音名、自定义语音引用；二进制音频或 `stream_format:sse` |
| Embeddings、Moderations | 沿用现有接口 |

Files、Batch、Responses 查询/取消/删除、Conversations 管理、WebRTC、临时凭据签发及自定义声音创建接口不在本次实现范围。`file_id`、会话 ID、历史响应 ID、自定义声音 ID 必须已存在于被选中的上游项目；网关没有新增资源管理或跨渠道资源迁移能力。依赖这些资源时应使用固定、匹配的渠道凭据。

`background:true` 返回 HTTP 400，因为后台结果查询和延迟结算尚未实现；不会静默改为同步调用。

## 文本参数和缓存

Chat Completions 与 Responses 均透传 `prompt_cache_options` 和 `moderation`。Responses 同时保留 `reasoning.context`、`reasoning.mode`、`reasoning.generate_summary`；新应用优先使用 `reasoning.summary`。

```bash
curl https://gateway.example/v1/responses \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "YOUR_MODEL",
    "input": [{"role":"user","content":[
      {"type":"input_text","text":"可复用的上下文","prompt_cache_breakpoint":{"mode":"explicit"}}
    ]}],
    "prompt_cache_options":{"mode":"explicit","ttl":"30m"},
    "background":false,
    "stream":true
  }'
```

缓存断点必须满足上游模型的最小缓存长度等要求，上例只展示请求格式。开启 Chat 转 Responses 策略后，可等价映射的缓存配置、内容块断点、工具定义、结构化输出和流选项会保留；音频输入/输出、音频历史和旧式 `function_call` 等无法等价表示的请求返回兼容性错误，需改用原生 Chat Completions。

可选数值和布尔字段区分缺省与显式 `0`/`false`。例如图像的 `n:0` 原样交给上游校验，不会自动改为 1；省略 `n` 时不主动添加该字段。

现有渠道字段控制继续生效：

| 渠道设置 | 默认行为 |
| --- | --- |
| `allow_service_tier` | 关闭时过滤 `service_tier` |
| `allow_safety_identifier` | 关闭时过滤请求身份标识；Realtime 的对应请求头也遵循此开关 |
| `disable_store` | 默认不禁用；开启后沿用既有存储字段过滤策略 |
| `allow_include_obfuscation` | 关闭时过滤 `stream_options.include_obfuscation`，让上游使用默认保护 |

强制请求流式 usage 只设置 `include_usage`，不再覆盖整个 `stream_options`。原有整包透传模式、参数覆盖和模型映射的优先级保持原有约定：整包透传会绕过通常的请求转换和模型名称修改。

## 图像编辑与流式媒体

JSON 编辑采用 `images` 数组，每项是 `image_url`（普通 URL 或 data URL）或 `file_id` 引用；`mask` 使用同样的引用结构。

```bash
curl https://gateway.example/v1/images/edits \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"YOUR_IMAGE_MODEL",
    "prompt":"将背景改成雪山",
    "images":[{"image_url":"https://example.com/source.png"}],
    "input_fidelity":"high",
    "stream":true,
    "partial_images":0
  }'
```

multipart 编辑仍支持 `image`、`image[]` 和 `mask` 文件，保留其他表单字段。

```bash
curl https://gateway.example/v1/audio/transcriptions \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -F model=YOUR_TRANSCRIPTION_MODEL \
  -F file=@recording.wav \
  -F stream=true
```

说话人标注模型可传 `response_format=diarized_json`、`chunking_strategy`、`known_speaker_names[]` 和 `known_speaker_references[]`；是否支持由上游模型决定。

语音生成可使用 `"voice":"alloy"`，也可使用 `"voice":{"id":"voice_123"}`。自定义语音引用仅用于官方 OpenAI 渠道，仍需上游账户具有对应权限。语音生成的 SSE 开关是 `"stream_format":"sse"`。

媒体 SSE 逐帧刷新，保留 `event`、`id`、多行 `data`、未知事件和字段。异常、断流与客户端取消会结束当前请求并记录状态；流已开始后不会重试或在流尾追加普通 JSON 错误体。完整帧大小沿用 `STREAM_SCANNER_MAX_BUFFER_MB` 配置。

## Realtime Beta 迁移

OpenAI 已移除 Realtime Beta。官方渠道显式携带 `OpenAI-Beta: realtime=v1` 或 `openai-beta.realtime-v1` 子协议时，在 WebSocket 升级前返回 `realtime_beta_not_supported`。

客户端迁移步骤：

1. 删除 Beta 请求头及子协议，连接网关 `/v1/realtime?model=YOUR_REALTIME_MODEL`。
2. 在 `session.update` 中指定 `session.type:"realtime"`，将输入/输出音频配置移到 `session.audio.input` 和 `session.audio.output`。
3. 处理 GA 事件，包括 `response.output_audio.delta`、`response.output_audio_transcript.delta` 和 `response.output_text.delta`。
4. 服务端使用 new-api Bearer 令牌；浏览器 WebSocket 可使用 `realtime`、`openai-insecure-api-key.<new-api令牌>` 子协议。浏览器应使用受限令牌，不得放置上游渠道密钥。

网关保留 GA 事件内容，使用渠道凭据连接上游。Azure 和其他渠道的协议行为保持不变；本次没有实现 Beta/GA 事件互转。

## 用量与计费

- `cache_write_tokens` 与旧 `cached_creation_tokens` 作为同一缓存创建量的两种表示处理；前者存在时优先，包括显式零值，不相加。
- 缓存读写用量进入现有倍率计费、表达式计费和日志。表达式使用 `cc` 时，从 `p` 中排除这部分量；未使用 `cc` 时，继续按既有基础输入定价语义处理。`len` 保持完整输入长度。
- 网页搜索和文件搜索按实际完成的调用计数；只有工具声明而没有调用时不收取调用费用。流式 item 事件与最终输出快照按调用 ID 去重。
- 图像、音频采用上游报告的 token usage；未报告 token usage（包括只报告时长）时使用现有本地估算，并记录提示。
- Realtime 优先使用每次 `response.done.usage`，同一 response ID 不重复结算；缺失时才使用本地估算。正式 usage 不与这次响应的估算叠加。

## 官方资料

- [Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
- [Responses](https://developers.openai.com/api/reference/resources/responses/methods/create)
- [Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)
- [图像生成](https://developers.openai.com/api/reference/resources/images/methods/generate) · [图像编辑](https://developers.openai.com/api/reference/resources/images/methods/edit)
- [文件转写](https://developers.openai.com/api/docs/guides/speech-to-text) · [语音生成](https://developers.openai.com/api/reference/resources/audio/subresources/speech/methods/create)
- [Realtime GA 迁移](https://developers.openai.com/api/docs/guides/realtime#beta-to-ga-migration) · [WebSocket 连接](https://developers.openai.com/api/docs/guides/voice-websockets?api=realtime)
