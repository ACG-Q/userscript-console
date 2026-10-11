# 版本帖链接回填与标题格式设计

日期：2026-10-11
状态：已批准（2026-10-11 用户确认方案 A 并修订表格格式）
范围：`usm project` 阶段二发帖后的 Issue 回写与新帖标题格式

## 背景

v1.4.1 验收发现两处体验问题：

1. 首次发版本帖成功后，面板 Issue 的 changelog 表格没有任何指向 Discussion 帖子的链接；用户需下一轮 `/project` 才可能感知（实际上因判等 body 不含帖子信息，永远不同步）。
2. 现行帖子标题 `脚本名 · v1.0.18 版本讨论` 未突出版本号，用户要求 `[版本号] 脚本名称 版本讨论`。

历史帖（Discussion #3）不动，新格式自下一个新版本起生效。

## 目标

- 面板 Issue 的 changelog 表格包含每条版本的 Discussion 链接（有则链，无则 `—`）。
- 发帖成功后当轮回写 Issue，用户无需再跑一轮 `/project` 即可看到链接。
- 新帖标题采用 `[版本号] 脚本名称 版本讨论`。

## 非目标

- 不改历史帖（含标题与正文）。
- 不做删除帖后的链接清理（账本条目只增不减，YAGNI）。
- 不引入讨论分类自定义配置。

## 设计

### 1. 标题格式（issuepage）

`DiscussionTitle` 改为：

```go
fmt.Sprintf("[%s] %s 版本讨论", version, s.Name)
```

例：`[1.0.19] All-in-One Video Downloader · HD (YouTube, TikTok, Instagram, X & More) 版本讨论`。

账本末条 == 当前版本时不会重发帖，因此 v1.0.18 的旧标题帖保持原样，新格式自 v1.0.19 起生效。

### 2. changelog 表格扩为 4 列（projector.BuildIssueBody）

现行 3 列 `版本 | 日期 | 说明` 扩为：

```markdown
| 版本 | 日期 | 说明 | 帖子 |
|---|---|---|---|
| v1.0.18 | 2026-10-06 | 初始同步 | [讨论 #3 →](https://github.com/ACG-Q/userscripts/discussions/3) |
| v1.0.19 | 2026-10-12 | 修复 X | — |
```

规则：

- 版本列带 `v` 前缀（与用户确认的样例一致）。
- 帖子列按版本号匹配 `s.Discussions` 账本条目，命中输出 `[讨论 #N →](URL)`，未命中输出 `—`。
- **恒为 4 列**：无账本时帖子列全 `—`。body 稳定，避免有无账本两种模板造成判等抖动。
- 老 Issue 下轮判等会因 body 变化自然更新一次，属预期演化。

### 3. 发帖后立即回写（projector.publishDiscussions）

`EnsureDiscussionPost` 成功且 `s.Issue != nil` 时，用新账本派生的 `BuildIssueBody` 调用 `UpdateIssue(s.Issue.NodeID, 现 title, 新 body)`：

- title 沿用阶段一同款 `[name] v{version}`（与 `reconcileIssues` 一致）。
- 回写失败**不计错误、不阻塞后续脚本**：发帖与账本落盘已成功，下一轮判等刷新会检测到 body 不等并自愈。
- 回写不计入 `Result.Updated`（统计语义保持「判等刷新」）；`Posted` 照常计数。

### 4. 自洽性论证

- 阶段一判等按 `BuildIssueBody`（含 4 列表格）派生 body；回写后相等 → 下轮零写，无循环。
- 回写失败 → body 落后于账本 → 下轮判等刷新更新 → 自愈。
- 帖子列来源是账本（阶段二成功才写），与阶段一解耦；回写发生在账本追加之后，列内必含新帖。

## 测试

- `issuepage`：`TestDiscussionTitle/Body` 更新标题断言（`[1.2.3] 名称 版本讨论`）。
- `projector`：`BuildIssueBody` 新增账本用例（命中链接、未命中 `—`、无账本全 `—`）；`TestPublish*` 断言发帖后追加一次 `UpdateIssue` 调用（route 序）。
- `commands`：集成测试 routeDoer 补回写响应，断言回帖格式不变（回写不进统计）。
- 门禁：gofmt / vet / golangci-lint / go test / 覆盖率 ≥ 90% / 快照 / build。

## 发布

- 语义化版本：patch（v1.4.2）——行为修复 + 格式调整，无破坏性变更。
- 验收：内容仓手动 `/project` 两轮——第一轮 `更新 Issue: 1`（回写生效，Issue 表格出现讨论 #3 链接）、第二轮全幂等零写。
