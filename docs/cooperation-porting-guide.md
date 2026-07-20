# UL/DL Cooperation 与 AP Container 移植指南

本文档说明如何把当前 AMF 分支中的 UL/DL Cooperation 和 AP Container 分片能力移植到另一个接近 free5GC 原版的 AMF。该能力是本地 NAS/GMM 扩展，不属于原版 free5GC 或 3GPP 标准消息。

## 1. 功能边界

新增两个 NAS 消息和一个 AP Container IE：

```text
UL Cooperation message type = 0xe1
DL Cooperation message type = 0xe2
AP Container IEI            = 0x71
```

当前流程由 UE 发起。UE 注册完成后发送受 NAS 安全保护的 UL Cooperation，AMF 解密、解析和处理各个 IE；需要响应时，AMF 通过 NGAP DownlinkNASTransport 返回一条或多条 DL Cooperation。默认配置会把重组完成的 AP Payload 解析为完整 Intent JSON，并将原始 AP Payload 作为 HTTP body 发送给 NAgent；NAgent JSON 响应原样封装为 `ContainerType=0x0101` 的 DL AP Container。当前完整运行流程见 `docs/ap-intent-nagent-flow.md`。

AMF 不会仅因为 UE 注册完成而主动发送 DL Cooperation。NAgent 路径没有新增 AP 层确认或 AMF paging；HTTP 失败使用本文第 14 节定义的 JSON 错误载荷返回给 UE。

## 2. Cooperation 外层消息

UL/DL Cooperation 的固定头为 4 字节：

```text
Offset  Length  Field
0       1       EPD, 通常为 0x7e
1       1       SecurityHeaderType，plain body 中为 0x00
2       1       MessageType，UL=0xe1，DL=0xe2
3       1       MessageIdentity
4       ...     独立的可选 IE TLV 列表
```

同一条消息中的 IE 相互独立，可以同时出现。每条消息最多包含一个 `0x71`。

当前代码保留两种外层 IE length 编码：

```text
MessageIdentity == 0x01:
  IEI(1) + Length(1, uint8) + Value(Length)

MessageIdentity != 0x01:
  IEI(1) + Length(2, uint16, big-endian) + Value(Length)
```

这里的 legacy 只表示外层 length 仍可使用 2 字节。无论使用哪一种外层格式，`0x71` 的 Value 都必须是本文第 4 节定义的新 AP Container 格式。旧的 4-byte `ContainerContent` 内部格式不再兼容。

## 3. IEI 与处理行为

UL 支持的已知 IE：

```text
0x10  存入 NegotiatedIEs，并在 DL 中返回同值 0x10
0x18  存入 NegotiatedIEs，但不生成 DL 0x18
0x71  作为 AP Container 解码、重组和保存；启用 NAgent 时异步调用 HTTP 并用响应生成 DL 分片
```

DL 只允许：

```text
0x10
0x71
```

未知 UL IE 会在 NAS 层保留，GMM 层记录日志后忽略。DL builder 遇到 `0x18`、未知 IE 或同一条 DL 中多个 `0x71` 时返回错误。

普通 IE 与 AP Container 的状态更新相互独立，但启用 NAgent 时 DL 发送需要等待 HTTP 事务完成：

- AP 分片不完整、格式错误或重组失败，不会阻止同一条 UL 中的 `0x10/0x18` 生效。
- 有效 AP 分片尚未重组完成时，`0x10` 的 DL 响应保存在重组状态中；HTTP 成功、明确失败或达到 3 秒总截止时间后再下发。
- 不含 `0x71`、`0x71` 无效或 NAgent 被禁用时，普通 `0x10` 仍可立即响应。
- `0x71` 不写入 `NegotiatedIEs`，完整载荷保存在专用的 completed AP Container 记录中。

## 4. AP Container 内部格式

### 4.1 字节布局

IE `0x71` 的 Value 使用固定 10 字节头：

```text
Offset  Length  Field
0       2       ContainerType, uint16
2       2       ContainerContentLength, uint16
4       1       ContainerTypePTI, uint8
5       2       ContainerPayloadId, uint16
7       1       ContainerFlags, uint8
8       2       FragmentOffset, uint16
10      N       Payload
```

所有多字节字段均为大端序。`FragmentOffset` 的单位是字节。

长度关系为：

