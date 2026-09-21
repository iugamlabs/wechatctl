//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/instance"
	"github.com/star-plan/wechatctl/internal/runtime"
	"github.com/star-plan/wechatctl/internal/wxdata"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
	"github.com/star-plan/wechatctl/internal/wxdata/query"
)

func initDataCmd() *cobra.Command {
	var force bool
	var format string
	cmd := &cobra.Command{
		Use:   "init-data <name>",
		Short: "Extract SQLCipher keys for an instance from its running WeChat process",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := config.ValidateName(name); err != nil {
				return err
			}
			if format != "json" && format != "text" {
				return fmt.Errorf("invalid --format %q (want json or text)", format)
			}

			imgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			if _, err := imgr.Get(name); err != nil {
				return fmt.Errorf("instance %q not found: %w", name, wxdata.ErrInstanceNotFound)
			}
			home := imgr.HomeDir(name)
			dbDir, err := wxdata.DiscoverDBDir(home)
			if err != nil {
				return err
			}

			stateDir := app.Layout.WxdataDir(name)
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				return err
			}
			keysPath := keys.KeysPath(stateDir)

			owner, err := wxdata.ResolveOwner()
			if err != nil {
				return err
			}

			if !force {
				if res, ok := tryInitDataSkip(name, dbDir, stateDir, keysPath, owner); ok {
					return emitInitData(format, res)
				}
			}

			if !keys.HasPtrace() {
				return fmt.Errorf("need root or CAP_SYS_PTRACE to read WeChat process memory\n  sudo wxctl init-data %s\n  or: sudo setcap cap_sys_ptrace=ep $(command -v wxctl): %w", name, wxdata.ErrPermission)
			}

			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			pids := rt.InstancePIDs(name)
			if len(pids) == 0 {
				return fmt.Errorf("instance %q is not running (start it, then retry init-data): %w", name, wxdata.ErrNotRunning)
			}

			files, saltToDBs := keys.CollectDBFiles(dbDir)
			if len(files) == 0 {
				return fmt.Errorf("no decryptable .db files in %s: %w", dbDir, wxdata.ErrNoKeys)
			}

			keyMap := keys.ExtractFromPIDs(pids, files, saltToDBs, os.Stderr)
			entries, missing := keys.BuildEntries(files, keyMap)
			if len(entries) == 0 {
				return fmt.Errorf("no keys extracted: %w", wxdata.ErrNoKeys)
			}
			for _, rel := range missing {
				fmt.Fprintf(os.Stderr, "MISSING: %s\n", rel)
			}
			if miss := keys.MissingRequired(files, entries); len(miss) > 0 {
				wxdata.WritePartial(stateDir, dbDir, entries)
				return fmt.Errorf("missing required keys %v: %w", miss, wxdata.ErrNoKeys)
			}

			if err := wxdata.PersistKeys(stateDir, dbDir, entries, owner); err != nil {
				return err
			}

			res := initDataResult{
				Instance: name,
				DBDir:    dbDir,
				KeysFile: keysPath,
				Keys:     len(entries),
				Missing:  missing,
			}
			if res.Missing == nil {
				res.Missing = []string{}
			}
			return emitInitData(format, res)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "rescan and overwrite all_keys.json")
	cmd.Flags().StringVar(&format, "format", "text", "json|text")
	return cmd
}

