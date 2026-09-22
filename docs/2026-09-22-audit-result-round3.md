第三轮没有新的 FAIL。上一轮 4 个 FAIL 在当前代码和实跑里都已成立。`CGO_ENABLED=0 go test ./... -count=1`、`go vet ./...`、`gofmt -l` 全绿。五个实例都是 stopped。`init-data work`（无 `--force`）exit 0 且跳过扫描；`init-data work --force` 现在是 exit 4，密钥文件前后都是 mtime `1790004811`、大小 `3003`、权限 `0600`。没有跑 `new-messages`。

## 上一轮 FAIL

| 上一轮问题 | 结论 |
|---|---|
| 多个 `--chat` 把打开库错误当成找不到聊天 | **PASS**。`ResolveChatContexts` 在 `ResolveChatContext` 出错时立刻 `return nil, nil, nil, resolveErr`。`search` 多聊分支把该错误原样返回 |
| 全局搜索枚举 `sqlite_master` 失败当空结果 | **PASS**。`loadSearchContextsFromDB` 查询失败返回 `err`，`SearchAllMessages` 立刻返回 |
| 微信未启动时 `init-data` 先报无 ptrace | **PASS**。先 `InstancePIDs`，空列表返回 `ErrNotRunning`。实跑 `--force` 的 stderr 是 `instance "work" is not running...`，exit 4 |
| README 写了不存在的 `WXCTL_DEBUG` | **PASS**。`decryptOneAttempt` 在 `WXCTL_DEBUG=1` 时向 stderr 打印 relKey、耗时和 WAL frames。`cache_test.go` 断言该行 |

## Key Decisions

| # | 结论 |
|---|---|
| 1 纯 Go，禁止 `mattn/go-sqlite3` | **PASS**。`go.mod` 是 `modernc.org/sqlite` 与 `klauspost/compress`。静态 ELF，`ldd` 报告不是动态可执行文件 |
| 2 顶层命令，实例名第一参数 | **PASS**。`--help` 有 9 个数据命令，没有 `data` 父命令 |
| 3 `chat-export`，不动元数据 `export` | **PASS** |
| 4 `wxdata/<name>`，purge 同时删除 | **PASS**（单测）。没有对真实实例执行 `--purge` |
| 5 禁止 `~/.wechat-cli` 和 `/tmp/wechat_cli_cache` | **PASS**。`/tmp/wechat_cli_cache` 时间仍是 2026-09-21 18:50 |
| 6 只扫本实例，禁止用 `belongsToInstance` 做 /proc 扫描 | **PASS**。`linux.go` 最后一次改动仍是 `6455b2e` |
| 7 history/search JSON 用结构体 | **PASS**。群 history 含 `local_id`，`failures` 为 JSON null |
| 8 不暴露 `--media` | **PASS** |
| 9 `--format` 默认值 | **PASS** |
| 10 schema 硬失败 exit 6 | **PASS**。打开库和 `sqlite_master` 枚举的错误向上返回。缺列 exit 6 仍由 `schema_test.go` 覆盖 |
| 11 sudo HOME、euid==0 忽略 XDG、chown 整树 | **PASS**（单测）。没有用 root 实跑 |
| 12 members 文本按成员列表循环 | **PASS**。抽样群 `member_count` 361，与 `members` 长度一致 |
| 13 独立 `wxdata.Error`，打印外层 err | **PASS**。`sessions nosuch` 的 stderr 是 `instance "nosuch" not found: instance not found`，exit 1 |
| 14 只有 `init-data` 要求进程在跑 | **PASS**。work 为 stopped 时 `sessions work --limit 1` exit 0 |
| 15 不留下可 skip 的残缺或 root 密钥 | **PASS**（单测与本次 skip）。校验通过后 exit 0，没有重扫 |

## Tests

| 文档要求 | 结论 |
|---|---|
| crypto 页 round-trip、错 key、WAL、尾部残渣 | **PASS** |
| keys：`..`、斜杠、hex、environ、maps fixture | **PASS** |
| `Msg_` + md5、时间三种格式、分页上下限 | **PASS** |
| `format_msg_type`、群前缀、sessions 解压占位 | **PASS** |
| appmsg 5/6/57，引用超过 160 字符加 `...` | **PASS** |
| history / search 黄金 JSON | **PASS** |
| discover、schema 缺列 exit 6 | **PASS** |
| cache：mtime、size、salt → `ErrNoKeys`、`WXCTL_DEBUG` | **PASS** |
| paths sudo 三分支、`data_output_test.go` | **PASS** |
| live 默认 Skip | **PASS**。未设置 `WXCTL_LIVE_INSTANCE` |
| `gofmt` | **PASS** |

