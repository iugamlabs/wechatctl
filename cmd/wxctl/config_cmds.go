//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/instance"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get or set global configuration",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get [key]",
			Short: "Show config (all keys, or one key)",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := loadApp()
				if err != nil {
					return err
				}
				if len(args) == 0 {
					fmt.Printf("wechat_bin=%s\n", app.Config.WechatBin)
					fmt.Printf("profiles_root=%s\n", app.Config.ProfilesRoot)
					fmt.Printf("shared_dir=%s\n", app.Config.SharedDir)
					fmt.Printf("im_module=%s\n", app.Config.IMModule)
					return nil
				}
				v, err := config.GetField(app.Config, args[0])
				if err != nil {
					return err
				}
				fmt.Println(v)
				return nil
			},
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a config key",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := loadApp()
				if err != nil {
					return err
				}
				cfg := app.Config
				if err := config.SetField(&cfg, args[0], args[1]); err != nil {
					return err
				}
				toSave := config.RelativizeForSave(app.Layout.Home, cfg)
				if err := config.Save(app.Layout, toSave); err != nil {
					return err
				}
				fmt.Printf("set %s=%s\n", args[0], args[1])
				return nil
			},
		},
	)
	return cmd
}

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrate ~/.local/share/wechat-profiles into wxctl",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			migrated, err := mgr.Migrate()
			if err != nil {
				return err
			}
			if len(migrated) == 0 {
				fmt.Println("nothing to migrate")
				return nil
			}
			for _, name := range migrated {
				fmt.Printf("migrated: %s\n", name)
			}
			return nil
		},
	}
}

func exportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <file>",
		Short: "Export config and instance metadata (no chat data)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			if err := mgr.Export(args[0]); err != nil {
				return err
			}
			fmt.Printf("exported metadata to %s\n", args[0])
			return nil
		},
	}
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Import config and instance metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			if _, err := os.Stat(args[0]); err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			if err := mgr.Import(args[0]); err != nil {
				return err
			}
			fmt.Printf("imported metadata from %s\n", args[0])
			return nil
		},
	}
}