func sessionsCmd() *cobra.Command {
	var limit int
	var format string
	cmd := &cobra.Command{
		Use:   "sessions <name>",
		Short: "List recent chat sessions for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("limit must be >= 0")
			}
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				var items []query.SessionItem
				if limit > 0 {
					items, err = query.ListSessions(store, book, limit)
				} else {
					items = []query.SessionItem{}
				}
				if err != nil {
					return err
				}
				return emitQuery(format, items, func(w io.Writer) { writeSessionsText(w, items) })
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "max sessions to return (0 for empty list)")
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func unreadCmd() *cobra.Command {
	var limit int
	var format string
	cmd := &cobra.Command{
		Use:   "unread <name>",
		Short: "List sessions with unread messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("limit must be >= 0")
			}
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				var items []query.SessionItem
				if limit > 0 {
					items, err = query.ListUnread(store, book, limit)
				} else {
					items = []query.SessionItem{}
				}
				if err != nil {
					return err
				}
				return emitQuery(format, items, func(w io.Writer) { writeUnreadText(w, items) })
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max sessions to return (0 for empty list)")
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func newMessagesCmd() *cobra.Command {
	var format string
	var reset bool
	cmd := &cobra.Command{
		Use:   "new-messages <name>",
		Short: "List new messages since the last check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				res, err := query.RunNewMessages(store, book, reset)
				if err != nil {
					return err
				}
				return emitQuery(format, res, func(w io.Writer) { writeNewMessagesText(w, res) })
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	cmd.Flags().BoolVar(&reset, "reset", false, "reset last_check.json and treat as first call")
	return cmd
}

func contactsCmd() *cobra.Command {
	var queryStr string
	var detail string
	var limit int
	var format string
	cmd := &cobra.Command{
		Use:   "contacts <name>",
		Short: "Search or list contacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("limit must be >= 0")
			}
			detailSet := cmd.Flags().Changed("detail")
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, db, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				if detailSet {
					username := book.ResolveUsername(detail)
					if username == "" {
						username = detail
					}
					info, err := query.GetContactDetail(db, username)
					if err != nil {
						return err
					}
					if info == nil {
						return fmt.Errorf("contact %q not found: %w", detail, wxdata.ErrChatNotFound)
					}
					return emitQuery(format, info, func(w io.Writer) { writeContactDetailText(w, info) })
				}
				var matched []query.ContactRow
				if limit > 0 {
					matched = query.FilterContacts(book.Full(), queryStr, limit)
				} else {
					matched = []query.ContactRow{}
				}
				return emitQuery(format, matched, func(w io.Writer) { writeContactsText(w, matched) })
			})
		},
	}
	cmd.Flags().StringVar(&queryStr, "query", "", "search nick_name, remark, or username")
	cmd.Flags().StringVar(&detail, "detail", "", "show one contact by name or wxid")
	cmd.Flags().IntVar(&limit, "limit", 50, "max contacts (0 for empty list)")
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func historyCmd() *cobra.Command {
	var limit, offset int
	var startTime, endTime, msgType, format string
	cmd := &cobra.Command{
		Use:   "history <name> <chat>",
		Short: "Get chat message history for an instance",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			if err := ValidateHistoryArgs(limit, offset, startTime, endTime, msgType); err != nil {
				return err
			}
			typeFilter, err := query.ParseMsgTypeFilter(msgType)
			if err != nil {
				return wrapInvalidArgs(err)
			}
			startTS, endTS, err := query.ParseTimeRange(startTime, endTime)
			if err != nil {
				return wrapInvalidArgs(err)
			}
			chatName := args[1]
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				ctx, err := query.ResolveChatContext(store, book, chatName)
				if err != nil {
					return err
				}
				if ctx == nil {
					return fmt.Errorf("chat %q not found: %w", chatName, wxdata.ErrChatNotFound)
				}
				if len(ctx.MessageTables) == 0 {
					return fmt.Errorf("找不到 %s 的消息记录: %w", ctx.DisplayName, wxdata.ErrChatNotFound)
				}
				messages, failures, err := query.CollectChatHistory(store, book, ctx, startTS, endTS, limit, offset, typeFilter)
				if err != nil {
					return err
				}
				res := query.BuildHistoryResult(ctx, messages, failures, startTime, endTime, msgType, limit, offset)
				return emitQuery(format, res, func(w io.Writer) { writeHistoryText(w, res) })
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max messages to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "pagination offset")
	cmd.Flags().StringVar(&startTime, "start-time", "", "start time YYYY-MM-DD [HH:MM[:SS]]")
	cmd.Flags().StringVar(&endTime, "end-time", "", "end time YYYY-MM-DD [HH:MM[:SS]]")
	cmd.Flags().StringVar(&msgType, "type", "", "filter: text|image|voice|video|sticker|location|link|file|call|system")
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func searchCmd() *cobra.Command {
	var chats []string
	var startTime, endTime, msgType, format string
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "search <name> <keyword>",
		Short: "Search message content",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			if err := query.ValidatePagination(limit, offset, 500); err != nil {
				return wrapInvalidArgs(err)
			}
			typeFilter, err := query.ParseMsgTypeFilter(msgType)
			if err != nil {
				return wrapInvalidArgs(err)
			}
			startTS, endTS, err := query.ParseTimeRange(startTime, endTime)
			if err != nil {
				return wrapInvalidArgs(err)
			}
			keyword := args[1]
			candidateLimit := limit + offset
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				var hits []query.SearchHit
				var failures []string
				var scope string

				switch len(chats) {
				case 0:
					hits, failures, err = query.SearchAllMessages(store, book, keyword, startTS, endTS, candidateLimit, typeFilter)
					if err != nil {
						return err
					}
					scope = "全部消息"
				case 1:
					ctx, err := query.ResolveChatContext(store, book, chats[0])
					if err != nil {
						return err
					}
					if ctx == nil {
						return fmt.Errorf("chat %q not found: %w", chats[0], wxdata.ErrChatNotFound)
					}
					if len(ctx.MessageTables) == 0 {
						return fmt.Errorf("找不到 %s 的消息记录: %w", ctx.DisplayName, wxdata.ErrChatNotFound)
					}
					hits, failures, err = query.CollectChatSearch(store, book, ctx, keyword, startTS, endTS, candidateLimit, typeFilter)
					if err != nil {
						return err
					}
					scope = ctx.DisplayName
				default:
					resolved, unresolved, _ := query.ResolveChatContexts(store, book, chats)
					if len(resolved) == 0 {
						return fmt.Errorf("no searchable chats: %w", wxdata.ErrChatNotFound)
					}
					for _, ctx := range resolved {
						h, f, err := query.CollectChatSearch(store, book, ctx, keyword, startTS, endTS, candidateLimit, typeFilter)
						if err != nil {
							return err
						}
						hits = append(hits, h...)
						failures = append(failures, f...)
					}
					if len(unresolved) > 0 {
						failures = append(failures, "未找到: "+strings.Join(unresolved, "、"))
					}
					scope = fmt.Sprintf("%d 个聊天对象", len(resolved))
				}

				paged := query.PageSearchHits(hits, limit, offset)
				res := &query.SearchResult{
					Scope:     scope,
					Keyword:   keyword,
					Count:     len(paged),
					Offset:    offset,
					Limit:     limit,
					StartTime: optionalStringPtr(startTime),
					EndTime:   optionalStringPtr(endTime),
					Type:      optionalStringPtr(msgType),
					Results:   paged,
					Failures:  failuresPtr(failures),
				}
				if res.Results == nil {
					res.Results = []query.SearchHit{}
				}
				return emitQuery(format, res, func(w io.Writer) { writeSearchText(w, res) })
			})
		},
	}
	cmd.Flags().StringArrayVar(&chats, "chat", nil, "limit to chat (repeatable)")
	cmd.Flags().StringVar(&startTime, "start-time", "", "start time")
	cmd.Flags().StringVar(&endTime, "end-time", "", "end time")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results (max 500)")
	cmd.Flags().IntVar(&offset, "offset", 0, "pagination offset")
	cmd.Flags().StringVar(&msgType, "type", "", "message type filter")
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func chatExportCmd() *cobra.Command {
	var format, outputPath, startTime, endTime string
	var limit int
	cmd := &cobra.Command{
		Use:   "chat-export <name> <chat>",
		Short: "Export chat history to markdown or plain text",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "markdown" && format != "txt" {
				return fmt.Errorf("invalid --format %q (want markdown or txt)", format)
			}
			if err := query.ValidatePagination(limit, 0, 0); err != nil {
				return wrapInvalidArgs(err)
			}
			startTS, endTS, err := query.ParseTimeRange(startTime, endTime)
			if err != nil {
				return wrapInvalidArgs(err)
			}
			chatName := args[1]
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, _, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				ctx, err := query.ResolveChatContext(store, book, chatName)
				if err != nil {
					return err
				}
				if ctx == nil {
					return fmt.Errorf("chat %q not found: %w", chatName, wxdata.ErrChatNotFound)
				}
				if len(ctx.MessageTables) == 0 {
					return fmt.Errorf("找不到 %s 的消息记录: %w", ctx.DisplayName, wxdata.ErrChatNotFound)
				}
				messages, _, err := query.CollectChatHistory(store, book, ctx, startTS, endTS, limit, 0, nil)
				if err != nil {
					return err
				}
				if len(messages) == 0 {
					fmt.Fprintf(os.Stderr, "%s 无消息记录\n", ctx.DisplayName)
					return nil
				}
				content := formatChatExport(format, ctx.DisplayName, ctx.IsGroup, startTime, endTime, messages)
				if outputPath == "" {
					fmt.Fprint(os.Stdout, content)
					if !strings.HasSuffix(content, "\n") {
						fmt.Fprintln(os.Stdout)
					}
					return nil
				}
				out := content
				if !strings.HasSuffix(out, "\n") {
					out += "\n"
				}
				if err := os.WriteFile(outputPath, []byte(out), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "已导出到: %s（%d 条消息）\n", outputPath, len(messages))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "markdown|txt")
	cmd.Flags().StringVar(&outputPath, "output", "", "output file (default stdout)")
	cmd.Flags().StringVar(&startTime, "start-time", "", "start time")
	cmd.Flags().StringVar(&endTime, "end-time", "", "end time")
	cmd.Flags().IntVar(&limit, "limit", 500, "max messages to export")
	return cmd
}