## Acceptance Checklist

| 项 | 结论 |
|---|---|
| `--help` 含 9 个数据命令，并仍有 `export` / `import` / `start` / `stop` / `restart` | **PASS** |
| `chat-export` 是聊天导出；`export` 是元数据 | **PASS** |
| `sessions` 无参数 exit 1 | **PASS** |
| 无 `--media`、无 `stats` / `favorites` | **PASS** |
| 密钥在 `wxdata/<name>/all_keys.json`（0600）。work 与 tk 的 `_db_dir` 各自指向自己的 `db_storage` | **PASS** |
| 缓存目录 `wxdata/work/cache` 为 0700 | **PASS** |
| `new-messages` 游标按实例分文件 | **PASS**（路径）。work 有 `last_check.json`（0600），tk 没有。本轮没有再执行该命令 |
| 两个实例同时在跑时 init-data 不串库 | **未验证**。两个实例都是 stopped |
| `remove --purge` 删 wxdata | **PASS**（单测）。真实实例未删 |
| 无 CGO、无 Windows/macOS scanner | **PASS** |
| sessions 九个字段；抽样 `msg_type` 为「文本」，`time` 为 `09-21 23:31` | **PASS** |
| history envelope 与 `messages[]` | **PASS**。群抽样 `local_id` 4838，`time` 为 `2026-09-21 23:31`，`failures` 为 null |
| contacts 列表与找不到的 `--detail` exit 1 | **PASS** |
| members | **PASS** |
| new-messages 首次与再次的字段 | **未验证**（会写游标）。结构体与锁内读写符合文档 |
| `sessions nosuch` exit 1，stderr 含 `not found`，不含 `wechat exited` | **PASS** |
| 未 init 的 `sessions work` exit 3 | **未验证**。work 已经有密钥 |
| `init-data` 未启动 exit 4 | **PASS** |
| 无 ptrace 时 `init-data` exit 5 | **未验证**。进程未启动时先返回 exit 4，本轮到不了权限检查。`HasPtrace` 有单测 |
| `history` 不存在的聊天 exit 1 | **PASS** |
| `members` 非群 exit 1 | **PASS** |
| 非法时间、`--limit 501`、未知 `--type` 都是 exit 2 | **PASS** |
| schema 缺列 exit 6 | **PASS**（单测） |
| `mirai` 无 db_storage | **PASS**。`init-data` 与 `sessions` 都是 exit 1，文案含 `no WeChat data` |
| stop 之后 `sessions work` exit 0 | **PASS** |
| `Msg_` = md5、群前缀、图片/文件占位、sender `me` | **PASS**（代码与单测）。本轮抽样到的是他人消息。`history work filehelper` 的 stderr 是 `找不到 文件传输助手 的消息记录`，exit 1 |
| 查询无需 sudo | **PASS** |
| 冷解密 < 3s | **未验证**。work 缓存是热的 |

## 必须 / 禁止 / 不要

| 约束 | 结论 |
|---|---|
| 不 exec Python、不包装 wechat-cli、不改 wechat-cli | **PASS**。`/home/deali/code/2/wechat-cli` 只有未跟踪的 `uv.lock` |
| 不实现 stats / favorites / MCP / `--media` / Windows scanner | **PASS** |
| 查询命令不调用 `runtime.Probe` | **PASS** |
| 目录 0700；密钥与明文 0600；明文只用 `file:` URI 且 `mode=ro` | **PASS** |
| 表名只允许 `Msg_[0-9a-f]{32}`；消息库正则是 `^message/message_\d+\.db$`；keyword 用 `LIKE ?` | **PASS** |
| 解压失败占位按调用方区分；300/160 按 rune | **PASS** |
| `WXCTL_DEBUG=1` | **PASS** |
| sudo 实机 chown、两实例同时在跑时的 init-data、冷解密耗时、已有密钥时的 exit 3、进程在跑且无 ptrace 时的 exit 5 | **未验证** |