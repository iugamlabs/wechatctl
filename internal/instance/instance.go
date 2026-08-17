package instance

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/star-plan/wechatctl/internal/backend"
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/desktop"
	"github.com/star-plan/wechatctl/internal/paths"
)

// Manager coordinates instance lifecycle against config + filesystem.
type Manager struct {
	Layout paths.Layout
	Config config.Config
}

func (m Manager) backend() backend.Backend {
	return backend.New(m.Layout, m.Config)
}

func (m Manager) instanceHome(name string) string {
	return m.backend().HomeDir(config.Instance{Name: name})
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
	if _, ok := reg.Get(opts.Name); ok {
		return config.Instance{}, fmt.Errorf("instance %q already exists", opts.Name)
	}
	inst := config.Instance{
		Name:      opts.Name,
		Alias:     opts.Alias,
		Tags:      opts.Tags,
		Note:      opts.Note,
		CreatedAt: time.Now(),
		Backend:   backend.DefaultName(),
	}
	b := m.backend()
	res, err := b.Create(inst)
	if err != nil {
		return config.Instance{}, err
	}
	inst.Backend = res.Backend
	inst.Username = res.Username
	inst.EncryptedPassword = res.EncryptedPassword
	if err := reg.Add(inst); err != nil {
		_ = b.Remove(inst, true)
		return config.Instance{}, err
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		_ = b.Remove(inst, true)
		return config.Instance{}, err
	}
	if err := desktop.Write(m.Layout, inst); err != nil {
		return config.Instance{}, err
	}
	return inst, nil
}

// DisplayUser 返回实例的隔离身份（Windows 用户名，或 "-"）。
func (m Manager) DisplayUser(inst config.Instance) string {
	return m.backend().DisplayUser(inst)
}

// SharedDir 返回实例可访问的共享目录。
func (m Manager) SharedDir(inst config.Instance) string {
	return m.backend().SharedDir(inst)
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

// HomeDir 返回实例数据目录；未注册时按名称推断。
func (m Manager) HomeDir(name string) string {
	if inst, err := m.Get(name); err == nil {
		return m.backend().HomeDir(inst)
	}
	return m.instanceHome(name)
}

// DataSize 估算实例数据占用；无法访问的文件会被跳过。
func (m Manager) DataSize(name string) (int64, error) {
	var total int64
	root := m.HomeDir(name)
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
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
	Alias     *string
	Tags      *[]string
	Note      *string
	WechatBin *string
	IMModule  *string
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
	removed, err := reg.Remove(name)
	if err != nil {
		return err
	}
	if err := config.SaveRegistry(m.Layout, reg); err != nil {
		return err
	}
	_ = desktop.Remove(m.Layout, name)

	if !opts.Purge {
		return nil
	}

	home := m.backend().HomeDir(removed)
	if _, err := os.Stat(home); err == nil && !opts.Yes {
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
	return m.backend().Remove(removed, true)
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
		if !strings.HasPrefix(name, paths.DesktopPrefix) || !strings.HasSuffix(name, paths.DesktopExt) {
			continue
		}
		instName := strings.TrimSuffix(strings.TrimPrefix(name, paths.DesktopPrefix), paths.DesktopExt)
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
		inst := config.Instance{Name: name, CreatedAt: time.Now(), Note: "migrated from wechat-profiles", Backend: backend.DefaultName()}
		if _, err := m.backend().Create(inst); err != nil {
			return migrated, err
		}
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
	Config    config.Config     `toml:"config"`
	Instances []config.Instance `toml:"instance"`
}

// Export writes config + instances metadata to path.
func (m Manager) Export(path string) error {
	reg, err := config.LoadRegistry(m.Layout)
	if err != nil {
		return err
	}
	exported := make([]config.Instance, len(reg.Instances))
	copy(exported, reg.Instances)
	for i := range exported {
		// DPAPI 密文绑定当前 Windows 用户，导出时丢弃以免误用。
		exported[i].EncryptedPassword = ""
	}
	bundle := ExportBundle{
		Config:    config.RelativizeForSave(m.Layout.Home, m.Config),
		Instances: exported,
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
	b := m.backend()
	for _, inst := range bundle.Instances {
		if err := config.ValidateName(inst.Name); err != nil {
			continue
		}
		inst = normalizeImported(inst)
		res, err := b.Create(inst)
		if err != nil {
			return err
		}
		if res.Backend != "" {
			inst.Backend = res.Backend
		}
		if res.Username != "" {
			inst.Username = res.Username
		}
		if res.EncryptedPassword != "" {
			inst.EncryptedPassword = res.EncryptedPassword
		}
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

// normalizeImported 将导入的实例字段对齐到当前平台后端。
func normalizeImported(inst config.Instance) config.Instance {
	def := backend.DefaultName()
	if inst.Backend == backend.BackendWindowsUser && def != backend.BackendWindowsUser {
		inst.Backend = def
		inst.Username = ""
		inst.EncryptedPassword = ""
	}
	if def == backend.BackendWindowsUser && inst.Backend != backend.BackendWindowsUser {
		inst.Backend = def
	}
	if inst.Backend == "" {
		inst.Backend = def
	}
	return inst
}
