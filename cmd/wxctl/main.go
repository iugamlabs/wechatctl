//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"github.com/star-plan/wechatctl/internal/runtime"
	"github.com/star-plan/wechatctl/internal/wxdata"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		code, printErr := classifyRunError(err)
		if printErr {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(code)
	}
}

// classifyRunError 决定退出码以及是否打印外层 err。
// wxdata.Error 必须打印 wrap 后的全文（含 --force 提示）；ExitError 保持静默。
func classifyRunError(err error) (code int, printErr bool) {
	var we *wxdata.Error
	if errors.As(err, &we) {
		return we.Code, true
	}
	if ee, ok := err.(*runtime.ExitError); ok {
		return ee.Code, false
	}
	return 1, true
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
		initDataCmd(),
	)
	return cmd
}
