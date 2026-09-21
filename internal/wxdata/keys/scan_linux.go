package keys

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/star-plan/wechatctl/internal/paths"
)

const (
	// MaxRegionSize 对齐 wechat-cli：超过则跳过该 maps 区域。
	MaxRegionSize = 500 * 1024 * 1024
	capSysPtrace  = 1 << 19
)

var (
	procRoot = "/proc"

	knownComms = map[string]bool{
		"wechat":      true,
		"wechatappex": true,
		"weixin":      true,
	}
	interpreterPrefixes = []string{"python", "bash", "sh", "zsh", "node", "perl", "ruby"}
	skipMappings        = map[string]bool{
		"[vdso]":     true,
		"[vsyscall]": true,
		"[vvar]":     true,
	}
	skipPathPrefixes = []string{"/usr/lib/", "/lib/", "/usr/share/"}

	geteuidFn = os.Geteuid
)

// Region 是 /proc/<pid>/maps 中一块可读内存。
type Region struct {
	Start uint64
	Size  uint64
}

// EnvironHasInstance 在 NUL 分隔的 environ 中精确匹配 WXCTL_INSTANCE=<name>（fail-closed）。
func EnvironHasInstance(environ []byte, name string) bool {
	needle := []byte(paths.InstanceEnvKey + "=" + name)
	for _, tok := range bytes.Split(environ, []byte{0}) {
		if bytes.Equal(tok, needle) {
			return true
		}
	}
	return false
}

// ParseMaps 解析 /proc/<pid>/maps，过滤不可读、vdso 与无关系统库。
func ParseMaps(r io.Reader) []Region {
	var regions []Region
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) < 2 {
			continue
		}
		if !strings.Contains(parts[1], "r") {
			continue
		}
		if len(parts) >= 6 {
			name := parts[5]
			if skipMappings[name] {
				continue
			}
			lower := strings.ToLower(name)
			if hasSkipPathPrefix(name) &&
				!strings.Contains(lower, "wcdb") &&
				!strings.Contains(lower, "wechat") &&
				!strings.Contains(lower, "weixin") {
				continue
			}
		}
		addrs := strings.Split(parts[0], "-")
		if len(addrs) != 2 {
			continue
		}
		start, err1 := strconv.ParseUint(addrs[0], 16, 64)
		end, err2 := strconv.ParseUint(addrs[1], 16, 64)
		if err1 != nil || err2 != nil || end <= start {
			continue
		}
		size := end - start
		if size > 0 && size < MaxRegionSize {
			regions = append(regions, Region{Start: start, Size: size})
		}
	}
	return regions
}

func hasSkipPathPrefix(name string) bool {
	for _, p := range skipPathPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// CapEffHasPtrace 解析 /proc/self/status 的 CapEff 是否含 CAP_SYS_PTRACE。
func CapEffHasPtrace(status []byte) bool {
	for _, line := range bytes.Split(status, []byte{'\n'}) {
		if !bytes.HasPrefix(line, []byte("CapEff:")) {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			return false
		}
		v, err := strconv.ParseUint(string(fields[1]), 16, 64)
		if err != nil {
			return false
		}
		return v&capSysPtrace != 0
	}
	return false
}

// HasPtrace 在 euid==0 或 CapEff 含 CAP_SYS_PTRACE 时通过。
func HasPtrace() bool {
	if geteuidFn() == 0 {
		return true
	}
	data, err := os.ReadFile(filepath.Join(procRoot, "self", "status"))
	if err != nil {
		return false
	}
	return CapEffHasPtrace(data)
}

// IsWeChatProcess 抄 scanner_linux._is_wechat_process。
func IsWeChatProcess(pid int) bool {
	if pid == os.Getpid() || pid <= 0 {
		return false
	}
	comm, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return false
	}
	if knownComms[strings.ToLower(strings.TrimSpace(string(comm)))] {
		return true
	}
	exe := safeReadlink(filepath.Join(procRoot, strconv.Itoa(pid), "exe"))
	base := strings.ToLower(filepath.Base(exe))
	for _, p := range interpreterPrefixes {
		if strings.HasPrefix(base, p) {
			return false
		}
	}
	return strings.Contains(base, "wechat") || strings.Contains(base, "weixin")
}