```text
ContainerContentLength = 6 + len(Payload)
APContainerValueLength = 10 + len(Payload)
CooperationOuterLength = APContainerValueLength
```

编码器自动计算 `ContainerContentLength`。解码器要求 Value 至少 10 字节，并严格检查内部 length 与实际字节数相等。

### 4.2 ContainerPayloadId

`ContainerPayloadId` 是 2 字节无符号整数，用于标识一个完整载荷并关联其所有分片，`0x0000` 也是合法值。

重组键的作用域是：

```text
UE + Direction + ContainerPayloadId
```

不同 UE 或不同方向可以同时使用同一个 ID。同一 UE、同一方向中的未完成载荷不能复用 ID；完成、失败或超时清理后可以复用。

### 4.3 ContainerFlags

```text
Bit mask  Name  Meaning
0x02      DF    0=允许分片，1=禁止分片
0x04      MF    0=最后一片，1=后面还有分片
```

其余位为 reserved，接收端发现任一 reserved bit 非零时拒绝该 AP Container。

约束：

```text
DF=1 -> MF 必须为 0，FragmentOffset 必须为 0
DF=0 -> 既可以是分片，也可以是 offset=0、MF=0 的完整消息
空 Payload -> MF 必须为 0，FragmentOffset 必须为 0
```

### 4.4 FragmentOffset 与载荷上限

`FragmentOffset` 表示当前 Payload 在完整应用载荷中的起始字节位置：

```text
片 1: offset=0,   payload=245 bytes, MF=1
片 2: offset=245, payload=55 bytes,  MF=0
完整长度: 300 bytes
```

每片必须满足：

```text
FragmentOffset + len(Payload) <= 65535
```

完整重组载荷上限也是 65535 字节。

## 5. 具体编码示例

### 5.1 不分片 AP Container

字段值：

```text
ContainerType      = 0x0100
Payload            = aa bb
ContainerTypePTI   = 0x05
ContainerPayloadId = 0x1234
DF=1, MF=0
FragmentOffset     = 0
```

AP Container Value：

```text
01 00  00 08  05  12 34  02  00 00  aa bb
|type| |len | |PTI| | ID | |FL| |off| |data|
```

`ContainerContentLength=0x0008`，即固定内部字段 6 字节加 Payload 2 字节；整个 AP Value 长 12 字节，即 `0x0c`。

包含 `0x10/0x18/0x71` 的 UL plain NAS：

```text
7e 00 e1 01
10 01 01
18 01 01
71 0c 01 00 00 08 05 12 34 02 00 00 aa bb
```

对应 DL plain NAS：

```text
7e 00 e2 01
10 01 01
71 0c 01 00 00 08 05 12 34 02 00 00 aa bb
```

DL 不包含 `0x18`。

### 5.2 300 字节分片示例

`MessageIdentity=0x01` 的外层 length 只有 1 字节。AP 头占 10 字节，因此每个 DL 分片最多携带 245 字节 Payload，使 `0x71` Value 总长恰好 255。

首片头：

```text
71 ff
01 00       ContainerType
00 fb       ContainerContentLength = 6 + 245
05          PTI
12 34       PayloadId
04          DF=0, MF=1
00 00       Offset=0
<245 bytes payload>
```

末片头：

```text
71 41
01 00       ContainerType
00 3d       ContainerContentLength = 6 + 55
05          PTI
12 34       PayloadId
00          DF=0, MF=0
00 f5       Offset=245
<55 bytes payload>
```

UL 可以乱序发送，例如先发送 offset 245，再发送 offset 0。AMF 在只有末片时不生成 AP 响应；收到首片且区间 `[0,300)` 无空洞后完成重组。

DL 对完整载荷重新分片，不依赖 UL 分片的边界。第一条 DL 放普通响应 IE 和第一个 `0x71`，后续每条 DL 只放一个 `0x71`：

```text
DL #1: 7e 00 e2 01 10 01 01 71 ff <AP fragment offset 0>
DL #2: 7e 00 e2 01          71 41 <AP fragment offset 245>
```

### 5.3 2 字节外层 length

当 `MessageIdentity=0x02` 时，相同的新 AP Value 可以这样承载：

```text
7e 00 e1 02 71 00 0c 01 00 00 08 05 12 34 02 00 00 aa bb
```

`00 0c` 仅是外层 IE length。后面的 AP 内部结构仍是新格式，不接受旧 4-byte `ContainerContent`。

