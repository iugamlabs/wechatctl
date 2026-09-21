当前实现还不能算验收通过。上一轮 8 项里有 7 项在现在的代码里已经成立；第 1 项只修了单聊和全局搜索，多个 `--chat` 仍会把打开数据库的错误当成「找不到聊天」。另外还有两处独立问题：`init-data` 在微信没启动时先报权限，以及全局搜索把 `sqlite_master` 查询失败当成空结果。

`CGO_ENABLED=0 go test ./... -count=1`、`go vet ./...`、`gofmt -l` 都是干净的。本机五个实例都是 stopped。抽查没有改密钥：`init-data work --force` 前后 `all_keys.json` 的 mtime 和大小都是 `1790004811` / `3003`。没有跑 `new-messages`，避免改写游标。

## 上一轮 FAIL

| 上一轮问题 | 结论 |
|---|---|
| 打开消息库失败被当成没有消息 | **FAIL**。`findMsgTablesForUser`、`CollectChatSearch`、`SearchAllMessages` 已向上返回 `OpenDB` 错误。多个 `--chat` 仍把该错误吃掉，见下面 FAIL 1 |
| 联系人模糊匹配走 map | **PASS**。`ResolveUsername` 按 `Book.full` 的查询顺序先全等再子串 |
| `new-messages` 读写不在同一把锁里 | **PASS**。`RunNewMessages` 在同一次 `WithStateLock` 里 `LoadLastCheck`，再用 `SaveLastCheckLocked` 写回 |
| 没有 `Msg_*` 表时的英文文案 | **PASS**。实跑 `history work filehelper` 的 stderr 是 `找不到 文件传输助手 的消息记录`，exit 1 |
| 解压不做 UTF-8 replace | **PASS**。`DecompressContent` 使用 `strings.ToValidUTF8(..., "\uFFFD")` |
| 300/160 按字节截断 | **PASS**。两处都走 `truncateRunes`；引用消息单测断言 160 个字符再加 `...` |
| `gofmt` | **PASS**。`gofmt -l` 无输出 |
| 缺测试（appmsg 57、两种时间、缓存 size、search JSON） | **PASS**。对应测试已在，并且全绿 |

## FAIL

### 1. 多个 `--chat` 仍把 schema/解密错误当成找不到聊天

- 文件：`internal/wxdata/query/messages.go`，`ResolveChatContexts` 197–200 行；调用点 `cmd/wxctl/data_cmds.go` 387–401 行
- 原因：`ResolveChatContext` 在 `OpenDB` 或 schema 探测失败时返回错误。多聊分支把任何错误都放进 `unresolved`，再继续下一个聊天。全部失败时变成 exit 1 的 `no searchable chats`；部分失败时写成 `未找到: …` 并且 exit 0。Key Decision 10 要求 schema 不匹配必须 exit 6，不能当成空结果或聊天名解析失败。
- 修复：`ResolveChatContexts` 增加 `error` 返回值。遇到 `OpenDB` / `*wxdata.Error` 立刻返回。只有解析不到名字、或确实没有 `Msg_*` 表时，才记入 `unresolved` / `missingTables`。`search` 的多聊分支把这个错误原样返回。

### 2. 全局搜索在枚举消息表失败时不出错

- 文件：`internal/wxdata/query/messages.go`，`loadSearchContextsFromDB` 556–560 行；`SearchAllMessages` 607 行直接使用返回值
- 原因：`SELECT name FROM sqlite_master ... LIKE 'Msg_%'` 失败时函数返回 `nil`。该库随后贡献 0 条命中，也不写入 `failures`，命令仍 exit 0。这是「SELECT 失败后当空结果」。
- 修复：把该错误返回给 `SearchAllMessages`，并立刻 `return nil, nil, err`。不要在这里吞掉。

### 3. 微信没在跑时，`init-data` 先报无 ptrace

- 文件：`cmd/wxctl/data_cmds.go` 67–75 行
- 原因：`HasPtrace` 在 `InstancePIDs` 之前。验收表要求实例未启动时 exit 4。本机无 root、无 `CAP_SYS_PTRACE`，`work` 为 stopped 时执行 `init-data work --force`，stderr 是 `need root or CAP_SYS_PTRACE...`，exit 5。密钥文件没有被改写。列进程不需要 ptrace，不该先报权限。
- 已有合法密钥时，不带 `--force` 的 `init-data work` 正确 skip，exit 0。这符合 Key Decision 15，不是这条的反例。
- 修复：先 `InstancePIDs`。空列表返回 `ErrNotRunning`（exit 4）。有进程且没有 ptrace 时再返回 `ErrPermission`（exit 5）。

### 4. README 写了不存在的调试开关

- 文件：`README.md` 179 行
- 原因：这里写 `WXCTL_DEBUG=1` 会在 stderr 打印解密进度。全仓库 Go 代码没有 `WXCTL_DEBUG`。设计里这项是可选的，但文档已经把它写成事实。
- 修复：按设计实现该环境变量，或者删掉这句。

