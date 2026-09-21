package wxdata

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

// Owner 是 sudo 下要把 wxdata 树 chown 到的 uid/gid。
type Owner struct {
	UID int
	GID int
}

var (
	chownFn  = os.Chown
	geteuid  = os.Geteuid
	lookupFn = user.Lookup
)

// ResolveOwner 在 euid==0 时解析 SUDO_UID/SUDO_GID（缺则 lookup SUDO_USER）。
// 非 root 返回 (nil, nil)，不 chown。解析失败返回 error。
func ResolveOwner() (*Owner, error) {
	if geteuid() != 0 {
		return nil, nil
	}
	if uidS, gidS := os.Getenv("SUDO_UID"), os.Getenv("SUDO_GID"); uidS != "" && gidS != "" {
		uid, err1 := strconv.Atoi(uidS)
		gid, err2 := strconv.Atoi(gidS)
		if err1 == nil && err2 == nil {
			return &Owner{UID: uid, GID: gid}, nil
		}
	}
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return nil, fmt.Errorf("cannot resolve target uid for chown (need SUDO_UID/SUDO_GID or SUDO_USER)")
	}
	u, err := lookupFn(sudoUser)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve target uid for chown: %w", err)
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("cannot parse passwd uid/gid for %s", sudoUser)
	}
	return &Owner{UID: uid, GID: gid}, nil
}

// ChownTree 将 root 下目录与文件全部 chown 到 uid/gid。
func ChownTree(root string, uid, gid int) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return chownFn(path, uid, gid)
	})
}

// ChownWxdata chown 实例目录树，以及 MkdirAll 可能以 root 创建的父目录 wxdata/。
// 只 chown 名为 wxdata 的直接父目录，不碰 ~/.local/share/wxctl 本身。
func ChownWxdata(stateDir string, uid, gid int) error {
	if err := chownWxdataParent(stateDir, uid, gid); err != nil {
		return err
	}
	return ChownTree(stateDir, uid, gid)
}

func chownWxdataParent(stateDir string, uid, gid int) error {
	parent := filepath.Dir(stateDir)
	if filepath.Base(parent) != "wxdata" {
		return nil
	}
	return chownFn(parent, uid, gid)
}

// PersistKeys 先写 all_keys.json.tmp，可选 chown 整树，成功才 rename。
// chown 失败不留下可 skip 的正式文件：rename 前失败则删 tmp；rename 后失败则删正式文件。
func PersistKeys(stateDir, dbDir string, entries map[string]keys.KeyInfo, owner *Owner) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	path := keys.KeysPath(stateDir)
	tmp := path + ".tmp"
	if err := keys.Write(tmp, dbDir, entries); err != nil {
		return err
	}
	if owner != nil {
		if err := ChownWxdata(stateDir, owner.UID, owner.GID); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("chown wxdata %s: %w", stateDir, err)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if owner != nil {
		if err := ChownWxdata(stateDir, owner.UID, owner.GID); err != nil {
			_ = os.Remove(path)
			return fmt.Errorf("chown wxdata %s: %w", stateDir, err)
		}
	}
	_ = os.Remove(keys.PartialPath(path))
	return nil
}

// WritePartial 写入 all_keys.json.partial（不覆盖正式文件）。
func WritePartial(stateDir, dbDir string, entries map[string]keys.KeyInfo) {
	_ = keys.Write(keys.PartialPath(keys.KeysPath(stateDir)), dbDir, entries)
}

// DemoteOfficialToPartial 在 skip 路径 chown 失败时拿掉正式文件，避免下次误 skip。
func DemoteOfficialToPartial(stateDir string) {
	path := keys.KeysPath(stateDir)
	partial := keys.PartialPath(path)
	_ = os.Remove(partial)
	_ = os.Rename(path, partial)
}

// EnsureOwned 在 euid==0 时始终 chown 整树（含父目录 wxdata/）。
// 即使 all_keys.json 已是 SUDO_UID，MkdirAll 创建的 wxdata/ 仍可能是 root:0700。
// 失败时 demote 正式文件，避免下次误 skip。
func EnsureOwned(stateDir string, owner *Owner) error {
	if owner == nil {
		return nil
	}
	if err := ChownWxdata(stateDir, owner.UID, owner.GID); err != nil {
		DemoteOfficialToPartial(stateDir)
		return err
	}
	return nil
}

// ValidateExistingKeys 校验已有 all_keys.json：_db_dir、必需 key、page1 salt。
func ValidateExistingKeys(keysPath, dbDir string) error {
	storedDir, entries, err := keys.Load(keysPath)
	if err != nil {
		return err
	}
	if storedDir == "" {
		return fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force")
	}
	storedDir, err = filepath.EvalSymlinks(storedDir)
	if err != nil {
		return fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force")
	}
	dbDir, err = filepath.EvalSymlinks(dbDir)
	if err != nil || storedDir != dbDir {
		return fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force")
	}
	files, _ := keys.CollectDBFiles(dbDir)
	if miss := keys.MissingRequired(files, entries); len(miss) > 0 {
		return fmt.Errorf("missing required keys: %v", miss)
	}
	return saltsMatchRequired(files, entries)
}

func saltsMatchRequired(files []keys.DBFile, entries map[string]keys.KeyInfo) error {
	byRel := make(map[string]keys.DBFile, len(files))
	for _, f := range files {
		byRel[f.Rel] = f
	}
	check := append([]string{}, keys.RequiredRels...)
	for _, f := range files {
		if keys.MessageDBRelRE.MatchString(f.Rel) {
			check = append(check, f.Rel)
		}
	}
	for _, rel := range check {
		info, ok := keys.GetKeyInfo(entries, rel)
		if !ok || info.EncKey == "" {
			if keys.MessageDBRelRE.MatchString(rel) {
				continue
			}
			return fmt.Errorf("missing key for %s", rel)
		}
		f, ok := byRel[rel]
		if !ok {
			return fmt.Errorf("missing db file %s", rel)
		}
		pageSalt := hex.EncodeToString(f.Page1[:16])
		if info.Salt != "" && info.Salt != pageSalt {
			return fmt.Errorf("salt mismatch for %s", rel)
		}
	}
	return nil
}
