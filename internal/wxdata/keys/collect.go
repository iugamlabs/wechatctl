package keys

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
)

// DBFile 是 collect_db_files 的一条记录。
type DBFile struct {
	Rel   string
	Abs   string
	Size  int64
	Salt  string
	Page1 []byte
}

// CollectDBFiles 遍历 db_dir 收集 .db 文件及 page1 salt。
func CollectDBFiles(dbDir string) (files []DBFile, saltToDBs map[string][]string) {
	saltToDBs = make(map[string][]string)
	_ = filepath.Walk(dbDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !stringsHasSuffixDB(name) {
			return nil
		}
		if info.Size() < crypto.PageSize {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		page1 := make([]byte, crypto.PageSize)
		if _, err := f.Read(page1); err != nil {
			f.Close()
			return nil
		}
		f.Close()

		rel, err := filepath.Rel(dbDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		saltHex := hex.EncodeToString(page1[:crypto.SaltSize])
		files = append(files, DBFile{
			Rel:   rel,
			Abs:   path,
			Size:  info.Size(),
			Salt:  saltHex,
			Page1: page1,
		})
		saltToDBs[saltHex] = append(saltToDBs[saltHex], rel)
		return nil
	})
	return files, saltToDBs
}

func stringsHasSuffixDB(name string) bool {
	if !strings.HasSuffix(name, ".db") {
		return false
	}
	if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
		return false
	}
	return true
}
