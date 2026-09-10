# Agnes AI 渠道接入

依据 [官方文档索引（28 篇）](https://wiki.agnes-ai.com/llms.txt)，核对日期：2026-09-10。

## 渠道配置与模型

选择 **Agnes AI**（渠道编号仍为 58），将 Agnes 密钥填入渠道密钥栏。上游地址使用 `https://apihub.agnes-ai.com`，也接受 SDK 常用的 `https://apihub.agnes-ai.com/v1`。客户端使用网关地址和网关令牌。

| 能力 | 模型 |
| --- | --- |
| 文本 | `agnes-2.5-flash`、`agnes-2.5-pro-beta`、`agnes-2.5-pro`、`agnes-3.0-flash` |
| 图片 | `agnes-image-2.0-flash`、`agnes-image-2.1-flash`、`agnes-image-2.5-flash` |
| 视频 | `agnes-video-v2.0`、`agnes-video-2.5`、`agnes-video-2.5-flash` |

保留旧模型 `agnes-1.5-flash`、`agnes-2.0-flash` 及原有模型映射。官方已将旧文本模型列为历史接口，新接入建议选择 2.5 或 3.0；旧模型能否调用以账户和上游实际可用性为准。升级不会自动重命名模型，也不会把旧模型自动映射到新模型。管理员配置的别名按最终映射后的上游模型校验视频参数；别名价格遵循现有模型映射计费设置。

## 文本

三个原生协议均可使用，工具调用、多模态内容及对应协议的 Thinking 参数保留：

```http
POST /v1/chat/completions
Authorization: Bearer <网关令牌>
Content-Type: application/json

{
  "model": "agnes-2.5-flash",
  "messages": [{"role": "user", "content": "用一句话介绍你自己"}],
  "temperature": 0,
  "stream": true,
  "stream_options": {"include_usage": true},
  "chat_template_kwargs": {"enable_thinking": false}
}
```

```http
POST /v1/responses
Authorization: Bearer <网关令牌>
Content-Type: application/json

{"model":"agnes-2.5-flash","input":"你好","max_output_tokens":128}
```

```http
POST /v1/messages
x-api-key: <网关令牌>
anthropic-version: 2023-06-01
Content-Type: application/json

{
  "model": "agnes-2.5-flash",
  "max_tokens": 2048,
  "thinking": {"type":"enabled","budget_tokens":1024},
  "messages": [{"role":"user","content":"解释一下光合作用"}]
}
```

Messages 向上游发送 `x-api-key` 和 `anthropic-version`，按 Claude 协议处理响应和缓存用量。2026-09-10 实测 Chat 流式请求支持 `stream_options.include_usage`，因此已启用；省略该选项仍可使用普通流式输出。模型对 Thinking 的具体支持范围及预算限制以各自文档为准。

## 图片

生成接口为 `POST /v1/images/generations`。2.1 和 2.5 Flash 推荐使用 `size: "1K" | "2K" | "3K" | "4K"` 和 `ratio`；也保留 `1024x768` 等旧尺寸写法，上游可能归一化尺寸。2.0 建议使用像素尺寸。未传尺寸时，2.0 默认 `1024x1024`，其余模型默认 `1K`。

支持比例：`1:1`、`3:4`、`4:3`、`16:9`、`9:16`、`2:3`、`3:2`、`21:9`。

```json
{
  "model": "agnes-image-2.5-flash",
  "prompt": "将输入图片改为水彩风格",
  "size": "2K",
  "ratio": "16:9",
  "n": 1,
  "return_base64": false,
  "image": ["https://example.com/source.png"]
}
```

`image` 可以是单个 URL、URL 数组或 `data:image/png;base64,...` 形式的 Data URI。也接受 `extra_body.image`；同时提供时顶层 `image` 优先，发送上游时统一放进 `extra_body.image`。纯文生图省略图片字段。

`return_base64: true` 请求 `data[].b64_json`，否则返回 `data[].url`。原有 `response_format` 会继续转换到 `extra_body.response_format`。建议使用官方 `return_base64`，避免同时设置相互矛盾的输出格式。

`POST /v1/images/edits` 接受同样的 JSON 图片输入，转发到上游生成接口；缺少图片或 `n` 不等于 1 会返回 400。文件上传形式的编辑不受支持。

## 视频

创建路径保持 `POST /v1/videos`。2.5 系列必须指定 `mode`：

| 模式 | 必需媒体 | 不可混用 |
| --- | --- | --- |
| `text` | 无 | 首尾帧和参考媒体 |
| `keyframe` | `first_frame`、`last_frame` 至少一个 | `images`、`audios`、`videos` |
| `reference` | 非空 `images`、`audios`、`videos` 至少一种 | `first_frame`、`last_frame` |

```json
{
  "model": "agnes-video-2.5",
  "prompt": "镜头缓慢推进，一颗蓝色小球在白色地面滚动",
  "mode": "text",
  "seconds": "5",
  "size": "720P",
  "aspect_ratio": "16:9",
  "seed": 0
}
```

首尾帧示例：

```json
{
  "model": "agnes-video-2.5-flash",
  "prompt": "从首帧自然过渡到尾帧",
  "mode": "keyframe",
  "seconds": "5",
  "size": "720P",
  "first_frame": "https://example.com/first.png",
  "last_frame": "https://example.com/last.png"
}
```

参考媒体示例：

```json
{
  "model": "agnes-video-2.5",
  "prompt": "使用 <Picture 1> 的角色、<Audio 1> 的节奏和 <Video 1> 的动作",
  "mode": "reference",
  "seconds": "8",
  "size": "1080P",
  "images": ["https://example.com/character.png"],
  "audios": ["https://example.com/music.mp3"],
  "videos": [{"url":"https://example.com/motion.mp4","start_seconds":0,"require_audio":false}]
}
```

`seconds` 为字符串整数 `"4"` 至 `"12"`，默认 `"5"`；`n` 只能为 1。2.5 支持 `720P`、`1080P`、`1K`、`2K`，Flash 仅支持 `720P`。`aspect_ratio` 支持 `21:9`、`16:9`、`4:3`、`1:1`、`3:4`、`9:16`。不要传像素尺寸或 `width`、`height`、`fps`、`num_frames` 等不可配置字段。

2.5 最多 8 张图片、3 段音频、1 段视频，总数最多 12；Flash 最多 5 张图片、3 段音频，不支持参考视频。媒体必须使用 Agnes 可访问且在任务结束前有效的 HTTP(S) URL。网关保留 `seed=0`、`start_seconds=0`、`require_audio=false`。直接 HTTP 客户端也可将扩展参数写在 `extra_body`，顶层同名字段优先。

旧 v2.0 继续支持 `num_frames` 和 `frame_rate`，不使用 `seconds`。帧数需为正数、满足 `8n+1` 且不超过 441；帧率为 1–60。例如：

```json
{"model":"agnes-video-v2.0","prompt":"海边日落","num_frames":121,"frame_rate":24,"seed":0}
```

### 查询与失败处理

客户端保存创建响应的公开 `id`，通过 `GET /v1/videos/{id}` 查询。新任务内部持久化上游 `video_id` 和映射后的模型名，查询 `/agnesapi?video_id=…&model_name=…`；服务重启后仍能继续轮询。未保存 `video_id` 的旧 v2.0 任务继续使用旧查询路径，无需迁移数据库列。

成功后从 `metadata.url` 读取结果；`size`、`seconds`、`metadata.size_mapping` 保留上游提供的实际值。上游可能只返回尺寸档位；网关不下载视频推测像素尺寸。顶层或嵌套的历史结果格式均兼容。

实时查询与后台轮询共用状态更新及结算流程，并发更新只允许一个终态变更执行退款或结算。失败、超时沿用现有退款及违规收费适用规则；429、临时网络错误和上游 5xx 保留任务，等待再次轮询。最终错误是否向客户端展示，遵循渠道现有的错误详情设置。

## 内置默认价格

以下是网关默认标准价，管理员可自定义，分组倍率仍按现有设置生效。

| 文本模型 | 输入／百万 token | 输出／百万 token | 缓存读取／百万 token |
| --- | ---: | ---: | ---: |
| 2.5 Flash、3.0 Flash | $0.05 | $0.15 | $0.005 |
| 2.5 Pro Beta | $0.10 | $0.30 | $0.01 |
| 2.5 Pro | $0.45 | $0.90 | $0.045 |

| 图片／视频模型 | 每次成功调用／任务 |
| --- | ---: |
| 图片 2.0、2.1、2.5 Flash | $0.01 |
| 视频 v2.0 | $0.025 |
| 视频 2.5、2.5 Flash | $0.125 |

图片和视频固定按次收费，不叠加时长、分辨率和参考媒体倍率，也不为计费下载参考视频。视频默认价按已确认的 5 秒标准价折算；这是网关收费规则，上游仍可能按秒、分辨率和输入媒体计费，高分辨率或长视频的上游成本可能高于网关默认收费。

升级只补齐已保存配置中缺失的 Agnes 默认项；管理员已保存的价格（包括显式 0）保持有效。已有 v2.0 自定义单价不会被自动修改，管理员可根据新的按次含义自行调整。历史任务保留提交时的价格及倍率快照。

官方价格页列有临时活动价，例如部分 Flash 文本、图片、v2.0 和 2.5 Flash 视频目前免费。活动随上游调整，不写入网关默认价格，详见 [官方价格页](https://agnes-ai.com/en/docs/pricing)。

## 验证

回归测试：

```sh
go test -mod=readonly ./relay/channel/agnes ./relay/channel/task/agnes ./relay ./relay/helper ./relay/common ./service ./model ./setting/ratio_setting
```

真实测试须显式设置环境变量 `AGNES_LIVE_TEST=1`、`AGNES_API_KEY`、`AGNES_LIVE_SUITE=text|images|videos`，再执行 `go test ./relay/channel/agnes -run '^TestAgnesLive$' -count=1 -v -timeout 28m`。每个 suite 仅发送有限次请求，不自动重试创建；使用前确认上游最新价格与账户预算。也可使用 `retrieve` suite 配合 `AGNES_LIVE_RETRIEVALS`（含 `model`、`video_id` 的 JSON 数组）检查已有任务，不产生新生成费用。

2026-09-10 验证结果：Chat、Responses、Messages 和 Chat 流式用量通过；图片 2.5 Flash 的 URL／Base64 返回通过；视频 v2.0、2.5 Flash 创建、轮询完成和结果地址解析通过。视频 2.5 创建返回 `403 insufficient_user_quota`（余额为零），无法验证生成全过程。首尾帧和多媒体参考模式通过本地参数及转发测试，未发起真实生成。真实调用未超过已授权的 $1 上限。
