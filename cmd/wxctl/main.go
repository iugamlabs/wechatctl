package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"github.com/star-plan/wechatctl/internal/runtime"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		if ee, ok := err.(*runtime.ExitError); ok {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type appContext struct {
	Layout paths.Layout
	Config config.Config
}

func loadApp() (appContext, error) {
	layout, err := paths.DefaultLayout()
	if err != nil {
		return appContext{}, err
	}
	cfg, err := config.Load(layout)
	if err != nil {
		return appContext{}, err
	}
	return appContext{Layout: layout, Config: cfg}, nil
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "wxctl",
		Short:         "Manage multiple isolated WeChat instances",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		createCmd(),
		listCmd(),
		showCmd(),
		startCmd(),
		stopCmd(),
		restartCmd(),
		statusCmd(),
		editCmd(),
		removeCmd(),
		desktopCmd(),
		configCmd(),
		migrateCmd(),
		exportCmd(),
		importCmd(),
		watchFrameCmd(),
	)
	return cmd
}