## Key Decisions

| # | 结论 |
|---|---|
| 1 纯 Go，禁止 `mattn/go-sqlite3` | PASS。`go.mod` 是 `modernc.org/sqlite` 与 `klauspost/compress`。`CGO_ENABLED=0` 静态 ELF，`ldd` 报告不是动态可执行文件 |
| 2 顶层命令，实例名第一参数 | PASS。`--help` 有 9 个数据命令，没有 `data` 父命令 |
| 3 `chat-export`，不动元数据 `export` | PASS。`export` 的 Short 仍是 “Export config and instance metadata (no chat data)” |
| 4 `wxdata/<name>`，purge 同时删除 | PASS。单测覆盖 HOME 缺失仍删除。没有对真实实例执行 `--purge` |
| 5 禁止 `~/.wechat-cli` 和 `/tmp/wechat_cli_cache` | PASS。Go 代码不写这两处。`/tmp/wechat_cli_cache` 时间是 2026-09-21 18:50 |
| 6 只扫本实例，禁止用 `belongsToInstance` 做 /proc 扫描 | PASS。`InstancePIDs` 走 pid 文件加 fail-closed environ。`linux.go` 最后一次改动是数据功能之前的 `6455b2e` |
| 7 history/search JSON 用结构体 | PASS。实跑群 history 含 `local_id`，`failures` 为 JSON null |
| 8 不暴露 `--media` | PASS。`history --help` 没有该 flag |
| 9 `--format` 默认值 | PASS。查询默认 json；`init-data` 默认 text；`chat-export` 默认 markdown |
| 10 schema 硬失败 exit 6 | FAIL。见 FAIL 1、2。`schema_test.go` 缺列返回 code 6 是对的 |
| 11 sudo HOME、euid==0 忽略 XDG、chown 整树 | PASS（单测）。没有用 root 实跑 |
| 12 members 文本按成员列表循环 | PASS。实跑一个群：JSON 含 `group, username, member_count, owner, members[]`，`member_count` 与数组长度一致 |
| 13 独立 `wxdata.Error`，打印外层 err | PASS。`sessions nosuch` 的 stderr 是 `instance "nosuch" not found: instance not found`，没有 `wechat exited`，exit 1 |
| 14 只有 `init-data` 要求进程在跑 | PASS。五个实例都是 stopped，`sessions work --limit 1` exit 0 |
| 15 不留下可 skip 的残缺或 root 密钥 | PASS（单测与 skip 路径）。`init-data work` 校验通过后 exit 0，没有重扫 |

## Tests

`CGO_ENABLED=0 go test ./... -count=1` 全绿，`go vet ./...` 全绿。无 `WXCTL_LIVE_INSTANCE` 时 `TestLiveQuery` 会 Skip。

| 文档要求 | 结论 |
|---|---|
| crypto 页 round-trip、错 key、WAL、`size=4096+100` 只解 1 页 | PASS |
| keys：`..`、斜杠、hex 64/96/100、environ、maps fixture | PASS |
| `Msg_` + md5(`wxid_abc`) | PASS |
| `parse_time_range` 三种格式、当天结束、`start>end` | PASS |
| `validate_pagination`：history 无上限、search 501、limit 0、offset -1 | PASS |
| `format_msg_type`、群 `wxid:\n`、sessions 解压失败 `'(压缩内容)'` | PASS |
| appmsg 5/6/57，引用超过 160 字符加 `...` | PASS |
| history / search 黄金 JSON：`local_id`、`"failures": null`、snake_case、无导出名 | PASS |
| discover：跳过 `all_users`/`WMPF`，选最新 mtime | PASS |
| schema 缺列含列名，作为 exit 6 | PASS |
| cache：同 mtime 不重解、mtime 变、size 变、`_mtimes.json`、salt → `ErrNoKeys` | PASS |
| paths：`WxdataDir` 与 sudo 三分支 | PASS |
| `data_output_test.go`：外层含 `--force`，不含 `wechat exited` | PASS |
| live 默认 Skip，不断言聊天正文 | PASS |
| `gofmt` | PASS |

## Acceptance Checklist

