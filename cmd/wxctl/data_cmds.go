//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/instance"
	"github.com/star-plan/wechatctl/internal/runtime"
	"github.com/star-plan/wechatctl/internal/wxdata"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
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
