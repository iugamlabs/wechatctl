package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/deali/wxctl/internal/paths"
	toml "github.com/pelletier/go-toml/v2"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Instance is a registered WeChat profile.
type Instance struct {
	Name      string    `toml:"name"`
	Alias     string    `toml:"alias,omitempty"`
	Tags      []string  `toml:"tags,omitempty"`
	Note      string    `toml:"note,omitempty"`
	CreatedAt time.Time `toml:"created_at"`
	WechatBin string    `toml:"wechat_bin,omitempty"`
	IMModule  string    `toml:"im_module,omitempty"`
}

// Registry is the instances.toml contents.
type Registry struct {
	Instances []Instance `toml:"instance"`
}

// ValidateName checks instance name rules.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("instance name is required")
	}
	if !namePattern.MatchString(name) {
		return errors.New("instance name may only contain letters, digits, underscore and hyphen")
	}
	return nil
}

// LoadRegistry reads instances.toml (empty if missing).
func LoadRegistry(layout paths.Layout) (Registry, error) {
	data, err := os.ReadFile(layout.InstancesFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Registry{}, nil
		}
		return Registry{}, err
	}
	var reg Registry
	if err := toml.Unmarshal(data, &reg); err != nil {
		return Registry{}, fmt.Errorf("parse instances: %w", err)
	}
	return reg, nil
}

// SaveRegistry writes instances.toml.
func SaveRegistry(layout paths.Layout, reg Registry) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(reg)
	if err != nil {
		return err
	}
	tmp := layout.InstancesFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, layout.InstancesFile)
}

// Get finds an instance by name.
func (r Registry) Get(name string) (Instance, bool) {
	for _, inst := range r.Instances {
		if inst.Name == name {
			return inst, true
		}
	}
	return Instance{}, false
}

// Add inserts a new instance or errors if duplicate.
func (r *Registry) Add(inst Instance) error {
	if _, ok := r.Get(inst.Name); ok {
		return fmt.Errorf("instance %q already exists", inst.Name)
	}
	r.Instances = append(r.Instances, inst)
	return nil
}

// Update replaces an existing instance.
func (r *Registry) Update(inst Instance) error {
	for i, existing := range r.Instances {
		if existing.Name == inst.Name {
			r.Instances[i] = inst
			return nil
		}
	}
	return fmt.Errorf("instance %q not found", inst.Name)
}

// Remove deletes an instance from the registry.
func (r *Registry) Remove(name string) (Instance, error) {
	for i, inst := range r.Instances {
		if inst.Name == name {
			r.Instances = slices.Delete(r.Instances, i, i+1)
			return inst, nil
		}
	}
	return Instance{}, fmt.Errorf("instance %q not found", name)
}

// DisplayName returns alias or a default label.
func (inst Instance) DisplayName() string {
	if inst.Alias != "" {
		return inst.Alias
	}
	return fmt.Sprintf("WeChat (%s)", inst.Name)
}

// EffectiveWechatBin returns instance override or global.
func (inst Instance) EffectiveWechatBin(cfg Config) string {
	if inst.WechatBin != "" {
		return inst.WechatBin
	}
	return cfg.WechatBin
}

// EffectiveIMModule returns instance override or global.
func (inst Instance) EffectiveIMModule(cfg Config) string {
	if inst.IMModule != "" {
		return inst.IMModule
	}
	return cfg.IMModule
}
