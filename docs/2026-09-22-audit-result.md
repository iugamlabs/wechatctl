当前实现没有完整符合 `docs/wxctl-linux-data-query.md`。PR 1–7 的主路径都在，`CGO_ENABLED=0 go test ./...` 和 `go vet ./...` 通过，`work` 已停止时 `sessions` / `history` / `search` / `contacts` / `members` / `new-messages` 仍能查出数据。对照文档仍有 8 处实现 FAIL 和 4 处测试 FAIL。

工作区干净，对应提交是 `97141da`+`c4f47fc`（PR1）、`68330ee`（PR2）、`544928d`（PR3）、`e41cc6e`（PR4）、`33c2082`（PR5）、`80df86f`（PR6）、`8e74a78`+`255ec4d`（PR7）。`internal/backend/linux.go` 的 `belongsToInstance` 自数据功能之前就没有再改。`/home/deali/code/2/wechat-cli` 只有未跟踪的 `uv.lock`，没有被这次实现改过。

审计时执行了 `wxctl new-messages work --reset`，因此新建了 `~/.local/share/wxctl/wxdata/work/last_check.json`（0600）。之前这个文件不存在。`tk` 仍没有游标文件。

## FAIL

### 1. 消息库打开失败被当成「没有消息」

- 文件：`internal/wxdata/query/messages.go`，`findMsgTablesForUser`（约 225–228 行）、`CollectChatSearch`（约 543–549 行）、`SearchAllMessages`（约 604–608 行）
- 原因：`OpenDB` 失败（schema 不匹配应 exit 6，解密失败应 exit 3）时，发现表的路径直接 `continue`。`history` / `chat-export` 随后在 `cmd/wxctl/data_cmds.go` 约 308、454 行变成 exit 1 的 `no message history`。`search` 把错误塞进 `failures` 后仍 exit 0。这违反 Key Decision 10：schema 失败必须硬失败，禁止当成空结果。
- 修复：`OpenDB` 返回 `*wxdata.Error` 时立刻向上返回，不要跳过该库。只有 `sqlite_master` 里确实没有这张 `Msg_*` 表时才跳过。

### 2. 联系人模糊匹配顺序不稳定

- 文件：`internal/wxdata/query/contacts.go`，`ResolveUsername`（约 132–141 行）
- 原因：显示名全等和子串都在 `map` 上遍历。文档要求按通讯录顺序命中第一条。Go 的 map 顺序不确定，同名或子串重叠时会命中不同的人。
- 修复：在已按 `SELECT` 顺序保存的 `Book.full` 上做全等，再做子串。

### 3. `new-messages` 的读-改-写没有放在同一把锁里

- 文件：`internal/wxdata/query/sessions.go`，`RunNewMessages`（约 192–216 行）；`internal/wxdata/wxdata.go`，`LoadLastCheck`（约 132 行）只读文件，`SaveLastCheck`（约 152 行）才 `flock`
- 原因：文档要求 `last_check.json` 的读写都用实例 `lock`。两个并发的 `new-messages` 可以同时看到「无状态」，都走首次调用并互相覆盖游标。
- 修复：在同一次 `WithStateLock` 里完成读取、判断 `first_call`、写回。

### 4. 有聊天对象但没有 `Msg_*` 表时的文案不对

- 文件：`cmd/wxctl/data_cmds.go`，约 308–309 行和 454–455 行
- 原因：文档规定这条 stderr 是 `找不到 %s 的消息记录`，exit 1。现在是英文 `no message history for %q`。
- 修复：改成文档中的中文句子，退出码保持 1。

### 5. 解压结果没有做 UTF-8 replace

- 文件：`internal/wxdata/query/format.go`，`DecompressContent`（约 65–80 行）
- 原因：文档要求非 zstd blob，以及 zstd 解压后的字节，按 UTF-8 replace 变成字符串。现在是 `string(content)` / `string(out)`，非法字节会原样留下。
- 修复：用 `strings.ToValidUTF8(..., "\uFFFD")` 或等价的 UTF-8 replace。

### 6. 截断按字节而不是字符

- 文件：`internal/wxdata/query/messages.go` 约 378 行（search `text` 300）；`internal/wxdata/query/format.go` 约 204 行（引用 `ref_content` 160）
- 原因：文档写的是 300 字符和 160 字符。`len(string)` 是字节数，中文会被提前截断，还可能切在 UTF-8 中间。
- 修复：按 `[]rune` 计数后再加 `...`。

