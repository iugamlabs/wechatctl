package instance

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/desktop"
	"github.com/star-plan/wechatctl/internal/paths"
)

// Manager coordinates instance lifecycle against config + filesystem.
type Manager struct {
	Layout paths.Layout
	Config config.Config
}

func (m Manager) resolved() config.Config {
	return m.Config.Resolve(m.Layout)
}

func (m Manager) profilesRoot() string {
	return m.resolved().ProfilesRoot
}

func (m Manager) sharedDir() string {
	return m.resolved().SharedDir
}

func (m Manager) instanceHome(name string) string {
	return filepath.Join(m.profilesRoot(), name)
}

// CreateOptions for creating an instance.
type CreateOptions struct {
	Name  string
	Alias string
	Tags  []string
	Note  string
}

// Create registers a new instance, prepares its HOME, and writes a desktop file.
func (m Manager) Create(opts CreateOptions) (config.Instance, error) {
	if err := config.ValidateName(opts.Name); err != nil {
		return config.Instance{}, err
	}
	if err := config.EnsureDirs(m.Layout, m.Config); err != nil {
		return config.Instance{}, err
	}

	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return config.Instance{}, err
	}
	inst := config.Instance{
		Name:      opts.Name,
		Alias:     opts.Alias,
		Tags:      opts.Tags,
		Note:      opts.Note,
		CreatedAt: time.Now(),
	}
	if err := reg.Add(inst); err != nil {
		return config.Instance{}, err
	}

	home := m.instanceHome(opts.Name)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return config.Instance{}, err
	}
	if err := ensureSharedLink(home, m.sharedDir()); err != nil {
		return config.Instance{}, err
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		return config.Instance{}, err
	}
	if err := desktop.Write(m.Layout, inst); err != nil {
		return config.Instance{}, err
	}
	return inst, nil
}

func ensureSharedLink(instanceHome, sharedDir string) error {
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	link := filepath.Join(instanceHome, "Shared")
	if _, err := os.Lstat(link); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(sharedDir, link)
}

// List returns all registered instances.
func (m Manager) List() ([]config.Instance, error) {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return nil, err
	}
	return reg.Instances, nil
}

// Get returns one instance.
func (m Manager) Get(name string) (config.Instance, error) {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return config.Instance{}, err
	}
	inst, ok := reg.Get(name)
	if !ok {
		return config.Instance{}, fmt.Errorf("instance %q not found", name)
	}
	return inst, nil
}

// HomeDir returns the fake HOME path for an instance (may exist even if unregistered).
func (m Manager) HomeDir(name string) string {
	return m.instanceHome(name)
}

// DataSize returns approximate disk usage of the instance home.
func (m Manager) DataSize(name string) (int64, error) {
	var total int64
	root := m.instanceHome(name)
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// EditOptions for updating metadata.
type EditOptions struct {
	Alias    *string
	Tags     *[]string
	Note     *string
	WechatBin *string
	IMModule *string
}

// Edit updates instance metadata and refreshes desktop.
func (m Manager) Edit(name string, opts EditOptions) (config.Instance, error) {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return config.Instance{}, err
	}
	inst, ok := reg.Get(name)
	if !ok {
		return config.Instance{}, fmt.Errorf("instance %q not found", name)
	}
	if opts.Alias != nil {
		inst.Alias = *opts.Alias
	}
	if opts.Tags != nil {
		inst.Tags = *opts.Tags
	}
	if opts.Note != nil {
		inst.Note = *opts.Note
	}
	if opts.WechatBin != nil {
		inst.WechatBin = *opts.WechatBin
	}
	if opts.IMModule != nil {
		inst.IMModule = *opts.IMModule
	}
	if err := reg.Update(inst); err != nil {
		return config.Instance{}, err
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		return config.Instance{}, err
	}
	if err := desktop.Write(m.Layout, inst); err != nil {
		return config.Instance{}, err
	}
	return inst, nil
}

// RemoveOptions controls remove behavior.
type RemoveOptions struct {
	Purge  bool
	Yes    bool
	Stdin  io.Reader
	Stdout io.Writer
}

// Remove unregisters an instance; with Purge also deletes data.
func (m Manager) Remove(name string, opts RemoveOptions) error {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return err
	}
	if _, err := reg.Remove(name); err != nil {
		return err
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		return err
	}
	_ = desktop.Remove(m.Layout, name)

	if !opts.Purge {
		return nil
	}

	home := m.instanceHome(name)
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return nil
	}
	if !opts.Yes {
		in := opts.Stdin
		if in == nil {
			in = os.Stdin
		}
		out := opts.Stdout
		if out == nil {
			out = os.Stdout
		}
		fmt.Fprintf(out, "This will permanently delete data at %s\nType the instance name %q to confirm: ", home, name)
		reader := bufio.NewReader(in)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		if strings.TrimSpace(line) != name {
			return fmt.Errorf("confirmation failed; data not deleted (instance already unregistered)")
		}
	}
	return os.RemoveAll(home)
}