func safeReadlink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if abs, err := filepath.EvalSymlinks(path); err == nil {
		return abs
	}
	return target
}

func readRSSPages(pid int) int {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return n
}

func readEnviron(pid int) ([]byte, error) {
	return os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "environ"))
}

// ListInstanceWeChatPIDs 扫 /proc 中 comm/exe 像微信、且 environ 精确匹配本实例的进程。
// environ 读失败则跳过（fail-closed）。按 RSS pages 降序。
func ListInstanceWeChatPIDs(name string) []int {
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	type rec struct {
		pid int
		rss int
	}
	var found []rec
	for _, ent := range ents {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if !IsWeChatProcess(pid) {
			continue
		}
		env, err := readEnviron(pid)
		if err != nil {
			continue
		}
		if !EnvironHasInstance(env, name) {
			continue
		}
		found = append(found, rec{pid: pid, rss: readRSSPages(pid)})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].rss != found[j].rss {
			return found[i].rss > found[j].rss
		}
		return found[i].pid < found[j].pid
	})
	out := make([]int, len(found))
	for i, r := range found {
		out[i] = r.pid
	}
	return out
}

// SortPIDsByRSS 按 /proc/<pid>/statm RSS pages 降序（原地）。
func SortPIDsByRSS(pids []int) {
	type rec struct {
		pid int
		rss int
	}
	tmp := make([]rec, len(pids))
	for i, pid := range pids {
		tmp[i] = rec{pid: pid, rss: readRSSPages(pid)}
	}
	sort.SliceStable(tmp, func(i, j int) bool {
		return tmp[i].rss > tmp[j].rss
	})
	for i, r := range tmp {
		pids[i] = r.pid
	}
}

func remainingSet(saltToDBs map[string][]string) map[string]struct{} {
	out := make(map[string]struct{}, len(saltToDBs))
	for s := range saltToDBs {
		out[s] = struct{}{}
	}
	return out
}

func logf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}

// ScanMemoryForKeys 扫描一段内存中的 x'<hex>' 模式并用 page1 HMAC 验证。
func ScanMemoryForKeys(data []byte, base uint64, pid int, files []DBFile, saltToDBs map[string][]string, keyMap map[string]string, remaining map[string]struct{}, log io.Writer) int {
	locs := HexMemoryRE.FindAllSubmatchIndex(data, -1)
	n := 0
	for _, loc := range locs {
		if len(loc) < 4 {
			continue
		}
		n++
		hexStr := string(data[loc[2]:loc[3]])
		addr := base + uint64(loc[0])
		encHex, saltHex, ok := ClassifyHex(hexStr)
		if !ok {
			continue
		}
		encKey, err := hex.DecodeString(encHex)
		if err != nil || len(encKey) != 32 {
			continue
		}
		switch {
		case len(hexStr) == 64:
			if len(remaining) == 0 {
				continue
			}
			for _, f := range files {
				if _, want := remaining[f.Salt]; !want {
					continue
				}
				if VerifyEncKey(encKey, f.Page1) {
					keyMap[f.Salt] = encHex
					delete(remaining, f.Salt)
					logf(log, "\n  [FOUND] salt=%s", f.Salt)
					logf(log, "    enc_key=%s", encHex)
					logf(log, "    PID=%d addr=0x%016X", pid, addr)
					logf(log, "    dbs: %s", strings.Join(saltToDBs[f.Salt], ", "))
					break
				}
			}
		default:
			if saltHex == "" {
				continue
			}
			if _, want := remaining[saltHex]; !want {
				continue
			}
			for _, f := range files {
				if f.Salt != saltHex {
					continue
				}
				if VerifyEncKey(encKey, f.Page1) {
					keyMap[saltHex] = encHex
					delete(remaining, saltHex)
					extra := ""
					if len(hexStr) > 96 {
						extra = fmt.Sprintf(" (long hex %d)", len(hexStr))
					}
					logf(log, "\n  [FOUND] salt=%s%s", saltHex, extra)
					logf(log, "    enc_key=%s", encHex)
					logf(log, "    PID=%d addr=0x%016X", pid, addr)
					logf(log, "    dbs: %s", strings.Join(saltToDBs[saltHex], ", "))
					break
				}
			}
		}
	}
	return n
}