| 项 | 结论 |
|---|---|
| `--help` 含 9 个数据命令，并仍有 `export` / `import` / `start` / `stop` / `restart` | PASS |
| `chat-export --help` 是聊天导出；`export --help` 是元数据 | PASS |
| `sessions` 无参数 exit 1 | PASS |
| 无 `--media`、无 `stats` / `favorites` | PASS |
| 密钥在 `wxdata/<name>/all_keys.json`（0600）。work 与 tk 的 `_db_dir` 各自指向自己的 `db_storage` | PASS |
| 缓存目录 `wxdata/work/cache` 为 0700，不在 `/tmp/wechat_cli_cache` | PASS |
| `new-messages` 的游标按实例分文件 | PASS（代码路径）。work 有 `last_check.json`（0600），tk 没有。本轮没有再执行该命令 |
| 两个实例同时在跑时 init-data 不会把对方的库写进自己的 keys | 未验证。两个实例都是 stopped。已有 JSON 的 `_db_dir` 是分开的 |
| `remove --purge` 删 wxdata | PASS（单测）。真实实例未删 |
| 无 CGO、无 Windows/macOS scanner | PASS |
| sessions 字段 | PASS。9 个字段齐全，抽样 `msg_type` 为「文本」 |
| history envelope 与 `messages[]` | PASS。群抽样含 `local_id`，`time` 为 `2026-09-21 23:31`，`failures` 为 null |
| contacts 列表与 `--detail` | PASS |
| members | PASS |
| new-messages 首次 `first_call`+`unread_count`，再次 `first_call:false`+`new_count` | 未验证（会写游标）。结构体字段与锁内读写是对的 |
| `sessions nosuch` exit 1，stderr 含 `not found`，不含 `wechat exited` | PASS |
| 未 init 的 `sessions work` exit 3 | 未验证。work 已经有密钥 |
| `init-data` 未启动 exit 4 | FAIL。见 FAIL 3。无 `--force` 且密钥有效时是 skip，exit 0 |
| 无 ptrace 时 `init-data` exit 5 | PASS。`--force` 实跑 exit 5，文案含 `sudo wxctl init-data work` |
| `history` 不存在的聊天 exit 1 | PASS |
| `members` 非群 exit 1 | PASS（`文件传输助手`） |
| 非法时间 exit 2、`--limit 501` exit 2、未知 `--type` exit 2 | PASS |
| schema 缺列 exit 6 | PASS（单测） |
| `mirai` 无 db_storage | PASS。`init-data mirai` 与 `sessions mirai` 都是 exit 1，文案含 `no WeChat data` |
| stop 之后 `sessions work` exit 0 | PASS。`status` 显示 work stopped；热缓存查询约 13ms |
| `Msg_` = md5、群前缀剥离、图片/文件占位、sender `me` | PASS。代码与单测覆盖前三项。另抽样到一条己方消息，`sender` 为 `me` |
| 查询无需 sudo | PASS |
| 冷解密 < 3s | 未验证。work 缓存是热的 |

## 必须 / 禁止 / 不要

上面已经覆盖的不重复。

| 约束 | 结论 |
|---|---|
| 不 exec Python、不包装 wechat-cli、不改 wechat-cli | PASS。`/home/deali/code/2/wechat-cli` 只有未跟踪的 `uv.lock` |
| 不实现 stats / favorites / MCP / `--media` / Windows scanner | PASS |
| 不把状态写进伪造 HOME；目录 0700；密钥与明文 0600 | PASS |
| Discover 不用 `~/Documents/xwechat_files`，多候选按 message mtime，不交互 | PASS |
| `_db_dir` 缺失或变化 → exit 3，文案含 `init-data --force` | PASS（`wxdata.Open`） |
| `Close` 不删缓存；不解密 fts/biz 用于查询；不用 `PRAGMA` 写业务数据 | PASS |
| 明文库只用 `file:` URI，`mode=ro`，两个 PRAGMA 在 Open 之后 Exec | PASS |
| hex 规则 64/96/>96、`cross_verify`、必需库 `session`+`contact`+至少一个 `message_N` | PASS |
| 进度走 stderr；JSON 成功走 stdout，`SetEscapeHTML(false)`，两空格缩进 | PASS |
| 表名只允许 `Msg_[0-9a-f]{32}`，keyword 不拼进 SQL | PASS。正则是 `message/message_\d+\.db$` |
| 分页：多表降序切片再升序输出；history 无 500 上限；search 最大 500 | PASS |
| 解压失败：sessions `'(压缩内容)'`，history `'(无法解压)'`，search 跳过该行 | PASS |
| 群摘要 `:\n` 后半；己方 `me`；appmsg 5/6/57/小程序占位；XXE 长度 20000 与 DOCTYPE/ENTITY | PASS |
| contacts `--detail` 用 `Flags().Changed`，找不到 exit 1，优先于 `--query` | PASS |
| members：非群 exit 1；owner 显示名，排序用原始 username | PASS |
| 查询 `--limit 0` 空列表 exit 0。负数 limit exit 1 | PASS（`sessions work --limit 0` 输出 `[]`） |
| `init-data` 成功删除 `.partial`；其它库缺 key 仍成功并打 `MISSING` | PASS（代码与单测）。本轮没有重扫 |
| 不改 `show`、不加 `cache-clear`、不改 `export` 的 Use | PASS |
| `WXCTL_DEBUG=1` | FAIL。见 FAIL 4 |
| sudo 实机 chown、两实例同时在跑时的 init-data、冷解密耗时 | 未验证 |

先改 FAIL 1 和 2，让 schema/打开库的错误保持原来的退出码；再把未启动的 `init-data` 改成 exit 4。README 那句要么实现，要么删掉。