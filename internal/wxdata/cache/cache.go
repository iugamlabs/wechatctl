package cache

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
	"github.com/star-plan/wechatctl/internal/wxdata/errkind"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"

	_ "modernc.org/sqlite"
)

type mtimeEntry struct {
	DBMT    int64  `json:"db_mt"`
	WalMT   int64  `json:"wal_mt"`
	DBSize  int64  `json:"db_size"`
	WalSize int64  `json:"wal_size"`
	Path    string `json:"path"`
}

// Cache 按 db+wal mtime/size 缓存明文 sqlite 副本。
type Cache struct {
	keys     map[string]keys.KeyInfo
	dbDir    string
	cacheDir string
	lockPath string

	mtimes map[string]mtimeEntry
}

// New 创建缓存目录（0700）并加载 _mtimes.json。
func New(keyMap map[string]keys.KeyInfo, dbDir, cacheDir string) (*Cache, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	c := &Cache{
		keys:     keyMap,
		dbDir:    dbDir,
		cacheDir: cacheDir,
		lockPath: filepath.Join(filepath.Dir(cacheDir), "lock"),
		mtimes:   make(map[string]mtimeEntry),
	}
	c.loadMtimes()
	return c, nil
}

func (c *Cache) loadMtimes() {
	path := filepath.Join(c.cacheDir, "_mtimes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]mtimeEntry
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	for k, v := range saved {
		c.mtimes[normalizeRelKey(k)] = v
	}
}

func (c *Cache) saveMtimes() error {
	path := filepath.Join(c.cacheDir, "_mtimes.json")
	data, err := json.MarshalIndent(c.mtimes, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeRelKey(rel string) string {
	return filepath.ToSlash(rel)
}

func cacheFileName(relKey string) string {
	h := md5.Sum([]byte(normalizeRelKey(relKey)))
	return hex.EncodeToString(h[:])[:12] + ".db"
}

func snapPrefix(relKey string) string {
	h := md5.Sum([]byte(normalizeRelKey(relKey)))
	return ".snap-" + hex.EncodeToString(h[:])[:12]
}

// Get 返回明文 sqlite 路径；按需解密并更新 _mtimes.json。
func (c *Cache) Get(relKey string) (string, error) {
	info, ok := keys.GetKeyInfo(c.keys, relKey)
	if !ok {
		return "", fmt.Errorf("no key for %s: %w", relKey, errkind.ErrDecrypt)
	}
	if info.EncKey == "" {
		return "", fmt.Errorf("missing enc_key for %s: %w", relKey, errkind.ErrDecrypt)
	}
	relKey = normalizeRelKey(relKey)
	dbPath := filepath.Join(c.dbDir, filepath.FromSlash(relKey))

	dbStat, walStat, err := statDBWal(dbPath)
	if err != nil {
		return "", err
	}

	if path, ok := c.cacheHit(relKey, dbStat, walStat); ok {
		return path, nil
	}

	var plainPath string
	if err := c.withLock(func() error {
		c.removeAllSnaps()
		dbStat, walStat, err = statDBWal(dbPath)
		if err != nil {
			return err
		}
		if path, ok := c.cacheHit(relKey, dbStat, walStat); ok {
			plainPath = path
			return nil
		}
		path, err := c.decryptOne(relKey, info, dbPath, dbStat, walStat)
		if err != nil {
			return err
		}
		plainPath = path
		return nil
	}); err != nil {
		return "", err
	}
	return plainPath, nil
}

func (c *Cache) cacheHit(relKey string, dbStat, walStat os.FileInfo) (string, bool) {
	ent, ok := c.mtimes[relKey]
	if !ok {
		return "", false
	}
	if ent.DBMT != dbStat.ModTime().UnixNano() ||
		ent.DBSize != dbStat.Size() ||
		ent.WalMT != walMtime(walStat) ||
		ent.WalSize != walSize(walStat) {
		return "", false
	}
	if ent.Path == "" || !fileExists(ent.Path) {
		return "", false
	}
	return ent.Path, true
}

func walMtime(st os.FileInfo) int64 {
	if st == nil {
		return 0
	}
	return st.ModTime().UnixNano()
}

func walSize(st os.FileInfo) int64 {
	if st == nil {
		return 0
	}
	return st.Size()
}

func statDBWal(dbPath string) (os.FileInfo, os.FileInfo, error) {
	dbStat, err := os.Stat(dbPath)
	if err != nil {
		return nil, nil, err
	}
	walPath := dbPath + "-wal"
	walStat, err := os.Stat(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return dbStat, nil, nil
		}
		return nil, nil, err
	}
	return dbStat, walStat, nil
}

func (c *Cache) decryptOne(relKey string, info keys.KeyInfo, dbPath string, dbStat, walStat os.FileInfo) (string, error) {
	return c.decryptOneAttempt(relKey, info, dbPath, dbStat, walStat, true)
}

func (c *Cache) decryptOneAttempt(relKey string, info keys.KeyInfo, dbPath string, dbStat, walStat os.FileInfo, allowRetry bool) (string, error) {
	page1 := make([]byte, crypto.SaltSize)
	f, err := os.Open(dbPath)
	if err != nil {
		return "", err
	}
	if _, err := io.ReadFull(f, page1); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	wantSalt := strings.ToLower(info.Salt)
	gotSalt := hex.EncodeToString(page1)
	if wantSalt != "" && gotSalt != wantSalt {
		return "", fmt.Errorf("salt mismatch for %s (run: wxctl init-data --force): %w", relKey, errkind.ErrNoKeys)
	}

	encKey, err := hex.DecodeString(info.EncKey)
	if err != nil {
		return "", fmt.Errorf("bad enc_key: %w", errkind.ErrDecrypt)
	}

	prefix := snapPrefix(relKey)
	snapDB := filepath.Join(c.cacheDir, prefix+".db")
	snapWal := filepath.Join(c.cacheDir, prefix+".wal")
	defer c.removeSnapPrefix(prefix)

	if err := copyFile(dbPath, snapDB); err != nil {
		return "", err
	}
	walPath := dbPath + "-wal"
	if walStat != nil {
		if err := copyFile(walPath, snapWal); err != nil {
			return "", err
		}
	}

	outName := cacheFileName(relKey)
	outPath := filepath.Join(c.cacheDir, outName)
	tmpPath := outPath + ".tmp"

	if _, err := crypto.FullDecrypt(snapDB, tmpPath, encKey); err != nil {
		return "", fmt.Errorf("full decrypt: %w", errkind.ErrDecrypt)
	}
	if walStat != nil {
		if _, err := crypto.DecryptWAL(snapWal, tmpPath, encKey); err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("wal decrypt: %w", errkind.ErrDecrypt)
		}
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return "", err
	}

	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return "", err
	}

	if err := verifyPlainSQLite(absOut); err != nil {
		os.Remove(outPath)
		delete(c.mtimes, relKey)
		_ = c.saveMtimes()
		if allowRetry {
			return c.decryptOneAttempt(relKey, info, dbPath, dbStat, walStat, false)
		}
		return "", fmt.Errorf("open plain sqlite: %w", errkind.ErrDecrypt)
	}

	c.mtimes[relKey] = mtimeEntry{
		DBMT:    dbStat.ModTime().UnixNano(),
		WalMT:   walMtime(walStat),
		DBSize:  dbStat.Size(),
		WalSize: walSize(walStat),
		Path:    absOut,
	}
	if err := c.saveMtimes(); err != nil {
		return "", err
	}
	return absOut, nil
}

func verifyPlainSQLite(plainPath string) error {
	db, err := OpenPlain(plainPath)
	if err != nil {
		return err
	}
	defer db.Close()
	var qo int
	if err := db.QueryRow("PRAGMA query_only").Scan(&qo); err != nil {
		return err
	}
	if qo != 1 {
		return fmt.Errorf("query_only not on")
	}
	return nil
}

// OpenPlain 用 file: URI 只读打开明文库（modernc.org/sqlite）。
func OpenPlain(plainPath string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(plainPath)}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	dsn := u.String()
	if strings.Contains(dsn, "_pragma=") {
		return nil, fmt.Errorf("invalid dsn contains _pragma")
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=3000"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func (c *Cache) removeAllSnaps() {
	c.removeSnapPrefix("")
}

func (c *Cache) removeSnapPrefix(prefix string) {
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		name := ent.Name()
		if !strings.HasPrefix(name, ".snap-") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		_ = os.Remove(filepath.Join(c.cacheDir, name))
	}
}

func (c *Cache) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(c.lockPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(c.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