### 7. `gofmt` 不干净

- 文件：`internal/wxdata/query/format.go`、`messages.go`、`messages_test.go`
- 原因：实现规则写明 `gofmt` 不过即验收失败。差异只是对齐空格。`go vet` 是干净的。
- 修复：对这三个文件执行 `gofmt -w`。

### 8. 文档要求的测试没有写全

| 缺口 | 位置 | 原因 | 修复 |
|------|------|------|------|
| appmsg type 57 | `internal/wxdata/query/format_test.go` 的 `TestFormatAppMessagePlaceholders` | 只断言了 type 5 和 6。实现里 57 的分支在 `format.go` 约 201–218 行 | 加一条引用消息的断言：标题、`↳`、超 160 的 `...` |
| 三种时间格式 | `internal/wxdata/query/time_test.go` | 只测了日期的当天结束和 `start>end`。`YYYY-MM-DD HH:MM` 与 `HH:MM:SS` 没有断言 | 补这两条 `ParseTimeValue` |
| 缓存按 size 失效 | `internal/wxdata/cache/cache_test.go` | 只改了 mtime。`cache.go` 的 `cacheHit`（约 149–153 行）同时比较 size，但没有测试 | 保持 mtime，改文件大小后再 `Get`，断言重新解密 |
| search 黄金 JSON | `internal/wxdata/query/messages_test.go` 的 `TestSearchResultJSONGolden` | history 断言了 `"local_id"`。search 只断言了 `"failures":null`，没有断言 snake_case 字段名 | 断言 `"timestamp"`、`"chat"` 等 tag，并确认没有导出名 |

## Key Decisions

| # | 结论 | 说明 |
|---|------|------|
| 1 纯 Go，禁止 `mattn/go-sqlite3` | PASS | `go.mod` 是 `modernc.org/sqlite` 与 `klauspost/compress`。`CGO_ENABLED=0 go build` 得到静态 ELF，`ldd` 报告不是动态可执行文件 |
| 2 顶层命令，实例名第一参数 | PASS | `wxctl --help` 有 9 个数据命令，没有 `data` 父命令 |
| 3 `chat-export`，不动元数据 `export` | PASS | `export <file>` 的 Short 仍是 “Export config and instance metadata (no chat data)” |
| 4 `wxdata/<name>`，purge 同时删除 | PASS | `paths.WxdataDir`；`instance.Remove` 确认文案含 HOME 和 wxdata；HOME 缺失仍 `RemoveAll`。单测覆盖。真实实例的 `--purge` 未跑 |
| 5 禁止 `~/.wechat-cli` 和 `/tmp/wechat_cli_cache` | PASS | Go 代码不写这两处。本机 `/tmp/wechat_cli_cache` 的时间是 2026-09-21 18:50，早于 wxdata 缓存，不是这次 `wxctl` 写的 |
| 6 只扫本实例，禁止用 `belongsToInstance` 做 /proc 扫描 | PASS | `runtime.InstancePIDs` 走 pid 文件，再加上 `keys.ListInstanceWeChatPIDs` 的 fail-closed environ。`work` / `work2` 单测在 `maps_test.go` |
| 7 history/search JSON 用结构体 | PASS | 现场 `history` 含 `local_id`，`failures` 为 JSON null。text / `chat-export` 走 `ToTextLine` |
| 8 不暴露 `--media` | PASS | `history --help` 没有该 flag |
| 9 `--format` 默认值 | PASS | 查询默认 json；`init-data` 默认 text；`chat-export` 默认 markdown |
| 10 schema 硬失败 exit 6 | FAIL | 见 FAIL 1。`schema_test.go` 对缺列返回 code 6 是对的，查询层会把 message 库的 Probe 错误吃掉 |
| 11 sudo HOME、euid==0 忽略 XDG、chown 整树 | PASS | `paths_test.go` 三分支和 XDG 都有。chown 失败会删正式 `all_keys.json`，并会 chown 父目录 `wxdata/`。没有真的 sudo 跑过 |
| 12 members 文本按成员列表循环 | PASS | `writeMembersText` 遍历 `res.Members`。现场 JSON 含 `group, username, member_count, owner, members[]` |
| 13 独立 `wxdata.Error`，打印外层 err | PASS | `main.go` 的 `classifyRunError` 对 `ExitError` 不打印。`sessions nosuch` 的 stderr 是 `instance "nosuch" not found`，没有 `wechat exited`，exit 1 |
| 14 只有 `init-data` 要求进程在跑 | PASS | 数据命令不调用 `runtime.Probe`。五个实例现在都是 stopped，`sessions work --limit 1` exit 0 |
| 15 不留下可 skip 的残缺或 root 密钥 | PASS | 无 key 不写正式文件；缺必需库写 `.partial`；skip 前校验 `_db_dir`、必需 key 和 salt；chown 失败会 demote。逻辑有 `persist_test.go`。没有用 root 实跑 |