## 6. 完整信令流程

```mermaid
sequenceDiagram
    participant UE
    participant RAN
    participant NAS as AMF NAS/Security
    participant GMM as AMF GMM
    participant CTX as CooperationContext
    participant NAgent

    UE->>RAN: Registration / Authentication / Security Mode
    UE->>RAN: Registration Complete
    RAN->>NAS: UplinkNASTransport
    NAS->>GMM: UE becomes Registered

    loop Each UL AP fragment, may be out of order
        UE->>RAN: Security protected UL Cooperation 0xe1
        RAN->>NAS: UplinkNASTransport(NASPDU)
        NAS->>NAS: Verify MAC and decrypt
        NAS->>NAS: Decode UL header and independent TLVs
        NAS->>GMM: HandleULCooperation
        GMM->>CTX: Save LastMessageIdentity and LastULIEs
        GMM->>GMM: Process 0x10/0x18 independently
        GMM->>GMM: Decode 0x71 AP header
        GMM->>CTX: Add fragment by UL + PayloadId
        alt AP invalid
            GMM-->>GMM: Drop AP state
            GMM->>NAS: Send ordinary DL response only, if any
        else AP incomplete
            GMM->>CTX: Keep fragment and pending ordinary DL IEs
            GMM-->>UE: No DL Cooperation
        else AP complete
            GMM->>CTX: Store completed payload
            GMM->>GMM: Strictly decode Intent JSON
            GMM->>GMM: Validate full Intent JSON
            GMM->>CTX: Store requestId -> PTI/PayloadId/generation
            GMM->>NAgent: POST /nagent-intent/v1/intent/{supi}<br/>Body = full AP payload
            GMM-->>UE: No DL Cooperation before HTTP completion
            alt Valid response within 3-second total deadline
                NAgent-->>GMM: 200 application/json
                GMM->>CTX: Match requestId and recover original PTI
            else HTTP failure or 3-second deadline
                GMM->>GMM: Build canonical NAgent error JSON
            end
            GMM->>CTX: Mark transaction Ready
            alt UE is still connected
                loop Each response DL fragment
                    GMM->>NAS: Build DL Cooperation 0xe2<br/>ContainerType=0x0101, PTI=original UL PTI
                    NAS->>RAN: DownlinkNASTransport
                    RAN->>UE: DL Cooperation
                end
            else UE is offline
                GMM->>CTX: Retain Ready response for reconnect
            end
        end
    end
```

## 7. NAS 库改动

### 7.1 注册新消息

在 NAS message type、GMM message union 以及 plain encode/decode dispatcher 中增加 `0xe1/0xe2`：

```go
const (
    MsgTypeULCooperation uint8 = 0xe1
    MsgTypeDLCooperation uint8 = 0xe2
)
```

当前仓库使用本地 NAS fork：

```text
third_party/nas
```

### 7.2 CooperationIE

`CooperationIE` 只负责外层表示：

```go
type CooperationIE struct {
    Iei       uint8
    Len       uint8
    LegacyLen uint16
    Contents  []uint8
}
```

新外层格式要求 `len(Contents)<=255`；legacy 外层可到 65535。不要再在 `CooperationIE` 上保留旧 `GetContainerContent/SetContainerContent`，也不要在 GMM 中用固定下标手工读取 AP 字段。

### 7.3 AP Container codec

新增专用模型与 codec：

```go
type APContainer struct {
    ContainerType      uint16
    ContainerTypePTI   uint8
    ContainerPayloadID uint16
    ContainerFlags     uint8
    FragmentOffset     uint16
    Payload            []byte
}

func DecodeAPContainer(contents []byte) (*APContainer, error)
func (a *APContainer) Encode() ([]byte, error)
func (a *APContainer) DontFragment() bool
func (a *APContainer) MoreFragments() bool
```

codec 必须统一执行长度、reserved flags、DF/MF/offset、空载荷和 65535 上限检查，避免上层出现多套不一致的验证。

## 8. UE 状态与生命周期

在 `CooperationContext` 中增加专用 AP 状态：

```text
APContainerState
  Reassemblies: map[(Direction, PayloadId)]ReassemblyState
  Completed:    map[PayloadId]CompletedAPContainer
```

每个重组状态保存：

```text
ContainerType, PTI, DF
MessageIdentity, AccessType
Fragments[offset]
FinalLength
CreatedAt, Generation, Timer
```