func ValidateHistoryArgs(limit, offset int, startTime, endTime, msgType string) error {
	if err := query.ValidatePagination(limit, offset, 0); err != nil {
		return wrapInvalidArgs(err)
	}
	if _, err := query.ParseMsgTypeFilter(msgType); err != nil {
		return wrapInvalidArgs(err)
	}
	_, _, err := query.ParseTimeRange(startTime, endTime)
	if err != nil {
		return wrapInvalidArgs(err)
	}
	return nil
}

func wrapInvalidArgs(err error) error {
	return fmt.Errorf("%w: %v", wxdata.ErrInvalidArgs, err)
}

func optionalStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func failuresPtr(failures []string) *[]string {
	if len(failures) == 0 {
		return nil
	}
	return &failures
}

func membersCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "members <name> <group>",
		Short: "List members of a group chat",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkQueryFormat(format); err != nil {
				return err
			}
			groupName := args[1]
			return runWithStore(args[0], func(store *wxdata.Store) error {
				book, db, err := query.LoadBook(store)
				if err != nil {
					return err
				}
				username := book.ResolveUsername(groupName)
				if username == "" {
					return fmt.Errorf("chat %q not found: %w", groupName, wxdata.ErrChatNotFound)
				}
				if !strings.Contains(username, "@chatroom") {
					return fmt.Errorf("%q is not a group chat: %w", groupName, wxdata.ErrNotGroup)
				}
				res, err := query.GetGroupMembers(db, book, username)
				if err != nil {
					return err
				}
				res.Group = book.DisplayName(username)
				res.Username = username
				res.MemberCount = len(res.Members)
				return emitQuery(format, res, func(w io.Writer) { writeMembersText(w, res) })
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "json|text")
	return cmd
}