## Tests

`CGO_ENABLED=0 go test ./... -count=1` 全绿，`go vet ./...` 全绿。无 `WXCTL_LIVE_INSTANCE` 时 `TestLiveQuery` 会 Skip。

| 文档要求 | 结论 |
|----------|------|
| crypto 页 round-trip、错 key、WAL、`size=4096+100` 只解 1 页 | PASS |
| keys：`..`、斜杠、hex 64/96/100、environ、maps fixture（vdso、`/usr/lib`、wcdb、>500MB） | PASS |
| `Msg_` + md5(`wxid_abc`) = `Msg_cdebbea2056901bfd390f03a4e892e6e` | PASS |
| `parse_time_range` 三种格式 | FAIL，见上表 |
| `validate_pagination`：history 无上限、search 501、limit 0、offset -1 | PASS |
| `format_msg_type`、群 `wxid:\n`、sessions 解压失败 `'(压缩内容)'` | PASS |
| appmsg 5/6/57 | FAIL，缺 57 |
| history 黄金 JSON：`local_id`、`"failures": null`、无 `LocalID` | PASS |
| search 黄金 JSON 同样断言 snake_case | FAIL |
| discover：跳过 `all_users`/`WMPF`，选最新 mtime | PASS |
| schema 缺列含列名，作为 exit 6 | PASS |
| cache：同 mtime 不重解、mtime 变则重解、`_mtimes.json`、salt → `ErrNoKeys` | 部分 FAIL：size 变化没有测 |
| paths：`WxdataDir` 与 sudo 三分支 | PASS |
| `data_output_test.go`：外层含 `--force`，不含 `wechat exited` | PASS |
| live 默认 Skip，不断言聊天正文 | PASS |
| `gofmt` | FAIL |

## Acceptance Checklist

| 项 | 结论 |
|----|------|
| `--help` 含 9 个数据命令，并仍有 `export` / `import` / `start` / `stop` / `restart` | PASS |
| `chat-export --help` 是聊天导出 | PASS |
| `sessions` 无参数 exit 1 | PASS（实测） |
| 无 `--media`、无 `stats` / `favorites` | PASS |
| 密钥在 `wxdata/<name>/all_keys.json`（0600），work 与 tk 的 `_db_dir` 各自指向自己的 `db_storage` | PASS |
| 缓存目录 `wxdata/work/cache` 为 0700，不在 `/tmp/wechat_cli_cache` | PASS |
| `new-messages` 的游标按实例分文件 | PASS（代码路径 + 本次只出现 work 的文件） |
| 两个实例同时在跑时 init-data 不会把 tk 的库写进 work | 未验证。两实例当前都是 stopped。已有 JSON 的 `_db_dir` 是分开的 |
| `remove --purge` 删 wxdata | PASS（单测）。真实实例未删 |
| 无 CGO、无 Windows/macOS scanner | PASS |
| sessions 字段 | PASS（现场 9 个字段齐全，`msg_type` 为「文本」） |
| history envelope 与 `messages[]` | PASS（含 `local_id`，`failures` 为 null） |
| contacts 列表与 `--detail` | PASS |
| members | PASS |
| new-messages 首次 `first_call`+`unread_count`，再次 `first_call:false`+`new_count` | PASS |
| `sessions nosuch` exit 1，stderr 含 `not found` | PASS |
| 未 init 的 `sessions work` exit 3 | 未验证。work 已经有密钥 |
| `init-data` 未启动 exit 4、无 ptrace exit 5 | 未验证。没有执行会扫内存的 `--force` |
| `history` 不存在的聊天 exit 1 | PASS |
| `members` 非群 exit 1 | PASS（`文件传输助手`） |
| 非法时间 exit 2、`--limit 501` exit 2、未知 `--type` exit 2 | PASS |
| schema 缺列 exit 6 | PASS（单测） |
| `mirai` 无 db_storage | PASS。`init-data mirai` 与 `sessions mirai` 都是 exit 1，文案含 `no WeChat data` |
| stop 之后 `sessions work` exit 0 | PASS。`wxctl status` 显示 work stopped |
| `Msg_` = md5、群前缀剥离、图片/文件占位、sender `me` | 前三项代码与单测 PASS。本次抽样没有单独断言某条消息的 sender 是 `me`，记为未验证 |
| 查询无需 sudo | PASS（本次命令都没有 sudo） |
| 冷解密 < 3s、命中后 sessions < 300ms | 未验证。work 缓存是热的 |