同一载荷的所有片必须保持 `ContainerType`、PTI、DF、MessageIdentity 和 AccessType 一致。完整记录保存 type、PTI、payload ID、完整 Payload 和完成时间。

当前资源限制：

```text
每 UE、每方向最多 8 个未完成载荷
每个载荷最多 1024 个不同分片
每 UE 最多保留 8 个 completed 记录，超限淘汰最旧记录
每次重组绝对超时 30 秒，收到新片不刷新超时
```

UE 删除时必须停止所有 AP timer 并清空未完成状态。状态和返回给调用者的 Payload 都应做深拷贝，锁的粒度应限制在 AP 状态访问范围内。

## 9. UL 重组规则

推荐独立入口：

```go
func AddULAPContainerFragment(
    ue *context.AmfUe,
    accessType models.AccessType,
    messageIdentity uint8,
    fragment *nasMessage.APContainer,
    now time.Time,
) (*nasMessage.APContainer, error)
```

返回语义：

```text
nil, nil       合法但尚未完整
container,nil  重组完成
nil,error      失败，当前 payload 的重组状态已清理
```

重组器支持乱序和完全相同的重复片。任何非完全相同的重叠区间都属于歧义并导致该载荷失败。`MF=0` 的片确定最终长度，只有已收区间无间隙覆盖 `[0, finalLength)` 时才完成。

超时时记录 payload ID、已收片数和缺失区间；如果尚未收到末片，则缺失信息为 `final-length-unknown`。timer callback 使用 generation 校验，避免旧 timer 删除复用相同 payload ID 的新状态。

## 10. DL 再分片与消息分组

AMF 始终基于完整载荷生成 DL，不直接 mirror 某个 UL 原始片。启用 NAgent 时待分片的是 HTTP 响应；关闭 NAgent 时待分片的是 UL 重组结果：

```text
DF=0: 按最多 245 Payload bytes 生成 DL AP 分片
DF=1: 保持单条，不允许 AMF 分片
空 Payload: 生成 offset=0、MF=0 的单条 AP Container
```

对于 `MessageIdentity=0x01`，DF=1 且 AP Value 超过 255 字节时无法用一字节外层 length 编码，builder 返回错误并丢弃 AP 响应。对于 legacy 外层，Value 最大为 65535 字节。

`processULCooperationIEs` 返回多组 DL IE：

```text
group 0 = ordinary response IEs + AP fragment 0
group 1 = AP fragment 1
group 2 = AP fragment 2
...
```

每组分别构造成一条 DL Cooperation 并经 NGAP 下发。这保证每条消息最多一个 `0x71`。

## 11. GMM 接入点

在 GMM dispatcher 中增加：

```go
case nas.MsgTypeULCooperation:
    return HandleULCooperation(amfUe, accessType, gmmMessage.ULCooperation)
```

处理顺序：

1. 拒绝 MAC 校验失败或缺少 UE/RAN 上下文的消息。
2. 保存本条消息的 identity 和原始 `LastULIEs`。
3. 先按 handler registry 处理 `0x10/0x18`。
4. 要求本条消息最多一个 `0x71`。
5. 解码 AP，加入重组器；错误只丢弃 AP 路径。
6. 完成后保存 completed record；启用 NAgent 时创建事务并异步提交 HTTP，否则直接生成 DL AP fragments。
7. HTTP 回调通过同一 UE 的 NGAP worker 串行化，响应 Ready 后生成 DL AP fragments。
8. 对每个 DL IE group 调用 `SendDLCooperation`。

普通 IE handler registry 便于以后扩展：

```go
var ulCooperationIEHandlers = map[uint8]cooperationIEHandler{
    0x10: handleULCooperationIE10,
    0x18: handleULCooperationIE18,
}
```

`0x71` 不建议作为普通 handler 注册，因为它需要跨消息状态和多条 DL 分组。

## 12. 安全与 NGAP 发送

Cooperation 一般发生在注册完成后，UL NAS 必须走现有安全链路：

```text
NGAP UplinkNASTransport
-> NAS integrity verification
-> NAS decryption
-> plain UL Cooperation decode
-> GMM dispatch
```

DL 使用现有 NAS security context 加密并计算 MAC，再调用 NGAP DownlinkNASTransport。连续生成多条 DL 时，每条消息使用递增的 DL NAS count；测试工具解密多条历史消息时必须使用各自编码时的 count，不能都使用当前 count 减一。