// SyncDesktops regenerates all desktop files for registered instances.
func (m Manager) SyncDesktops() error {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Layout.ApplicationsDir, 0o755); err != nil {
		return err
	}
	// Remove stale wxctl-*.desktop then rewrite
	entries, err := os.ReadDir(m.Layout.ApplicationsDir)
	if err != nil {
		return err
	}
	registered := make(map[string]struct{}, len(reg.Instances))
	for _, inst := range reg.Instances {
		registered[inst.Name] = struct{}{}
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, paths.DesktopPrefix) || !strings.HasSuffix(name, ".desktop") {
			continue
		}
		instName := strings.TrimSuffix(strings.TrimPrefix(name, paths.DesktopPrefix), ".desktop")
		if _, ok := registered[instName]; !ok {
			_ = os.Remove(filepath.Join(m.Layout.ApplicationsDir, name))
		}
	}
	for _, inst := range reg.Instances {
		if err := desktop.Write(m.Layout, inst); err != nil {
			return err
		}
	}
	return nil
}

// Migrate moves legacy wechat-profiles into wxctl layout.
func (m Manager) Migrate() (migrated []string, err error) {
	legacy := m.Layout.LegacyRoot
	st, err := os.Stat(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("legacy directory not found: %s", legacy)
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("legacy path is not a directory: %s", legacy)
	}
	if err := config.EnsureDirs(m.Layout, m.Config); err != nil {
		return nil, err
	}
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(legacy)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if err := config.ValidateName(name); err != nil {
			continue
		}
		src := filepath.Join(legacy, name)
		dst := m.instanceHome(name)
		if _, ok := reg.Get(name); ok {
			migrated = append(migrated, name+" (already registered, skipped move)")
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			// destination exists: register only
			inst := config.Instance{Name: name, CreatedAt: time.Now(), Note: "migrated (data already at destination)"}
			_ = reg.Add(inst)
			_ = desktop.Write(m.Layout, inst)
			migrated = append(migrated, name+" (registered existing dest)")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return migrated, err
		}
		if err := os.Rename(src, dst); err != nil {
			return migrated, fmt.Errorf("move %s: %w", name, err)
		}
		if err := ensureSharedLink(dst, m.sharedDir()); err != nil {
			return migrated, err
		}
		inst := config.Instance{Name: name, CreatedAt: time.Now(), Note: "migrated from wechat-profiles"}
		if err := reg.Add(inst); err != nil {
			return migrated, err
		}
		if err := desktop.Write(m.Layout, inst); err != nil {
			return migrated, err
		}
		migrated = append(migrated, name)
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		return migrated, err
	}
	return migrated, nil
}

// ExportBundle is metadata-only backup.
type ExportBundle struct {
	Config    config.Config    `toml:"config"`
	Instances []config.Instance `toml:"instance"`
}

// Export writes config + instances metadata to path.
func (m Manager) Export(path string) error {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return err
	}
	bundle := ExportBundle{
		Config:    config.RelativizeForSave(m.Layout.Home, m.Config),
		Instances: reg.Instances,
	}
	data, err := marshalBundle(bundle)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Import loads metadata from path and merges into registry (creates dirs + desktops).
func (m Manager) Import(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var bundle ExportBundle
	if err := unmarshalBundle(data, &bundle); err != nil {
		return err
	}
	if bundle.Config.WechatBin != "" || bundle.Config.ProfilesRoot != "" || bundle.Config.SharedDir != "" || bundle.Config.IMModule != "" {
		merged := m.Config
		if bundle.Config.WechatBin != "" {
			merged.WechatBin = bundle.Config.WechatBin
		}
		if bundle.Config.ProfilesRoot != "" {
			merged.ProfilesRoot = bundle.Config.ProfilesRoot
		}
		if bundle.Config.SharedDir != "" {
			merged.SharedDir = bundle.Config.SharedDir
		}
		if bundle.Config.IMModule != "" {
			merged.IMModule = bundle.Config.IMModule
		}
		if err := config.Save(m.Layout, config.RelativizeForSave(m.Layout.Home, merged)); err != nil {
			return err
		}
		m.Config = merged
	}
	if err := config.EnsureDirs(m.Layout, m.Config); err != nil {
		return err
	}
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return err
	}
	for _, inst := range bundle.Instances {
		if err := config.ValidateName(inst.Name); err != nil {
			continue
		}
		home := m.instanceHome(inst.Name)
		_ = os.MkdirAll(home, 0o755)
		_ = ensureSharedLink(home, m.sharedDir())
		if _, ok := reg.Get(inst.Name); ok {
			_ = reg.Update(inst)
		} else {
			if inst.CreatedAt.IsZero() {
				inst.CreatedAt = time.Now()
			}
			_ = reg.Add(inst)
		}
		_ = desktop.Write(m.Layout, inst)
	}
	return config.SaveRegistry(m.Layout, reg)
}