## PR 1–7

| PR | 结论 |
|----|------|
| 1 路径、sudo HOME、purge | PASS |
| 2 页解密与 HMAC | PASS。常量、AES 直接用 enc_key、HMAC 小端页号、WAL 大端都在 |
| 3 discover、key JSON、缓存、`file:` DSN | PASS。快照、`.snap-*` 清理、`mode=ro`、无 `_pragma=`、salt 不匹配返回 `ErrNoKeys` |
| 4 `init-data` 与 fail-closed 扫描 | PASS（代码与单测）。exit 4/5 未在本机实跑 |
| 5 schema、sessions/unread/contacts/members/new-messages、`format.go` | FAIL 1、2、3 落在这一层。命令本身和 JSON 字段是对的，`FormatMsgType` 不是 `"type=?"` 占位 |
| 6 history/search/chat-export | FAIL 1、4、6。严格正则 `message/message_\d+\.db$`、参数化 `LIKE ?`、无 `--media`、`chat-export` 用 `ToTextLine` 是对的 |
| 7 live 测试与 README | PASS。README 写了 sudo init-data、离线查询、wxdata 路径、`rm -rf .../cache`、zstd LIKE、WAL 快照、不要对任意可写路径 setcap。另有 FAIL 7（gofmt） |

## 必须 / 禁止 / 不要

上面 Key Decisions 和验收表里已经覆盖的，这里只列其余硬约束。

| 约束 | 结论 |
|------|------|
| 不 exec Python、不包装 wechat-cli、不改 wechat-cli | PASS |
| 不实现 stats / favorites / MCP / `--media` / Windows scanner | PASS |
| 不把状态写进伪造 HOME；`MkdirAll` 0700；密钥与明文 0600 | PASS |
| Discover 不用 `~/Documents/xwechat_files`，多候选按 message mtime，不交互 | PASS |
| `_db_dir` 缺失或变化 → exit 3，文案含 `init-data --force` | PASS（`wxdata.Open`） |
| `Close` 不删缓存；不解密 fts/biz/favorite 用于查询；不写 `-shm`；不用 `PRAGMA` 写业务数据 | PASS |
| 打开明文库只用 `file:` URI，两个 PRAGMA 在 Open 之后 Exec | PASS |
| hex 规则 64/96/>96、`cross_verify`、必需库 `session`+`contact`+至少一个 `message_N` | PASS |
| 进度走 stderr；JSON 成功走 stdout，`SetEscapeHTML(false)`，两空格缩进，末尾换行 | PASS |
| 表名只允许 `Msg_[0-9a-f]{32}`，keyword 不拼进 SQL | PASS |
| 分页：多表降序切片再升序输出；history 无 500 上限；search 最大 500 | PASS |
| 解压失败：sessions `'(压缩内容)'`，history `'(无法解压)'`，search 跳过该行 | PASS |
| 群摘要 `:\n` 后半；己方 `me`；appmsg 5/6/57/小程序占位；XXE 长度 20000 与 DOCTYPE/ENTITY | 代码 PASS。57 缺测试 |
| contacts `--detail` 用 `Flags().Changed`，找不到 exit 1，优先于 `--query` | PASS |
| members：非群 exit 1；owner 显示名，排序用原始 username | PASS |
| 查询 `--limit 0` 空列表 exit 0，且不算 exit 2 | PASS（代码）。负数 limit 走 exit 1 |
| `init-data` 成功删除 `.partial`；其它库缺 key 仍成功并打 `MISSING` | PASS |
| 不改 `show`、不加 `cache-clear`、不改 `export` 的 Use | PASS |
| `WXCTL_DEBUG=1` 打印解密进度 | 未实现。文档标为可选 |
| 冷启动与热查询的耗时预算 | 未验证 |
| sudo 实机 chown、两实例同时在跑时的 init-data | 未验证 |

结论：不能视为验收通过。先改 FAIL 1–4（错误不能被吞掉、联系人匹配顺序、游标锁、规定文案），再补测试和 `gofmt`。