## 13. 日志与错误策略

AP 格式错误、metadata 不一致、重叠、重组资源超限、超时和 DL 编码失败均记录日志后丢弃，不发送重组层错误响应。进入 NAgent 事务后发生的 payload ID 冲突、HTTP 错误和 AMF HTTP 队列溢出会返回第 14.6 节定义的 JSON 错误 AP Payload。日志应包含：

```text
AccessType, MessageIdentity
ContainerType, PTI, PayloadId
DF, MF, FragmentOffset, PayloadLength
具体错误原因
```

为避免大载荷刷屏，Payload hex 日志最多输出前 32 字节，同时记录完整长度。

## 14. NAgent HTTP integration v1

### 14.1 配置

默认 `amfcfg.yaml` 启用本地 mock 地址：

```yaml
configuration:
  nagent:
    enabled: true
    baseUri: http://127.0.0.1:8088
    connectTimeoutMs: 1000
    attemptTimeoutMs: 2000
    totalTimeoutMs: 3000
    maxAttempts: 3
    maxPayloadBytes: 65535
    maxInFlight: 64
    maxInFlightPerUe: 8
    queueSize: 256
    pendingDlTtlSeconds: 60
    mock:
      enabled: true
      listenAddress: 127.0.0.1:8088
      delayMs: 0
      status: 200
```

`baseUri` 是 AMF HTTP client 访问 Agent 的地址，`mock.listenAddress` 是内嵌 mock server 的 TCP 监听地址。默认两者都指向本进程的 `127.0.0.1:8088`，因此启动 AMF 时会自动启动 mock，不需要额外运行 `cmd/nagent-mock`。端口绑定失败会使 AMF 启动失败；AMF 终止时会同时优雅关闭 mock。

接入真实 Agent 时设置 `mock.enabled: false`，并把 `baseUri` 改成真实 Agent 地址。AMF 与外部 Agent 位于不同容器时，`127.0.0.1` 必须改成 Agent 的容器 DNS 名称或宿主机可达地址。第一版 client 只接受绝对 `http://` URL，不启用 TLS 和认证。

### 14.2 HTTP 契约

```http
POST /nagent-intent/v1/intent/{supi}
Content-Type: application/json
Accept: application/json
Idempotency-Key: <64 lowercase hex characters>
X-NAgent-Request-ID: <same value as Idempotency-Key>
X-AP-Access-Type: 3GPP_ACCESS
X-AP-Message-Identity: <decimal uint8>
X-AP-Container-Type: <decimal uint16>
X-AP-PTI: <decimal uint8>
X-AP-Payload-ID: <decimal uint16>

<exact UL AP Container payload bytes>
```

`{supi}` 使用 URL path escaping。UL AP Payload 必须严格符合以下 JSON 结构，所有字段都必须存在，不允许未知字段，`intentPriority` 必须是整数：

```json
{
  "intentId": "intent-001",
  "issuer": "ue",
  "intentPriority": 10,
  "intentType": "location",
  "intentDescription": "Locate the target UE and return its current cell",
  "object": "ue-location",
  "constraint": "accuracy<100m",
  "target": "imsi-001010000000002"
}
```

AMF 会把完整 UL AP Payload 原样转发给 NAgent。协议约定 Intent 使用 `ContainerType=0x0101`，此时 HTTP body 就是上面的完整 Intent JSON 字节，不再提取并单独包装 `intentDescription`。当前 AMF 实现的 NAgent 入口以“AP payload 是否是完整 Intent JSON”为实际触发条件，并不会在 UL 方向强制拒绝其它 `ContainerType`；但 DL 响应和错误始终使用 `ContainerType=0x0101`。

NAgent 成功响应必须是 `HTTP 200`、`Content-Type: application/json` 且 body 为合法 JSON。第一版 mock 原样返回请求 body，并回显上述 `X-*` 关联头。响应 body 被视为不透明 Agent 结果，不从中读取 PTI，并原样进入 DL AP Payload。为兼容只返回 JSON 的 Agent，响应关联头均为可选；但只要响应携带其中任意一个，HTTP client 就会解析并验证它与原请求一致，不一致时返回 `NAGENT_INVALID_RESPONSE`。

### 14.3 幂等与事务键

