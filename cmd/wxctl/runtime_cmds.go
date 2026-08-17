package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/backend"
	"github.com/star-plan/wechatctl/internal/instance"
	"github.com/star-plan/wechatctl/internal/runtime"
)

func startCmd() *cobra.Command {
	var detach bool
	cmd := &cobra.Command{
		Use:   "start <name> [-- wechat-args...]",
		Short: "Start WeChat for an instance",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			name := args[0]
			extra := args[1:]
			imgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			inst, err := imgr.Get(name)
			if err != nil {
				return err
			}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			if st := rt.Probe(name); st.Running {
				return fmt.Errorf("instance %q is already running (pid %d)", name, st.PID)
			}
			if err := rt.StartWith(inst, backend.StartOptions{ExtraArgs: extra, Detach: detach}); err != nil {
				return err
			}
			if detach {
				fmt.Printf("started instance %q\n", name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "start and return immediately")
	return cmd
}

func stopCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a running WeChat instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			if err := rt.Stop(args[0], timeout); err != nil {
				return err
			}
			fmt.Printf("stopped instance %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "wait before force kill")
	return cmd
}

func restartCmd() *cobra.Command {
	var timeout time.Duration
	var detach bool
	cmd := &cobra.Command{
		Use:   "restart <name> [-- wechat-args...]",
		Short: "Restart a WeChat instance",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			name := args[0]
			extra := args[1:]
			imgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			inst, err := imgr.Get(name)
			if err != nil {
				return err
			}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			if st := rt.Probe(name); st.Running {
				if err := rt.Stop(name, timeout); err != nil {
					return err
				}
			}
			if err := rt.StartWith(inst, backend.StartOptions{ExtraArgs: extra, Detach: detach}); err != nil {
				return err
			}
			if detach {
				fmt.Printf("restarted instance %q\n", name)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "wait before force kill when stopping")
	cmd.Flags().BoolVar(&detach, "detach", false, "start and return immediately")
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show running status of all instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			imgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			list, err := imgr.List()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(list))
			for _, inst := range list {
				names = append(names, inst.Name)
			}
			rt := runtime.Manager{Layout: app.Layout, Config: app.Config}
			statuses := rt.StatusAll(names)
			if len(statuses) == 0 {
				fmt.Println("no instances")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tPID")
			for _, st := range statuses {
				status := "stopped"
				pid := "-"
				if st.Running {
					status = "running"
					pid = fmt.Sprintf("%d", st.PID)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", st.Name, status, pid)
			}
			return w.Flush()
		},
	}
}

func desktopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Manage desktop / Start Menu launchers",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Regenerate launchers from the registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}
			mgr := instance.Manager{Layout: app.Layout, Config: app.Config}
			if err := mgr.SyncDesktops(); err != nil {
				return err
			}
			fmt.Println("desktop files synced")
			return nil
		},
	})
	return cmd
}