// CrossVerifyKeys 用已找到的 enc_key 再试未匹配 salt。
func CrossVerifyKeys(files []DBFile, saltToDBs map[string][]string, keyMap map[string]string, log io.Writer) {
	missing := remainingSet(saltToDBs)
	for s := range keyMap {
		delete(missing, s)
	}
	if len(missing) == 0 || len(keyMap) == 0 {
		return
	}
	logf(log, "\n%d salt(s) unmatched, trying cross-verify...", len(missing))
	for saltHex := range missing {
		var page1 []byte
		for _, f := range files {
			if f.Salt == saltHex {
				page1 = f.Page1
				break
			}
		}
		if page1 == nil {
			continue
		}
		for knownSalt, knownKey := range keyMap {
			encKey, err := hex.DecodeString(knownKey)
			if err != nil {
				continue
			}
			if VerifyEncKey(encKey, page1) {
				keyMap[saltHex] = knownKey
				logf(log, "  [CROSS] salt=%s uses key from salt=%s", saltHex, knownSalt)
				break
			}
		}
	}
}

func parsePIDMaps(pid int) ([]Region, error) {
	f, err := os.Open(filepath.Join(procRoot, strconv.Itoa(pid), "maps"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseMaps(f), nil
}

// ExtractFromPIDs 按 RSS 顺序扫描各 PID 内存，返回 salt→enc_key。
func ExtractFromPIDs(pids []int, files []DBFile, saltToDBs map[string][]string, log io.Writer) map[string]string {
	keyMap := make(map[string]string)
	remaining := remainingSet(saltToDBs)
	allMatches := 0
	t0 := time.Now()

	for _, pid := range pids {
		if len(remaining) == 0 {
			logf(log, "\n[+] all keys found, skipping remaining processes")
			break
		}
		regions, err := parsePIDMaps(pid)
		if err != nil {
			logf(log, "[WARN] cannot read /proc/%d/maps, skip", pid)
			continue
		}
		var total uint64
		for _, r := range regions {
			total += r.Size
		}
		logf(log, "\n[*] scanning PID=%d (%.0fMB, %d regions)", pid, float64(total)/1024/1024, len(regions))

		mem, err := os.Open(filepath.Join(procRoot, strconv.Itoa(pid), "mem"))
		if err != nil {
			logf(log, "[WARN] cannot open /proc/%d/mem, skip", pid)
			continue
		}
		if !IsWeChatProcess(pid) {
			logf(log, "[WARN] PID %d is no longer a WeChat process, skip", pid)
			mem.Close()
			continue
		}

		var scanned uint64
		for i, reg := range regions {
			if reg.Start > uint64(^uint64(0)>>1) {
				continue
			}
			if _, err := mem.Seek(int64(reg.Start), io.SeekStart); err != nil {
				continue
			}
			buf := make([]byte, reg.Size)
			n, err := mem.Read(buf)
			if err != nil && n <= 0 {
				continue
			}
			if n <= 0 {
				continue
			}
			scanned += uint64(n)
			allMatches += ScanMemoryForKeys(buf[:n], reg.Start, pid, files, saltToDBs, keyMap, remaining, log)
			if (i+1)%200 == 0 {
				elapsed := time.Since(t0).Seconds()
				progress := 100.0
				if total > 0 {
					progress = float64(scanned) / float64(total) * 100
				}
				logf(log, "  [%.1f%%] %d/%d salts matched, %d hex patterns, %.1fs",
					progress, len(keyMap), len(saltToDBs), allMatches, elapsed)
			}
		}
		mem.Close()
	}

	logf(log, "\nscan done: %.1fs, %d processes, %d hex patterns", time.Since(t0).Seconds(), len(pids), allMatches)
	CrossVerifyKeys(files, saltToDBs, keyMap, log)
	return keyMap
}