每个 UE 的事务以 `ContainerPayloadId` 为索引。AMF 事务指纹覆盖 SUPI、MessageIdentity、AccessType、ContainerType、PTI、PayloadId 和完整 UL Intent payload 的 SHA-256：

```text
相同 PayloadId + 相同指纹 + Pending/Sending -> 视为重复上报，不再次调用 HTTP 或重复下发
相同 PayloadId + 相同指纹 + Ready/Sent -> 重放已缓存 DL 响应
相同 PayloadId + 不同指纹 -> 返回 PAYLOAD_ID_CONFLICT
不同 PayloadId -> 可以并行，响应允许乱序完成
```

HTTP `Idempotency-Key` 由协议版本、SUPI、消息 metadata 和完整 HTTP body SHA-256 确定。一次请求的所有重试使用同一个 key，并将相同值放入 `X-NAgent-Request-ID`。AP 事务显式保存该 Request ID、原 UL PTI、PayloadId 和 generation；HTTP callback 必须匹配保存的 Request ID，随后从事务取回 PTI。PTI 不是全局唯一值，不能单独作为并发事务键，也不从 Agent 响应 body 中解析。

### 14.4 异步处理与 DL 字段

AP 完成后 HTTP 请求不会阻塞当前 GMM worker，但当前 UL 中的普通 `0x10` 响应会保存在 AP 事务中，不会提前下发。AMF 在收到有效 HTTP 响应，或 HTTP 请求达到 3 秒总截止时间并生成错误载荷后，才构造 DL Cooperation。第一条 DL 消息包含待回复普通 IE 和第一个 `0x71` 分片，后续消息仅包含剩余 `0x71` 分片。有效 AP 分片尚未重组完成时，普通 DL IE 保存在对应 PayloadId 的重组状态中。

DL AP Container 复用 UL 的：

```text
MessageIdentity
ContainerTypePTI
ContainerPayloadId
```

其中 `ContainerTypePTI` 是通过匹配 HTTP Request ID 后从原 UL 事务取得的 PTI。DL `ContainerType` 不复用 UL 值，Agent 成功和错误响应都固定使用：

```text
ContainerType = 0x0101
Payload       = exact Agent HTTP response body, or canonical AMF error JSON
```

AMF 将 DL `DF` 设为 0，允许按当前外层 length 限制重新分片；`FragmentOffset` 从 0 开始按响应字节计算。UE 有有效 RAN 关联时，HTTP callback 通过非阻塞方式进入与该 UE NGAP 消息相同的 worker 队列；没有 RAN 时不使用虚构的 worker ID，而是保留 Ready 等待重连。队列暂满时按最大 1 秒间隔退避重试，直到事务被投递、被其他 UE 事件认领或 TTL 到期，避免 worker 相互提交 callback 时死锁。投递前事务由 Ready 原子转换为 Sending；RanUe 短锁内生成只包含 NGAP ID、RAN、AMF UE 和关联版本的只读发送目标，不复制或消费 `OldAmfName`。所有 `AmfUe.RanUe` map 读写必须通过关联层的 `RWMutex`、单项 accessor 或快照 API，禁止 SBI/HTTP goroutine 直接索引该 map。NAS/NGAP 编码和网络写入在锁外完成，每个分片发送前后均校验关联版本。若发送期间发生 N2/Xn 切换，旧链路发送会尽快停止，事务退回 Ready 并重路由到当前 RanUe。每 UE 的 NAS 安全编码另行串行化，避免切换窗口中并发修改 `DLCount`。

### 14.5 重试、并发和离线行为

以下情况可重试：DNS/连接失败、attempt timeout、HTTP 408、429、500、502、503、504。其他 4xx、非法 JSON 响应和超大响应不重试。单次连接、单次尝试和总请求分别受 `connectTimeoutMs`、`attemptTimeoutMs` 和 `totalTimeoutMs` 限制；默认 `totalTimeoutMs=3000`，所有重试都必须包含在这 3 秒内。HTTP 响应和截止定时器并发到达时，仅第一个完成事务，迟到结果会被忽略。