func checkQueryFormat(format string) error {
	if format != "json" && format != "text" {
		return fmt.Errorf("invalid --format %q (want json or text)", format)
	}
	return nil
}

func runWithStore(name string, fn func(*wxdata.Store) error) error {
	app, err := loadApp()
	if err != nil {
		return err
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}
	imgr := instance.Manager{Layout: app.Layout, Config: app.Config}
	if _, err := imgr.Get(name); err != nil {
		return fmt.Errorf("instance %q not found: %w", name, wxdata.ErrInstanceNotFound)
	}
	store, err := wxdata.Open(app.Layout, app.Config, name)
	if err != nil {
		return err
	}
	defer store.Close()
	return fn(store)
}

func emitQuery(format string, jsonVal interface{}, textFn func(io.Writer)) error {
	if format == "json" {
		return writeJSON(os.Stdout, jsonVal)
	}
	textFn(os.Stdout)
	return nil
}

func tryInitDataSkip(name, dbDir, stateDir, keysPath string, owner *wxdata.Owner) (initDataResult, bool) {
	if _, err := os.Stat(keysPath); err != nil {
		return initDataResult{}, false
	}
	if err := wxdata.ValidateExistingKeys(keysPath, dbDir); err != nil {
		return initDataResult{}, false
	}
	if err := wxdata.EnsureOwned(stateDir, owner); err != nil {
		return initDataResult{}, false
	}
	// euid==0 时 EnsureOwned 已 chown；再验一次以免 chown 后文件仍不可用。
	if err := wxdata.ValidateExistingKeys(keysPath, dbDir); err != nil {
		return initDataResult{}, false
	}
	_, entries, err := keys.Load(keysPath)
	if err != nil {
		return initDataResult{}, false
	}
	files, _ := keys.CollectDBFiles(dbDir)
	_, missing := keys.BuildEntries(files, saltKeyMap(entries))
	if missing == nil {
		missing = []string{}
	}
	return initDataResult{
		Instance: name,
		DBDir:    dbDir,
		KeysFile: keysPath,
		Keys:     len(entries),
		Missing:  missing,
	}, true
}

func saltKeyMap(entries map[string]keys.KeyInfo) map[string]string {
	m := make(map[string]string)
	for _, info := range entries {
		if info.Salt != "" && info.EncKey != "" {
			m[info.Salt] = info.EncKey
		}
	}
	return m
}

func emitInitData(format string, res initDataResult) error {
	if res.Missing == nil {
		res.Missing = []string{}
	}
	if format == "json" {
		return writeJSON(os.Stdout, res)
	}
	writeInitDataText(os.Stdout, res)
	return nil
}
