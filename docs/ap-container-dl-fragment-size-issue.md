# AP Container DL 分片大小不匹配问题记录

- 状态：未提交（工作区改动）
- 发现日期：2026-07-25
- 关联设计：`docs/superpowers/specs/2026-07-10-ap-container-fragmentation-design.md`（§5.2 / §8.1 规定 `APContainerMaxDLFragmentSize = 245`）

## 1. 现象

`MessageIdentity == 0x01` 且 DL AP Container 响应/分片 payload 单片 > 245 字节时，UE 收不到任何 DL `0x71` 响应，AMF 侧只在日志里留一行 error/drop，不下发 DL AP Container。

## 2. 根因

工作区把 DL 分片大小常量从 245 改为 1400（未提交）：

```diff
// third_party/nas/nasMessage/NAS_APContainer.go:15
- APContainerMaxDLFragmentSize = 245
+ APContainerMaxDLFragmentSize = 1400
```

`MessageIdentity == 0x01` 的 DL 路径走 `NewCooperationIE`，即 **1 字节外层 length** 格式：

- `CooperationIE.Len` 类型为 `uint8`，上限 255（`third_party/nas/nasMessage/NAS_CooperationIE.go:17`）
- 构造时硬校验：`len(contents) > 255` 即返回 error（`NAS_CooperationIE.go:23`）
- AP Container 的 `contents = 10 字节固定头 + payload`（`NAS_APContainer.go:10` `APContainerHeaderLength=10`）

因此 1 字节 length 路径下：

```
max payload = 255 − 10 = 245
```

245 正是设计文档选定的死值，`10 + 245 = 255` 顶到 1 字节 length 天花板。改成 1400 后单片 `contents = 10 + 1400 = 1410` 字节，远超 255，`NewCooperationIE` 直接返回 error。

## 3. 触发条件

同时满足：

1. UL Cooperation 的 `MessageIdentity == 0x01`（`internal/gmm/ap_container_downlink.go:78-80` 走 `NewCooperationIE`）
2. DL AP Container 单片 payload > 245 字节

`DF=1` 单片超 245 的失败是**设计内**行为（设计文档 §8.2：装不下则记日志、不发 DL `0x71`）；`DF=0` 配 1400 片大小导致本应分片却装不下，是**本次常量改动引入的回归**——设计本意是用 245 把 `DF=0` 每片卡在 1 字节 length 上限内。

## 4. 影响链路

`NewCooperationIE` 返回 err → `buildDLAPContainerIEs` 透传 err 返回 `(nil, err)`（`ap_container_downlink.go:50-52`）。

两个上游调用点都是**捕获 err 后静默吞掉**，只返回普通 IE（`0x10/0x18`），不下发任何 DL `0x71`：

| 调用点 | 失败后行为 |
|---|---|
| `internal/gmm/ap_container_intent.go:147-150`（NAgent 响应封装） | `GmmLog.Errorf("Build NAgent DL AP Container failed: %v", err)`，`return groupOrdinaryDLCooperationIEs(ordinary), nil` |
| `internal/gmm/handler.go:254-258`（非 intent 直发） | `logAPContainerDrop(...)`，`return groupOrdinaryDLCooperationIEs(ordinaryDLIEs), nil` |

净效果：UE 发了 UL AP Container，核心网没回 DL `0x71`，且无任何错误反馈给 UE。

## 5. 关键代码位置

| 位置 | 说明 |
|---|---|
| `third_party/nas/nasMessage/NAS_APContainer.go:15` | `APContainerMaxDLFragmentSize` 工作区=1400，HEAD=245 |
| `third_party/nas/nasMessage/NAS_APContainer.go:10` | `APContainerHeaderLength = 10` |
| `third_party/nas/nasMessage/NAS_CooperationIE.go:17` | `Len uint8`（1 字节 length） |
| `third_party/nas/nasMessage/NAS_CooperationIE.go:23` | `len(contents) > 255` 返回 error |
| `internal/gmm/ap_container_downlink.go:78-80` | `MessageIdentity==0x01` 走 `NewCooperationIE` |
| `internal/gmm/ap_container_downlink.go:30-56` | `DF=0` 多片切分逻辑 |
| `internal/gmm/ap_container_intent.go:147-150` | NAgent 响应路径失败吞错 |
| `internal/gmm/handler.go:254-258` | 直发路径失败吞错 |

## 6. 处置建议

二选一：

1. 回退 `APContainerMaxDLFragmentSize` 为 245，与设计文档一致（推荐，最小改动）。
2. 按 `MessageIdentity` 分档：`0x01` 用 245（适配 1 字节 length），其余用 1400（走 `NewCooperationIELegacy` 2 字节 length，上限 65525）。

若选方案 2，需同步更新设计文档 §5.2 / §8.1 的常量定义与分片说明。