全局最多 64 个 HTTP worker，队列默认 256；每 UE 最多保留 8 个 Pending/Ready/Sending/Sent 事务。UE 删除时取消 Pending HTTP context 并停止 Ready TTL timer。每个 RAN 同时最多执行一次底层 NGAP 写入，等待发送槽和实际写入共用 5 秒预算；连接支持 deadline 时同时设置底层 deadline，不支持时 watchdog 会在超时后关闭失去响应的连接以解除阻塞。短写也视为失败。只有所有 DL 分片均成功完成 NGAP 构造和底层连接写入，事务才进入 Sent；失败或关联切换会退回 Ready，等待当前切换流程或后续 UE 可用事件重投。这里的 Sent 只表示所有分片已经写入 AMF 本地 SCTP 连接，不表示 UE 已经接收或处理。

每次 Ready -> Sending 都生成单调递增的 delivery attempt。AMF 对该轮每个分片实际下发的完整安全保护 NAS PDU 计算 SHA-256，并绑定到 PayloadId、事务 generation 和 attempt。收到 NGAP NAS Non-Delivery Indication 时，只有 NAS PDU、AccessType、generation 和当前 attempt 全部匹配才会把整条 AP 响应退回 Ready；随后在下一次注册完成、Service Request 成功、切换完成或重复 UL 触发的可投递时机从 offset 0 重发。旧轮次、错误 AccessType 和重复 Non-Delivery 会被忽略。AMF 不在 Non-Delivery 回调中立即重发，避免 RAN 无法投递时形成紧密循环。

当前协议没有为 DL AP Container 定义 UE 端确认消息，因此“未收到 Non-Delivery”仍不能构成端到端送达证明。若未来要求确认 UE 已重组并处理 payload，需要另外定义带 PayloadId/PTI 的 UL ACK/NAK IE 和确认超时；该扩展不应复用当前 Sent 状态的含义。

切换或部分写入失败后，AMF 会从 offset 0 重投整个 DL AP Payload。因此 UE 重组器必须幂等接受 metadata 和 payload 完全相同的重复分片；同一 PayloadId、offset 上内容或 MF 不同的重叠分片仍必须拒绝。

HTTP 响应到达时 UE 离线，不发 paging。响应保持 Ready 最多 60 秒，在 UE 重新进入 Registered 或成功处理 Service Request 后下发；超时后删除。已经 Sent 的事务暂留用于重复 UL 的幂等重放，容量不足时优先淘汰最旧 Sent 事务。

### 14.6 错误 AP Payload

NAgent 路径错误统一编码为合法 JSON：

```json
{"$nagent":{"version":1,"status":"error","code":"NAGENT_TIMEOUT","retryable":true,"httpStatus":0,"message":"NAgent did not respond before the deadline"}}
```

当前错误 code 包括：

```text
INVALID_JSON
INVALID_REQUEST
PAYLOAD_TOO_LARGE
PAYLOAD_ID_CONFLICT
AMF_QUEUE_FULL
NAGENT_TIMEOUT
NAGENT_UNAVAILABLE
NAGENT_REJECTED
NAGENT_INVALID_RESPONSE
NAGENT_RESPONSE_TOO_LARGE
```

错误 body 同样使用原 PayloadId/PTI 包装成 `ContainerType=0x0101` 的 DL AP Container。当前调试版本会在 NAS Cooperation 日志中输出 AP 明文内容；部署到非调试环境前应关闭对应详细日志。NAgent 日志不写明文 SUPI，只记录截断的 SUPI hash、payload length 和 idempotency key。指标 label 不应包含 SUPI。

### 14.7 本地 mock

默认配置会随 AMF 自动启动原样回显 mock。可通过配置模拟慢响应或错误：

```yaml
configuration:
  nagent:
    baseUri: http://127.0.0.1:8088
    mock:
      enabled: true
      listenAddress: 127.0.0.1:8088
      delayMs: 250
      status: 200
```

`delayMs` 用于模拟处理时间，`status` 可设为 `503` 验证重试和错误映射。mock 只接受正确路径、POST、JSON Content-Type 和合法 JSON。

独立命令仍保留用于不启动 AMF 的调试：

```bash
go run ./cmd/nagent-mock -addr :8088
```

使用独立命令前必须把内嵌 `mock.enabled` 设为 `false`，否则两个服务会争用同一端口。独立命令可用 `-delay 250ms` 或 `-status 503` 调整行为。

## 15. 测试要求

至少覆盖：

