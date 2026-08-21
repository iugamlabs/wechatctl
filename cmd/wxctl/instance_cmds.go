package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/backend"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/instance"
	"github.com/star-plan/wechatctl/internal/runtime"
)

func createCmd() *cobra.Command {
	var alias, note string
	var tags []string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a WeChat instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			inst, err := mgr.Create(instance.CreateOptions{
				Name:  args[0],
				Alias: alias,
				Tags:  tags,
				Note:  note,
			})
			if err != nil {
				return err
			}
			fmt.Printf("created instance %q\n", inst.Name)
			fmt.Printf("  backend:  %s\n", instanceBackend(inst))
			fmt.Printf("  home:     %s\n", mgr.HomeDir(inst.Name))
			fmt.Printf("  shared:   %s\n", mgr.SharedDir(inst))
			fmt.Printf("  desktop:  %s\n", app.Layout.DesktopFile(inst.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "display name (can be Chinese)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	cmd.Flags().StringVar(&note, "note", "", "free-form note")
	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List WeChat instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			list, err := mgr.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("no instances")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tBACKEND\tUSER\tSTATUS\tPID\tSIZE\tALIAS\tNOTE")
			for _, inst := range list {
				st := rt.Probe(inst.Name)
				status := "stopped"
				pid := "-"
				if st.Running {
					status = "running"
					pid = fmt.Sprintf("%d", st.PID)
				}
				size, _ := mgr.DataSize(inst.Name)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					inst.Name,
					instanceBackend(inst),
					mgr.DisplayUser(inst),
					status,
					pid,
					humanSize(size),
					inst.Alias,
					inst.Note,
				)
			}
			return w.Flush()
		},
	}
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show instance details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			inst, err := mgr.Get(args[0])
			if err != nil {
				return err
			}
			cfg := app.Config.Resolve(app.Layout)
			st := rt.Probe(inst.Name)
			size, _ := mgr.DataSize(inst.Name)
			fmt.Printf("name:       %s\n", inst.Name)
			fmt.Printf("alias:      %s\n", inst.Alias)
			fmt.Printf("tags:       %s\n", strings.Join(inst.Tags, ", "))
			fmt.Printf("note:       %s\n", inst.Note)
			fmt.Printf("created:    %s\n", inst.CreatedAt.Format(time.RFC3339))
			fmt.Printf("backend:    %s\n", instanceBackend(inst))
			fmt.Printf("home:       %s\n", mgr.HomeDir(inst.Name))
			fmt.Printf("desktop:    %s\n", app.Layout.DesktopFile(inst.Name))
			fmt.Printf("shared:     %s\n", mgr.SharedDir(inst))
			fmt.Printf("wechat_bin: %s\n", inst.EffectiveWechatBin(cfg))
			if inst.EffectiveIMModule(cfg) != "" {
				fmt.Printf("im_module:  %s\n", inst.EffectiveIMModule(cfg))
			}
			fmt.Printf("size:       %s\n", humanSize(size))
			if st.Running {
				fmt.Printf("status:     running (pid %d)\n", st.PID)
			} else {
				fmt.Printf("status:     stopped\n")
			}
			return nil
		},
	}
}

func editCmd() *cobra.Command {
	var (
		alias     string
		note      string
		tags      []string
		wechatBin string
		imModule  string
		setAlias  bool
		setNote   bool
		setTags   bool
		setBin    bool
		setIM     bool
	)
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit instance metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			opts := instance.EditOptions{}
			if setAlias {
				opts.Alias = &alias
			}
			if setNote {
				opts.Note = &note
			}
			if setTags {
				opts.Tags = &tags
			}
			if setBin {
				opts.WechatBin = &wechatBin
			}
			if setIM {
				opts.IMModule = &imModule
			}
			if opts.Alias == nil && opts.Note == nil && opts.Tags == nil && opts.WechatBin == nil && opts.IMModule == nil {
				return fmt.Errorf("specify at least one of --alias, --note, --tags, --wechat-bin, --im-module")
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			inst, err := mgr.Edit(args[0], opts)
			if err != nil {
				return err
			}
			fmt.Printf("updated instance %q\n", inst.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "display name")
	cmd.Flags().StringVar(&note, "note", "", "note")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "tags (replaces existing)")
	cmd.Flags().StringVar(&wechatBin, "wechat-bin", "", "per-instance wechat binary override")
	cmd.Flags().StringVar(&imModule, "im-module", "", "per-instance IM module override")
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		setAlias = cmd.Flags().Changed("alias")
		setNote = cmd.Flags().Changed("note")
		setTags = cmd.Flags().Changed("tags")
		setBin = cmd.Flags().Changed("wechat-bin")
		setIM = cmd.Flags().Changed("im-module")
	}
	return cmd
}

func removeCmd() *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Unregister an instance (keeps data unless --purge)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			if err := mgr.Remove(args[0], instance.RemoveOptions{
				Purge:  purge,
				Yes:    yes,
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
			}); err != nil {
				return err
			}
			if purge {
				fmt.Printf("removed instance %q and deleted data\n", args[0])
			} else {
				fmt.Printf("unregistered instance %q (data kept at %s)\n", args[0], mgr.HomeDir(args[0]))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete instance data directory")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation when using --purge")
	return cmd
}

func instanceBackend(inst config.Instance) string {
	if name := inst.EffectiveBackend(); name != "" {
		return name
	}
	return backend.DefaultName()
}

func humanSize(n int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fG", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fM", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1fK", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