1. 10 字节 AP 头精确编码、解码和所有结构校验。
2. 新外层一字节 length 的 245 字节 Payload 边界。
3. legacy 外层承载新 AP 内部头。
4. 乱序、完全重复、重叠、metadata mismatch 和最终长度冲突。
5. 8 个并发重组、1024 片、30 秒 generation-safe timeout。
6. completed 深拷贝和最旧记录淘汰。
7. DF=0 DL 分片、DF=1 不分片、空 Payload。
8. 普通 IE 与 AP 失败/未完成相互独立。
9. DL builder 拒绝 `0x18` 和多个 `0x71`。
10. 注册完成后通过受保护 UL 注入乱序分片，并验证多条受保护 DL。
11. context、GMM 和 NGAP 相关 race tests。
12. HTTP header/path/body、稳定幂等 key、retryable/permanent status 和总超时。
13. UL 重组 -> mock HTTP -> DL 再分片的跨层回显。
14. Pending 重复、PayloadId 冲突、并发乱序完成、离线 Ready 和重连补发。
15. 内嵌 mock 配置校验、端口监听、HTTP 回显和优雅关闭。

相关文件：

```text
third_party/nas/nasMessage/NAS_APContainer_test.go
internal/context/ap_container_test.go
internal/gmm/ap_container_reassembly_test.go
internal/gmm/ap_container_downlink_test.go
internal/gmm/cooperation_test.go
internal/gmm/message/cooperation_test.go
internal/nas/ul_cooperation_test.go
internal/nas/dl_cooperation_test.go
internal/ngap/registration_procedure_test.go
internal/nagent/client_test.go
internal/nagent/dispatcher_test.go
internal/nagent/mock_test.go
internal/gmm/ap_container_intent_test.go
```

验证命令：

```bash
cd third_party/nas
go test ./...

cd ../..
go test ./...
go test -race ./internal/context ./internal/gmm ./internal/gmm/message ./internal/nagent ./internal/ngap
```

NGAP 测试通过 `httptest` 监听本地端口，受限 sandbox 中可能需要开放 listen 权限。

## 16. 移植任务列表

```text
1. 注册 NAS message type 0xe1/0xe2，并接入 plain encode/decode
2. 实现 ULCooperation、DLCooperation 和通用 CooperationIE
3. 保留需要的外层一字节/二字节 length 规则
4. 实现严格的 10 字节 AP Container codec
5. 删除旧 4-byte ContainerContent 的访问与兼容路径
6. 在 AmfUe 上增加有锁的 AP reassembly/completed 状态
7. UE 删除时停止 timer 并清理状态
8. 实现乱序、重复、重叠、末片和 gap 检查
9. 实现并发数、片数、载荷长度、completed 数和超时限制
10. 让 0x10/0x18 独立于 0x71 处理
11. completed AP 不写入 NegotiatedIEs[0x71]
12. 按 DF 和 245 字节规则生成 DL AP fragments
13. 每条 Cooperation 最多一个 0x71
14. 多个 DL fragments 分成多条 DL Cooperation 发送
15. 接入 NAS security 和 NGAP DownlinkNASTransport
16. 增加 NAgent 配置、严格 JSON HTTP client、重试和幂等 key
17. 增加每 UE intent 状态、HTTP dispatcher、UE worker callback 和离线 TTL
18. 提供 mock NAgent，并验证 HTTP 回显到 DL AP 再分片
19. 增加 codec、重组、GMM、NGAP 和 race tests
```

## 17. 常见问题

### 是否兼容旧 UE 的 4-byte ContainerContent？

不兼容。长度恰好大于等于 10 并不代表合法，内部 `ContainerContentLength`、flags 和 offset 都必须通过严格校验。

### 为什么还存在 legacy length？

它只保留 Cooperation IE 外层 `uint16` length 的承载能力，不代表恢复旧 AP 内部格式。两种外层格式都使用相同的新 10 字节 AP 头。

### 为什么 DL 不是原样返回 UL 分片？

UL 的分片边界和到达顺序不可信。AMF 先得到完整载荷并存储，再按自己的外层限制生成确定的 DL 分片。

### 没有真实 UE 怎么测试？

标准 UERANSIM 不会发送本地扩展 `0xe1`。当前最接近真实链路的无 UE 测试是 NGAP registration mock：完成注册和安全上下文后，通过 UplinkNASTransport 注入加密 UL Cooperation，并从 DownlinkNASTransport 解出 DL Cooperation。真实空口测试仍需修改 UE/UESIM 侧 NAS 实现。
