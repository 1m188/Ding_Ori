// Command probe 是一个 CE 式的实时内存诊断/扫描工具，用于对照
// 活体游戏进程定位字段地址。仅用于本机授权调试。
//
// 用法:
//
//	probe info                  进程/位数/模块基址/可读内存统计
//	probe regions               列出归一化后的可读内存区域
//	probe read  0xADDR 64       十六进制转储
//	probe write 0xADDR i32 999  写值
//	probe scan  i32 16          精确值扫描（保存候选到 .probe_state.json）
//	probe next  i32 8           在上次候选里按新值过滤
//	probe list                  列出候选及当前值
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"oritrainer/core"
	"oritrainer/ori"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	modpsapi    = syscall.NewLazyDLL("psapi.dll")

	procIsWow64              = modkernel32.NewProc("IsWow64Process")
	procVirtualQueryEx       = modkernel32.NewProc("VirtualQueryEx")
	procEnumProcessModulesEx = modpsapi.NewProc("EnumProcessModulesEx")
	procGetModuleFileNameExW = modpsapi.NewProc("GetModuleFileNameExW")
)

const (
	memCommit = 0x1000
	listAll   = 0x03
	stateFile = ".probe_state.json"
)

type mbi struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

type reg struct {
	Base    uint64
	Size    uint64
	Protect uint32
	Type    uint32
}

type state struct {
	Type  string   `json:"type"`
	Addrs []uint32 `json:"addrs"`
}

// probeProfile 按命令行是否含 -vanilla 选择目标版本。
//
//	probe <cmd> ...           默认终极版 oriDE.exe
//	probe <cmd> ... -vanilla  原版 ori.exe（字段偏移需用 offs 重新核验）
func probeProfile() ori.Profile {
	for _, a := range os.Args {
		if a == "-vanilla" {
			return ori.Profiles[ori.Vanilla]
		}
	}
	return ori.Profiles[ori.Definitive]
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	prof := probeProfile()
	p, err := core.Attach(prof.ProcessName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "attach:", err)
		os.Exit(1)
	}
	defer p.Close()

	switch cmd {
	case "info":
		cmdInfo(p)
	case "regions":
		cmdRegions(p)
	case "read":
		cmdRead(p)
	case "write":
		cmdWrite(p)
	case "scan":
		cmdScan(p)
	case "next":
		cmdNext(p)
	case "list":
		cmdList(p)
	case "diag":
		cmdDiag(p)
	case "findptr":
		cmdFindPtr(p)
	case "struct":
		cmdStruct(p)
	case "region":
		cmdRegion(p)
	case "mono":
		cmdMono(p)
	case "findobj":
		cmdFindObj(p)
	case "diffscan":
		cmdDiffScan(p)
	case "klass":
		cmdKlass(p)
	case "fields":
		cmdFields(p)
	case "findclass":
		cmdFindClass(p)
	case "staticref":
		cmdStaticRef(p)
	case "findfield":
		cmdFindField(p)
	case "instances":
		cmdInstances(p)
	case "staticmap":
		cmdStaticMap(p)
	case "singleton":
		cmdSingleton(p)
	case "objof":
		cmdObjOf(p)
	case "offs":
		cmdOffs(p)
	case "refs":
		cmdRefs(p)
	case "findall":
		cmdFindAll(p)
	case "rawregions":
		cmdRawRegions(p)
	case "diffscan2":
		cmdDiffScan2(p)
	case "diffobj":
		cmdDiffObj(p)
	case "feat":
		cmdFeat(p)
	case "onescan":
		cmdOneScan(p)
	case "allrefs":
		cmdAllRefs(p)
	case "diffstrict":
		cmdDiffStrict(p)
	case "staticblock":
		cmdStaticBlock(p)
	case "corereg":
		cmdCoreReg(p)
	case "racetest":
		cmdRaceTest(p)
	case "conin":
		cmdConIn()
	case "diffname":
		cmdDiffName(p)
	case "heapfind":
		cmdHeapFind(p)
	case "feats":
		cmdFeats()
	case "bases":
		cmdBases(p)
	case "strref":
		cmdStrRef(p)
	case "fields2":
		cmdFields2(p)
	case "class":
		cmdClass(p)
	case "live":
		cmdLive(p)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `probe <command> [args]
  info
  regions
  read  <hexaddr> <len>
  write <hexaddr> <i8|i16|i32|u32|i64|f32|f64> <value>
  scan  <type> <value>
  next  <type> <value>
  list`)
}

// ---------- 内存区域 ----------

func enumRegions(h uintptr) []reg {
	var out []reg
	var addr uint64
	var m mbi
	sz := uintptr(unsafe.Sizeof(m))
	for {
		r1, _, _ := procVirtualQueryEx.Call(h, uintptr(addr), uintptr(unsafe.Pointer(&m)), sz)
		if r1 == 0 {
			break
		}
		base := uint64(m.BaseAddress)
		size := uint64(m.RegionSize)
		if size == 0 {
			break
		}
		if m.State == memCommit {
			out = append(out, reg{Base: base, Size: size, Protect: m.Protect, Type: m.Type})
		}
		next := base + size
		if next <= addr {
			break
		}
		addr = next
		if addr >= 0x7FFFFFFFFFFF {
			break
		}
	}
	return out
}

// readable 判断保护属性是否允许 RPM 读取。
func readable(protect uint32) bool {
	if protect == 0 || protect == 0x01 || protect == 0x10 { // NOACCESS / EXECUTE-only
		return false
	}
	if protect&0x100 != 0 { // PAGE_GUARD
		return false
	}
	return true
}

// normRegions 归一化到 32 位地址空间（WoW64 影子区取低 32 位），并去重。
func normRegions(h uintptr) []reg {
	seen := map[uint32]bool{}
	var out []reg
	for _, r := range enumRegions(h) {
		if !readable(r.Protect) {
			continue
		}
		b := r.Base & 0xFFFFFFFF
		if b == 0 || b+r.Size > 0x100000000 {
			continue
		}
		if seen[uint32(b)] {
			continue
		}
		seen[uint32(b)] = true
		out = append(out, reg{Base: b, Size: r.Size, Protect: r.Protect, Type: r.Type})
	}
	return out
}

// ---------- 命令 ----------

func cmdInfo(p *core.Process) {
	var wow int32
	procIsWow64.Call(p.Handle, uintptr(unsafe.Pointer(&wow)))
	fmt.Printf("PID=%d name=%s targetIsWow64=%v\n", p.Pid, p.Name, wow != 0)

	ms := modules(p.Handle)
	for i, m := range ms {
		short := m.Name
		if j := strings.LastIndexAny(short, "\\/"); j >= 0 {
			short = short[j+1:]
		}
		if i == 0 || strings.Contains(strings.ToLower(short), "mono") {
			fmt.Printf("  module[%d] %-24s base=0x%X\n", i, short, m.Base)
		}
	}

	regs := normRegions(p.Handle)
	var total uint64
	for _, r := range regs {
		total += r.Size
	}
	fmt.Printf("readable regions=%d total=%.1f MB\n", len(regs), float64(total)/1048576)
}

type modInfo struct {
	Name string
	Base uint64
}

func modules(h uintptr) []modInfo {
	const maxMods = 1024
	var hmods [maxMods]uintptr
	var needed uint32
	r1, _, _ := procEnumProcessModulesEx.Call(h, uintptr(unsafe.Pointer(&hmods[0])),
		uintptr(maxMods*int(unsafe.Sizeof(uintptr(0)))), uintptr(unsafe.Pointer(&needed)), listAll)
	if r1 == 0 {
		return nil
	}
	n := int(needed) / int(unsafe.Sizeof(uintptr(0)))
	if n > maxMods {
		n = maxMods
	}
	var out []modInfo
	for i := 0; i < n; i++ {
		var buf [512]uint16
		procGetModuleFileNameExW.Call(h, hmods[i], uintptr(unsafe.Pointer(&buf[0])), 512)
		out = append(out, modInfo{Name: syscall.UTF16ToString(buf[:]), Base: uint64(hmods[i])})
	}
	return out
}

func cmdRegions(p *core.Process) {
	regs := normRegions(p.Handle)
	var total uint64
	for _, r := range regs {
		total += r.Size
	}
	fmt.Printf("%d regions, %.1f MB total\n", len(regs), float64(total)/1048576)
	for _, r := range regs {
		fmt.Printf("  0x%08X  size=0x%08X (%6.1f MB)  prot=0x%02X type=0x%X\n",
			r.Base, r.Size, float64(r.Size)/1048576, r.Protect, r.Type)
	}
}

func cmdRead(p *core.Process) {
	if len(os.Args) < 4 {
		usage()
		return
	}
	addr := parseHex(os.Args[2])
	n, _ := strconv.Atoi(os.Args[3])
	buf := make([]byte, n)
	if !p.ReadBytes(uint32(addr), buf) {
		fmt.Println("read failed")
		return
	}
	hexdump(uint32(addr), buf)
}

func hexdump(base uint32, buf []byte) {
	for i := 0; i < len(buf); i += 16 {
		end := i + 16
		if end > len(buf) {
			end = len(buf)
		}
		fmt.Printf("%08X  ", base+uint32(i))
		for j := i; j < end; j++ {
			fmt.Printf("%02X ", buf[j])
		}
		for j := end; j < i+16; j++ {
			fmt.Print("   ")
		}
		fmt.Print(" ")
		for j := i; j < end; j++ {
			c := buf[j]
			if c < 32 || c > 126 {
				c = '.'
			}
			fmt.Printf("%c", c)
		}
		fmt.Println()
	}
}

func cmdWrite(p *core.Process) {
	if len(os.Args) < 5 {
		usage()
		return
	}
	addr := parseHex(os.Args[2])
	needle, err := parseNeedle(os.Args[3], os.Args[4])
	if err != nil {
		fmt.Println("value:", err)
		return
	}
	if p.WriteBytes(uint32(addr), needle) {
		fmt.Printf("wrote % X to 0x%08X\n", needle, addr)
	} else {
		fmt.Println("write failed")
	}
}

func cmdScan(p *core.Process) {
	if len(os.Args) < 4 {
		usage()
		return
	}
	typ, val := os.Args[2], os.Args[3]
	needle, err := parseNeedle(typ, val)
	if err != nil {
		fmt.Println("value:", err)
		return
	}
	step := scanStep(len(needle))
	regs := normRegions(p.Handle)
	var all []uint32
	for _, r := range regs {
		all = append(all, scanRegion(p, r, needle, step)...)
	}
	saveState(state{Type: typ, Addrs: all})
	fmt.Printf("scan %s=%s -> %d candidates\n", typ, val, len(all))
	printAddrs(p, typ, all, 40)
}

func cmdNext(p *core.Process) {
	if len(os.Args) < 4 {
		usage()
		return
	}
	typ, val := os.Args[2], os.Args[3]
	needle, err := parseNeedle(typ, val)
	if err != nil {
		fmt.Println("value:", err)
		return
	}
	st := loadState()
	if st.Type != "" && st.Type != typ {
		fmt.Printf("warning: state type=%s != %s\n", st.Type, typ)
	}
	var kept []uint32
	buf := make([]byte, len(needle))
	for _, a := range st.Addrs {
		if p.ReadBytes(a, buf) && bytes.Equal(buf, needle) {
			kept = append(kept, a)
		}
	}
	saveState(state{Type: typ, Addrs: kept})
	fmt.Printf("next %s=%s -> %d candidates (from %d)\n", typ, val, len(kept), len(st.Addrs))
	printAddrs(p, typ, kept, 40)
}

func cmdList(p *core.Process) {
	st := loadState()
	fmt.Printf("state type=%s count=%d\n", st.Type, len(st.Addrs))
	printAddrs(p, st.Type, st.Addrs, 200)
}

func printAddrs(p *core.Process, typ string, addrs []uint32, limit int) {
	n := 0
	for _, a := range addrs {
		if n >= limit {
			fmt.Printf("  ... (%d more)\n", len(addrs)-n)
			break
		}
		v := readValue(p, typ, a)
		fmt.Printf("  0x%08X = %s\n", a, v)
		n++
	}
}

// ---------- 扫描 ----------

func scanStep(size int) int {
	switch size {
	case 1, 2:
		return 1
	default:
		return 4
	}
}

func scanRegion(p *core.Process, r reg, needle []byte, step int) []uint32 {
	var found []uint32
	const chunk = 8 << 20
	base := r.Base
	end := r.Base + r.Size
	for base < end {
		sz := uint64(chunk)
		if end-base < sz {
			sz = end - base
		}
		buf := make([]byte, sz)
		if !p.ReadBytes(uint32(base), buf) {
			base += sz
			continue
		}
		for off := 0; off+len(needle) <= int(sz); off += step {
			if bytes.Equal(buf[off:off+len(needle)], needle) {
				found = append(found, uint32(base)+uint32(off))
			}
		}
		base += sz
	}
	return found
}

// ---------- 编解码 ----------

func parseNeedle(typ, val string) ([]byte, error) {
	switch typ {
	case "i8":
		n, err := strconv.ParseInt(val, 10, 8)
		if err != nil {
			return nil, err
		}
		return []byte{byte(int8(n))}, nil
	case "i16":
		n, err := strconv.ParseInt(val, 10, 16)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(int16(n)))
		return b, nil
	case "i32":
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(int32(n)))
		return b, nil
	case "u32":
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(n))
		return b, nil
	case "i64":
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(n))
		return b, nil
	case "f32":
		f, err := strconv.ParseFloat(val, 32)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(f)))
		return b, nil
	case "f64":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, err
		}
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, math.Float64bits(f))
		return b, nil
	}
	return nil, fmt.Errorf("unknown type %q", typ)
}

func readValue(p *core.Process, typ string, addr uint32) string {
	sz := 4
	switch typ {
	case "i8", "u8":
		sz = 1
	case "i16", "u16":
		sz = 2
	case "i64", "f64":
		sz = 8
	}
	buf := make([]byte, sz)
	if !p.ReadBytes(addr, buf) {
		return "<read fail>"
	}
	switch typ {
	case "i8":
		return strconv.FormatInt(int64(int8(buf[0])), 10)
	case "i16":
		return strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(buf))), 10)
	case "i32":
		return strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(buf))), 10)
	case "u32":
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(buf)), 10)
	case "i64":
		return strconv.FormatInt(int64(binary.LittleEndian.Uint64(buf)), 10)
	case "f32":
		return strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(buf))), 'f', 4, 32)
	case "f64":
		return strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(buf)), 'f', 4, 64)
	}
	return fmt.Sprintf("% X", buf)
}

// ---------- 状态文件 ----------

func saveState(s state) {
	b, _ := json.Marshal(s)
	os.WriteFile(stateFile, b, 0644)
}

func loadState() state {
	var s state
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return s
	}
	json.Unmarshal(b, &s)
	return s
}

// cmdDiag 直接对活体进程运行 ori 解析器，报告定位结果与实际数值。
// auxWait 为 true 时等待后台单例定位（死亡计数/难度控制）完成。
func cmdDiag(p *core.Process) {
	auxWait := false
	for _, a := range os.Args {
		if a == "-wait" {
			auxWait = true
		}
	}
	prof := probeProfile()
	rt := &ori.Runtime{Prof: &prof}
	rt.SetProcess(p)
	t0 := time.Now()
	err := rt.ScanObjects()
	el := time.Since(t0)
	fmt.Printf("ScanObjects: err=%v elapsed=%v\n", err, el)
	fmt.Printf("  SeinLevel=0x%08X  SeinCharacter=0x%08X  DeathCounter=0x%08X  DiffController=0x%08X\n",
		rt.SeinLevelAddr(), rt.SeinCharacterAddr(), rt.DeathCounterAddr(), rt.SubAddr("diff"))
	fmt.Printf("  SoulFlame=0x%08X  SeinJump=0x%08X  DoubleJump=0x%08X\n",
		rt.SubAddr("soulflame"), rt.SubAddr("jump"), rt.SubAddr("doublejump"))
	snap := rt.Read()
	fmt.Printf("  snapshot: attached=%v seinOK=%v err=%q\n", snap.Attached, snap.SeinOK, snap.ScanError)
	fmt.Printf("  deaths=%d SP=%d EXP=%d energy=%.2f/%.2f hp=%.2f maxHP=%d\n",
		snap.Deaths, snap.SkillPoints, snap.Experience, snap.EnergyCur, snap.EnergyMax, snap.HealthCur, snap.HealthMax)

	if auxWait {
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			if rt.DeathCounterAddr() != 0 && rt.SubAddr("diff") != 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		fmt.Printf("  [等待后] DeathCounter=0x%08X DiffController=0x%08X\n",
			rt.DeathCounterAddr(), rt.SubAddr("diff"))
		if d := rt.DeathCounterAddr(); d != 0 {
			if v, ok := p.ReadI32(d + 0x14); ok {
				fmt.Printf("  [等待后] deathCount=%d\n", v)
			}
		}
		if dc := rt.SubAddr("diff"); dc != 0 {
			d1, _ := p.ReadI32(dc + 0x18)
			d2, _ := p.ReadI32(dc + 0x1C)
			fmt.Printf("  [等待后] Difficulty=%d Lowest=%d\n", d1, d2)
		}
	}
}

// cmdFindPtr 查找所有存放指定 4 字节值的槽位（定位静态单例字段用）。
// 可选地址范围过滤: probe findptr 0xVALUE [0xLO 0xHI]
func cmdFindPtr(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe findptr 0xHEXVALUE [0xLO 0xHI]")
		return
	}
	val := uint32(parseHex(os.Args[2]))
	lo, hi := uint64(0), uint64(0x100000000)
	if len(os.Args) >= 5 {
		lo, hi = parseHex(os.Args[3]), parseHex(os.Args[4])
	}
	needle := make([]byte, 4)
	binary.LittleEndian.PutUint32(needle, val)
	var hits []uint32
	for _, r := range normRegions(p.Handle) {
		if r.Base+r.Size < lo || r.Base > hi {
			continue
		}
		for _, h := range scanRegion(p, r, needle, 4) {
			if uint64(h) >= lo && uint64(h) < hi {
				hits = append(hits, h)
			}
		}
	}
	fmt.Printf("findptr 0x%08X in [0x%X,0x%X) -> %d hits\n", val, lo, hi, len(hits))
	for i, a := range hits {
		if i >= 80 {
			fmt.Printf("  ... (%d more)\n", len(hits)-i)
			break
		}
		// 标注前后邻域是否是同类指针（静态块特征）
		fmt.Printf("  0x%08X\n", a)
	}
}

// cmdStruct 以结构体视图转储一块内存: 偏移 / 原始 / int32 / float32 / 指针推测。
func cmdStruct(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe struct 0xADDR [ndwords]")
		return
	}
	addr := uint32(parseHex(os.Args[2]))
	n := 32
	if len(os.Args) >= 4 {
		n, _ = strconv.Atoi(os.Args[3])
	}
	buf := make([]byte, n*4)
	if !p.ReadBytes(addr, buf) {
		fmt.Println("read failed")
		return
	}
	fmt.Printf("struct @ 0x%08X\n", addr)
	for i := 0; i < n; i++ {
		off := i * 4
		raw := binary.LittleEndian.Uint32(buf[off:])
		f := math.Float32frombits(raw)
		note := ""
		if raw >= 0x10000 {
			// 可疑指针: 尝试读取目标首 dword（类指针通常是 0x2xxxxxxx-0x6xxxxxxx）
			if cls, ok := p.ReadU32(raw); ok && cls >= 0x20000000 && cls < 0xFFFF0000 {
				note = fmt.Sprintf("-> 0x%08X (class 0x%08X)", raw, cls)
			}
		}
		fmt.Printf("  +0x%03X  %08X  i32=%-12d f32=%-14.4g %s\n", off, raw, int32(raw), f, note)
	}
}

// cmdRegion 打印包含指定地址的内存区域信息。
func cmdRegion(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe region 0xADDR")
		return
	}
	addr := uintptr(parseHex(os.Args[2]))
	var m mbi
	r1, _, _ := procVirtualQueryEx.Call(p.Handle, addr, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
	if r1 == 0 {
		fmt.Printf("0x%08X: VirtualQueryEx failed\n", addr)
		return
	}
	fmt.Printf("0x%08X region: base=0x%X size=0x%X state=0x%X protect=0x%X type=0x%X\n",
		addr, uint64(m.BaseAddress), uint64(m.RegionSize), m.State, m.Protect, m.Type)
}

// cmdMono 用纯内存扫描复刻 CE 的 mono dissect:
// 通过类型名 -> MonoClass -> 实例首 dword 匹配，枚举某类型的所有堆对象。
func cmdMono(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe mono <TypeName>")
		return
	}
	name := os.Args[2]
	needle := append([]byte(name), 0)

	// 阶段1: 找类型名字符串
	t0 := time.Now()
	var strHits []uint32
	for _, r := range normRegions(p.Handle) {
		strHits = append(strHits, scanRegion(p, r, needle, 1)...)
		if len(strHits) > 60 {
			break
		}
	}
	fmt.Printf("阶段1 字符串 %q: %d 处 (%.1fs)\n", name, len(strHits), time.Since(t0).Seconds())
	for i, s := range strHits {
		if i >= 12 {
			fmt.Printf("  ... %d more\n", len(strHits)-i)
			break
		}
		fmt.Printf("  0x%08X\n", s)
	}

	// 阶段2: 找指向该字符串的 MonoClass（其 name 字段 @+0x0C == 字符串地址）
	seen := map[uint32]bool{}
	var classes []uint32
	for _, s := range strHits {
		if seen[s] {
			continue
		}
		seen[s] = true
		pn := make([]byte, 4)
		binary.LittleEndian.PutUint32(pn, s)
		for _, r := range normRegions(p.Handle) {
			for _, h := range scanRegion(p, r, pn, 4) {
				if v, ok := p.ReadU32(h + 0x0C); ok && v == s {
					classes = append(classes, h)
				}
			}
		}
	}
	fmt.Printf("阶段2 MonoClass 候选: %d\n", len(classes))
	for _, c := range classes {
		img, _ := p.ReadU32(c + 0x08)
		nsPtr, _ := p.ReadU32(c + 0x10)
		ns := readCStr(p, nsPtr)
		imgName := ""
		if img != 0 {
			if nptr, ok := p.ReadU32(img + 0x0C); ok && nptr != 0 {
				imgName = readCStr(p, nptr)
			}
		}
		fmt.Printf("  class @0x%08X ns=%q image=0x%08X(%s)\n", c, ns, img, imgName)
	}

	// 阶段3: 枚举首 dword == class 的实例
	for _, c := range classes {
		cn := make([]byte, 4)
		binary.LittleEndian.PutUint32(cn, c)
		var insts []uint32
		for _, r := range normRegions(p.Handle) {
			for _, h := range scanRegion(p, r, cn, 4) {
				insts = append(insts, h)
			}
		}
		fmt.Printf("class 0x%08X -> %d 个实例\n", c, len(insts))
		for i, a := range insts {
			if i >= 30 {
				fmt.Printf("  ... %d more\n", len(insts)-i)
				break
			}
			fmt.Printf("  0x%08X\n", a)
		}
	}
}

// readCStr 读取以 NUL 结尾的 ASCII 字符串（上限 128 字节）。
func readCStr(p *core.Process, addr uint32) string {
	if addr == 0 {
		return ""
	}
	buf := make([]byte, 128)
	if !p.ReadBytes(addr, buf) {
		return "?"
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// cmdFindObj 查找"某对象字段等于指定值"的对象:
//
//	probe findobj <value> <fieldOff> [dumpOff]
//
// 即: 找到所有 addr 满足 u32(addr)==value, 报告对象 addr-fieldOff。
func cmdFindObj(p *core.Process) {
	if len(os.Args) < 4 {
		fmt.Println("usage: probe findobj <value> <fieldOff> [dumpOff]")
		return
	}
	val := uint32(parseHex(os.Args[2]))
	foff := uint32(parseHex(os.Args[3]))
	dump := uint32(0xFFFFFFFF)
	if len(os.Args) >= 5 {
		dump = uint32(parseHex(os.Args[4]))
	}
	needle := make([]byte, 4)
	binary.LittleEndian.PutUint32(needle, val)
	var hits []uint32
	for _, r := range normRegions(p.Handle) {
		hits = append(hits, scanRegion(p, r, needle, 4)...)
	}
	seen := map[uint32]bool{}
	n := 0
	for _, a := range hits {
		obj := a - foff
		if obj&3 != 0 || seen[obj] {
			continue
		}
		seen[obj] = true
		cls, _ := p.ReadU32(obj)
		if cls < 0x20000000 {
			continue
		}
		n++
		if n > 60 {
			fmt.Println("  ...more")
			break
		}
		extra := ""
		if dump != 0xFFFFFFFF {
			if dv, ok := p.ReadU32(obj + dump); ok {
				extra = fmt.Sprintf("  +0x%X: u32=%d i32=%d f32=%.4g", dump, dv, int32(dv), math.Float32frombits(dv))
			}
		}
		fmt.Printf("  obj=0x%08X class=0x%08X slot=0x%08X%s\n", obj, cls, a, extra)
	}
	fmt.Printf("findobj value=0x%08X foff=0x%X -> %d objects\n", val, foff, n)
}

// cmdDiffScan 在静态字段区扫描 DifficultyController.Instance 候选:
// 值 v 满足 [+0x04]==0, [+0x18]/[+0x1C] ∈ [0,3], [+0x24]==0,
// 且 +0x0C/+0x10/+0x14/+0x20 为堆/代码指针。
func cmdDiffScan(p *core.Process) {
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x20000000 {
			continue
		}
		buf := make([]byte, r.Size)
		if !p.ReadBytes(uint32(r.Base), buf) {
			continue
		}
		for off := 0; off+0x28 <= len(buf); off += 4 {
			v := u32atb(buf, off)
			if v < 0x40000000 || v >= 0xFFFF0000 || v&3 != 0 {
				continue
			}
			if u32atb(buf, off+4) != 0 || u32atb(buf, off+0x24) != 0 {
				continue
			}
			d := u32atb(buf, off+0x18)
			l := u32atb(buf, off+0x1C)
			if d > 3 || l > 3 {
				continue
			}
			c1 := u32atb(buf, off+0x0C)
			c2 := u32atb(buf, off+0x10)
			c3 := u32atb(buf, off+0x14)
			del := u32atb(buf, off+0x20)
			if c1 < 0x40000000 || c2 < 0x40000000 || c3 < 0x40000000 || del < 0x20000000 {
				continue
			}
			cls, _ := p.ReadU32(v)
			fmt.Printf("  slot=0x%08X inst=0x%08X class=0x%08X diff=%d lowest=%d del=0x%08X (c12=0x%08X c16=0x%08X c20=0x%08X)\n",
				r.Base+uint64(off), v, cls, d, l, del, c1, c2, c3)
		}
	}
	fmt.Println("diffscan done")
}

func u32atb(buf []byte, off int) uint32 {
	return uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
}

// cmdKlass 解析 vtable -> MonoClass -> 类型名。
//
//	probe klass 0xVTABLE
func cmdKlass(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe klass 0xVTABLE")
		return
	}
	vt := uint32(parseHex(os.Args[2]))
	klass, ok := p.ReadU32(vt)
	fmt.Printf("vtable 0x%08X -> klass 0x%08X (ok=%v)\n", vt, klass, ok)
	if !ok || klass < 0x10000 {
		return
	}
	// 在 klass[0..0x80] 里找指向 ASCII 字符串的指针
	for off := 0; off < 0x80; off += 4 {
		v, ok := p.ReadU32(klass + uint32(off))
		if !ok || v < 0x10000 {
			continue
		}
		s := readCStr(p, v)
		if len(s) >= 2 && isPrintable(s) {
			fmt.Printf("  klass+0x%02X -> 0x%08X %q\n", off, v, s)
		}
	}
}

// cmdClass 按类型名定位 MonoClass / vtable / 全部实例。
//
//	probe class <TypeName>
func cmdClass(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe class <TypeName>")
		return
	}
	name := os.Args[2]
	needle := append([]byte(name), 0)
	t0 := time.Now()

	// 阶段1: 低区找类型名字符串（元数据区在 0x30000000 以下）
	var strs []uint32
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x30000000 {
			continue
		}
		strs = append(strs, scanRegion(p, r, needle, 1)...)
	}
	fmt.Printf("[1] 字符串 %q: %d 处 (%.1fs)\n", name, len(strs), time.Since(t0).Seconds())

	// 阶段2: 找指向该字符串的槽位 -> klass = slot - nameOff
	// 先用一个已知实例反推 nameOff（试 0x08..0x40），保留能同时满足
	// "klass+0x08 是合法指针" 的偏移。
	foundKlass := map[uint32]uint32{} // klass -> nameOff
	for _, s := range strs {
		pn := make([]byte, 4)
		binary.LittleEndian.PutUint32(pn, s)
		for _, r := range normRegions(p.Handle) {
			if r.Base >= 0x30000000 {
				continue
			}
			for _, slot := range scanRegion(p, r, pn, 4) {
				for _, no := range []uint32{8, 12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60} {
					k := slot - no
					if k < 0x10000 || k&3 != 0 {
						continue
					}
					// klass 首字段应是指针（element_class/cast_class 等）
					if c0, ok := p.ReadU32(k); ok && c0 >= 0x10000 && c0 < 0xFFFF0000 {
						if _, dup := foundKlass[k]; !dup {
							foundKlass[k] = no
						}
					}
				}
			}
		}
	}
	fmt.Printf("[2] MonoClass 候选: %d\n", len(foundKlass))

	// 阶段3: 找 vtable（指向 klass 的槽位），枚举实例（首 dword == vtable）
	for k, no := range foundKlass {
		kn := make([]byte, 4)
		binary.LittleEndian.PutUint32(kn, k)
		var vts []uint32
		for _, r := range normRegions(p.Handle) {
			for _, s := range scanRegion(p, r, kn, 4) {
				vts = append(vts, s)
			}
		}
		if len(vts) == 0 {
			continue
		}
		fmt.Printf("[3] klass=0x%08X nameOff=0x%X vtable 槽位: %d 个\n", k, no, len(vts))
		for vi, vt := range vts {
			if vi >= 3 {
				break
			}
			vn := make([]byte, 4)
			binary.LittleEndian.PutUint32(vn, vt)
			var insts []uint32
			for _, r := range normRegions(p.Handle) {
				for _, h := range scanRegion(p, r, vn, 4) {
					insts = append(insts, h)
				}
			}
			fmt.Printf("    vtable@0x%08X -> %d 个实例\n", vt, len(insts))
			for i, a := range insts {
				if i >= 12 {
					fmt.Printf("      ... %d more\n", len(insts)-i)
					break
				}
				fmt.Printf("      0x%08X\n", a)
			}
		}
	}
}

// cmdLive 定位活体玩家对象（通过静态引用判定）:
// 扫描低地址区（< 0x10000000，mono 静态数据区）中形如
// [P, x, y, P] 的 12 字节间隔双引用，再验证 P 是活体 SeinCharacter。
func cmdLive(p *core.Process) {
	t0 := time.Now()
	// 收集低区所有可疑堆指针槽位
	type slot struct{ at, val uint32 }
	byVal := map[uint32][]uint32{}
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x10000000 {
			continue
		}
		buf := make([]byte, r.Size)
		if !p.ReadBytes(uint32(r.Base), buf) {
			continue
		}
		for off := 0; off+16 <= len(buf); off += 4 {
			v := u32atb(buf, off)
			if v < 0x40000000 || v >= 0xFFFF0000 || v&3 != 0 {
				continue
			}
			v2 := u32atb(buf, off+12)
			if v == v2 {
				byVal[v] = append(byVal[v], uint32(r.Base)+uint32(off))
			}
		}
	}
	fmt.Printf("[1] [P,x,y,P] 双引用候选: %d 个值 (%.1fs)\n", len(byVal), time.Since(t0).Seconds())

	// 验证每个候选 P 是否是活体 SeinCharacter
	t1 := time.Now()
	n := 0
	for v, slots := range byVal {
		if !validateSein(p, v) {
			continue
		}
		n++
		fmt.Printf("[2] 活体 SeinCharacter = 0x%08X  静态槽: ", v)
		for i, s := range slots {
			if i >= 6 {
				fmt.Printf("...(%d)", len(slots)-i)
				break
			}
			fmt.Printf("0x%08X ", s)
		}
		fmt.Println()
		dumpSeinGraph(p, v)
	}
	fmt.Printf("[2] 验证通过 %d 个 (%.1fs)\n", n, time.Since(t1).Seconds())
}

// validateSein 验证 P 是否满足活体 SeinCharacter 结构。
func validateSein(p *core.Process, v uint32) bool {
	vt, ok := p.ReadU32(v)
	if !ok || vt < 0x20000000 || vt >= 0xFFFF0000 {
		return false
	}
	lvl, ok1 := p.ReadU32(v + 0x38)
	en, ok2 := p.ReadU32(v + 0x3C)
	mor, ok3 := p.ReadU32(v + 0x40)
	if !ok1 || !ok2 || !ok3 || lvl < 0x40000000 || en < 0x40000000 || mor < 0x40000000 {
		return false
	}
	if back, ok := p.ReadU32(lvl + 0x20); !ok || back != v {
		return false
	}
	cur, okc := p.ReadF32(en + 0x20)
	max, okm := p.ReadF32(en + 0x24)
	if !okc || !okm || cur < -0.01 || max < 0 || max > 1000 || cur > max+0.01 {
		return false
	}
	h, okh := p.ReadU32(mor + 0x0C)
	if !okh || h < 0x40000000 {
		return false
	}
	amt, oka := p.ReadF32(h + 0x1C)
	mh, okx := p.ReadI32(h + 0x20)
	if !oka || !okx || amt < -0.01 || mh < 4 || mh > 400 {
		return false
	}
	return true
}

func dumpSeinGraph(p *core.Process, v uint32) {
	lvl, _ := p.ReadU32(v + 0x38)
	en, _ := p.ReadU32(v + 0x3C)
	mor, _ := p.ReadU32(v + 0x40)
	sf, _ := p.ReadU32(v + 0x28)
	ab, _ := p.ReadU32(v + 0x10)
	pb, _ := p.ReadU32(v + 0x48)
	pa, _ := p.ReadU32(v + 0x4C)
	sp, _ := p.ReadI32(lvl + 0x24)
	exp, _ := p.ReadI32(lvl + 0x2C)
	cur, _ := p.ReadF32(en + 0x20)
	max, _ := p.ReadF32(en + 0x24)
	h, _ := p.ReadU32(mor + 0x0C)
	amt, _ := p.ReadF32(h + 0x1C)
	mh, _ := p.ReadI32(h + 0x20)
	fmt.Printf("      Level=0x%08X SP=%d Exp=%d | Energy=0x%08X %.2f/%.2f | Health=0x%08X %.2f/%d\n",
		lvl, sp, exp, en, cur, max, h, amt, mh)
	fmt.Printf("      SoulFlame=0x%08X Abilities=0x%08X PlatformBeh=0x%08X PlayerAbil=0x%08X\n",
		sf, ab, pb, pa)
}

// className 通过对象地址取类型名: obj -> vtable -> klass -> klass+0x30 -> name。
func className(p *core.Process, obj uint32) string {
	vt, ok := p.ReadU32(obj)
	if !ok || vt < 0x10000 {
		return ""
	}
	klass, ok := p.ReadU32(vt)
	if !ok || klass < 0x10000 {
		return ""
	}
	np, ok := p.ReadU32(klass + 0x30)
	if !ok || np < 0x10000 {
		return ""
	}
	s := readCStr(p, np)
	if len(s) == 0 || len(s) > 120 || !isPrintable(s) {
		return ""
	}
	return s
}

// findKlassByName 通过类型名字符串定位 MonoClass。
//
// 原理: MonoClass+0x30 存放指向类型名字符串的指针，且 klass 首字段自指。
// 步骤: ① 在低区找到名字字符串  ② 找到"存放该字符串指针的槽位 h"
// ③ klass = h - 0x30，并用自指 + 名字回读双重校验。
func findKlassByName(p *core.Process, name string) uint32 {
	needle := append([]byte(name), 0)
	regs := normRegions(p.Handle)

	// ① 字符串出现位置（元数据都在低区，限制范围提速）
	var strs []uint32
	for _, r := range regs {
		if r.Base >= 0x30000000 {
			continue
		}
		strs = append(strs, scanRegion(p, r, needle, 1)...)
	}
	// ② 反向查找槽位
	for _, s := range strs {
		pn := make([]byte, 4)
		binary.LittleEndian.PutUint32(pn, s)
		for _, r := range regs {
			if r.Base >= 0x30000000 {
				continue
			}
			for _, h := range scanRegion(p, r, pn, 4) {
				k := h - 0x30
				if k < 0x10000 || k&3 != 0 {
					continue
				}
				if c0, ok := p.ReadU32(k); !ok || c0 != k {
					continue
				}
				np, ok := p.ReadU32(k + 0x30)
				if !ok || np != s {
					continue
				}
				return k
			}
		}
	}
	return 0
}

// cmdStaticRef 定位某类型被静态数据块引用的实例（单例定位通用方法）。
//
//	probe staticref <ClassName>
//
// 方法: 解析目标 klass，然后批量读取低地址区（< 0x10000000，mono 静态数据
// 所在带），对所有形如堆指针的 dword 解析 vtable->klass，命中目标 klass
// 即输出该静态引用槽位与实例字段。
func cmdStaticRef(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe staticref <ClassName>")
		return
	}
	name := os.Args[2]
	t0 := time.Now()
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q\n", name)
		return
	}
	fmt.Printf("klass=0x%08X (%q) [%.2fs]\n", k, name, time.Since(t0).Seconds())

	type slot struct{ at, val uint32 }
	var slots []slot
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x10000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+4 <= int(sz); off += 4 {
				v := u32atb(buf, off)
				if v < 0x40000000 || v >= 0xFFFF0000 || v&3 != 0 {
					continue
				}
				slots = append(slots, slot{uint32(base) + uint32(off), v})
			}
		}
	}
	fmt.Printf("低区候选堆指针: %d [%.2fs]\n", len(slots), time.Since(t0).Seconds())

	vtCache := map[uint32]uint32{}
	seenInst := map[uint32]bool{}
	hits := 0
	for _, s := range slots {
		kv, ok := vtCache[s.val]
		if !ok {
			if vt, ok2 := p.ReadU32(s.val); ok2 && vt >= 0x40000000 && vt < 0xFFFF0000 {
				if kk, ok3 := p.ReadU32(vt); ok3 {
					kv = kk
				}
			}
			vtCache[s.val] = kv
		}
		if kv != k {
			continue
		}
		hits++
		seenInst[s.val] = true
		fmt.Printf("  槽位 0x%08X -> 实例 0x%08X\n", s.at, s.val)
	}
	fmt.Printf("命中 %d 槽位 / %d 实例\n", hits, len(seenInst))
	for inst := range seenInst {
		dumpFields(p, inst, 0x80)
	}
}

// dumpFields 以 4 字节步长转储对象字段，标注指针目标类名。
func dumpFields(p *core.Process, obj uint32, n int) {
	fmt.Printf("--- 对象 0x%08X (%s) ---\n", obj, className(p, obj))
	for i := 0; i < n/4; i++ {
		off := uint32(i * 4)
		v, ok := p.ReadU32(obj + off)
		if !ok {
			break
		}
		note := fmt.Sprintf("i32=%-11d f32=%-12.5g", int32(v), math.Float32frombits(v))
		if v >= 0x10000000 && v < 0xFFFF0000 {
			if cn := className(p, v); cn != "" {
				note += "  -> " + cn
			} else if s := readCStr(p, v); isIdent(s) {
				note += fmt.Sprintf("  -> str %q", s)
			}
		}
		fmt.Printf("  +0x%03X  %08X  %s\n", off, v, note)
	}
}

// cmdStaticMap 转储 mono 静态数据带里所有"指向堆对象"的槽位及其类型名。
//
//	probe staticmap [filter]
//
// 这是定位单例（Instance 静态字段）的万能手段：低地址区（< 0x10000000）
// 中每个形如堆指针的 dword，解析其 vtable->klass->name，按类型名聚合输出。
func cmdStaticMap(p *core.Process) {
	filter := ""
	if len(os.Args) >= 3 {
		filter = os.Args[2]
	}
	t0 := time.Now()

	type hit struct{ at, val uint32 }
	byName := map[string][]hit{}
	vtToName := map[uint32]string{}
	klassToName := map[uint32]string{}

	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x10000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+4 <= int(sz); off += 4 {
				v := u32atb(buf, off)
				if v < 0x40000000 || v >= 0xFFFF0000 || v&3 != 0 {
					continue
				}
				nm, ok := vtToName[v]
				if !ok {
					nm = ""
					if vt, ok2 := p.ReadU32(v); ok2 && vt >= 0x40000000 && vt < 0xFFFF0000 {
						if kn, ok3 := klassToName[vt]; ok3 {
							nm = kn
						} else if np, ok4 := p.ReadU32(vt + 0x30); ok4 && np >= 0x10000 {
							nm = readCStr(p, np)
						}
						klassToName[vt] = nm
					}
					vtToName[v] = nm
				}
				if nm == "" || !isIdent(nm) {
					continue
				}
				byName[nm] = append(byName[nm], hit{uint32(base) + uint32(off), v})
			}
		}
	}

	// 按引用数排序输出
	type kv struct {
		name string
		hits []hit
	}
	var list []kv
	for k, v := range byName {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i].hits) > len(list[j].hits) })

	fmt.Printf("静态带类型引用统计: %d 个类型 [%.2fs]\n", len(list), time.Since(t0).Seconds())
	for _, e := range list {
		if filter != "" && !strings.Contains(e.name, filter) {
			continue
		}
		fmt.Printf("  %-42s x%d\n", e.name, len(e.hits))
		for i, h := range e.hits {
			if i >= 6 {
				fmt.Printf("      ... %d more\n", len(e.hits)-i)
				break
			}
			fmt.Printf("      slot 0x%08X -> 0x%08X\n", h.at, h.val)
		}
	}
}

// cmdInstances 枚举某类的全部活体实例，并列出引用它们的静态槽位。
//
//	probe instances <ClassName>
//
// 原理: 对象[0] = vtable 槽位 V，且 u32(V) == klass。V 可能位于元数据区
// （非 MonoBehaviour 类）或堆区（MonoBehaviour 类），因此不限制 V 的地址。
// 实例判据: 位于堆区 (>=0x40000000)、4 字节对齐、[4]==0。
func cmdInstances(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe instances <ClassName>")
		return
	}
	name := os.Args[2]
	t0 := time.Now()
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q\n", name)
		return
	}
	fmt.Printf("klass=0x%08X (%q) [%.2fs]\n", k, name, time.Since(t0).Seconds())

	// ① vtable 槽位: 任何位置存放 klass 指针的槽位（排除 klass 自身区域）
	kn := make([]byte, 4)
	binary.LittleEndian.PutUint32(kn, k)
	var vts []uint32
	for _, r := range normRegions(p.Handle) {
		for _, s := range scanRegion(p, r, kn, 4) {
			if s&3 != 0 || (s >= k && s < k+0x800) {
				continue
			}
			vts = append(vts, s)
		}
	}
	fmt.Printf("vtable 槽位: %d [%.2fs]\n", len(vts), time.Since(t0).Seconds())

	// ② 堆区实例: [0]==V 且 [4]==0
	type inst struct {
		at, vt uint32
	}
	var found []inst
	seen := map[uint32]bool{}
	for _, vt := range vts {
		vn := make([]byte, 4)
		binary.LittleEndian.PutUint32(vn, vt)
		for _, r := range normRegions(p.Handle) {
			if r.Base < 0x40000000 {
				continue
			}
			for _, o := range scanRegion(p, r, vn, 4) {
				if o < 0x40000000 || o&3 != 0 || seen[o] {
					continue
				}
				if s, ok := p.ReadU32(o + 4); !ok || s != 0 {
					continue
				}
				seen[o] = true
				found = append(found, inst{o, vt})
			}
		}
	}
	fmt.Printf("堆实例: %d [%.2fs]\n", len(found), time.Since(t0).Seconds())

	for i, in := range found {
		if i >= 24 {
			fmt.Printf("  ... %d more\n", len(found)-i)
			break
		}
		on := make([]byte, 4)
		binary.LittleEndian.PutUint32(on, in.at)
		var refs []uint32
		for _, r := range normRegions(p.Handle) {
			if r.Base >= 0x10000000 {
				continue
			}
			refs = append(refs, scanRegion(p, r, on, 4)...)
		}
		extra := ""
		for i2, rf := range refs {
			if i2 >= 3 {
				extra += "..."
				break
			}
			extra += fmt.Sprintf("0x%08X ", rf)
		}
		fmt.Printf("  inst=0x%08X vt=0x%08X 静态引用: %s\n", in.at, in.vt, extra)
	}
	if len(found) > 0 {
		dumpFields(p, found[0].at, 0x60)
	}
}

// cmdOffs 批量解析字段偏移（等价 CE mono dissect）。
//
//	probe offs <ClassName> <Field1> [Field2 ...]
//
// 一次读取低地址带（mono 元数据区）到内存，然后为每个字段名查找
// 其"字段描述符"槽位 H: u32(H)==字符串地址 && u32(H+4)==声明类 klass。
// 输出字段名、偏移 (u32(H+8))、声明类名。
func cmdOffs(p *core.Process) {
	if len(os.Args) < 4 {
		fmt.Println("usage: probe offs <ClassName> <Field1> [Field2 ...]")
		return
	}
	filter := os.Args[2]
	want := os.Args[3:]
	t0 := time.Now()

	// 读取元数据/静态数据带（< 0x40000000）。mono 的类元数据在
	// 0x2A-0x2B 段，静态字段数据在 0x06 段，两者都在此范围内。
	type region struct {
		base uint32
		data []byte
	}
	var bands []region
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x40000000 {
			continue
		}
		buf := make([]byte, r.Size)
		if !p.ReadBytes(uint32(r.Base), buf) {
			continue
		}
		bands = append(bands, region{uint32(r.Base), buf})
	}
	n := 0
	for _, b := range bands {
		n += len(b.data)
	}
	fmt.Printf("已读入 %.1f MB [%.2fs]\n", float64(n)/1048576, time.Since(t0).Seconds())

	for _, w := range want {
		needle := append([]byte(w), 0)
		found := 0
		for _, b := range bands {
			for i := 0; i+len(needle) <= len(b.data); i++ {
				if b.data[i] != needle[0] || !bytes.Equal(b.data[i:i+len(needle)], needle) {
					continue
				}
				sAddr := b.base + uint32(i)
				sn := make([]byte, 4)
				binary.LittleEndian.PutUint32(sn, sAddr)
				// 在该带内查指向 sAddr 的槽位
				for _, b2 := range bands {
					for j := 0; j+8 <= len(b2.data); j += 4 {
						if !bytes.Equal(b2.data[j:j+4], sn) {
							continue
						}
						declKlass := u32atb(b2.data, j+4)
						ofs := u32atb(b2.data, j+8)
						if ofs > 0x40000 {
							continue
						}
						declName := ""
						if declKlass >= 0x10000 {
							if np, ok := p.ReadU32(declKlass + 0x30); ok && np >= 0x10000 {
								declName = readCStr(p, np)
							}
						}
						if filter != "" && filter != "*" && declName != filter {
							continue
						}
						fmt.Printf("  %-32s +0x%-5X  声明类=%s\n", w, ofs, declName)
						found++
					}
				}
			}
		}
		if found == 0 {
			fmt.Printf("  %-32s <未找到>\n", w)
		}
	}
	fmt.Printf("完成 [%.2fs]\n", time.Since(t0).Seconds())
}

// cmdRefs 查找堆区中所有指向指定地址的槽位（用于反查静态引用）。
//
//	probe refs 0xADDR [maxShow]
func cmdRefs(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe refs 0xADDR [maxShow]")
		return
	}
	target := uint32(parseHex(os.Args[2]))
	maxShow := 40
	if len(os.Args) >= 4 {
		maxShow, _ = strconv.Atoi(os.Args[3])
	}
	needle := make([]byte, 4)
	binary.LittleEndian.PutUint32(needle, target)
	n := 0
	for _, r := range normRegions(p.Handle) {
		for _, a := range scanRegion(p, r, needle, 4) {
			n++
			if n > maxShow {
				continue
			}
			note := ""
			if owner := className(p, a); owner != "" {
				note = "  (位于 " + owner + " 对象内)"
			}
			fmt.Printf("  0x%08X%s\n", a, note)
		}
	}
	fmt.Printf("refs 0x%08X -> %d 处\n", target, n)
}

// cmdObjOf 在堆区全量扫描某类的全部实例（按类名严格校验）。
//
//	probe objof <ClassName> [nFields]
func cmdObjOf(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe objof <ClassName> [nFields]")
		return
	}
	target := os.Args[2]
	n := 0x40
	if len(os.Args) >= 4 {
		n, _ = strconv.Atoi(os.Args[3])
	}
	t0 := time.Now()
	type hit struct{ at, vt uint32 }
	var hits []hit
	nameCache := map[uint32]string{} // vtable -> class name
	seen := map[uint32]bool{}

	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+8 <= int(sz); off += 4 {
				vt := u32atb(buf, off)
				if vt < 0x08000000 || vt >= 0xFFFF0000 || vt&3 != 0 {
					continue
				}
				nm, ok := nameCache[vt]
				if !ok {
					obj := uint32(base) + uint32(off)
					nm = ""
					if s, _, ok2 := classOf(p, obj); ok2 {
						nm = s
					}
					nameCache[vt] = nm
				}
				if nm != target {
					continue
				}
				obj := uint32(base) + uint32(off)
				if seen[obj] {
					continue
				}
				// [4] 是 mono 监视器/同步块，未锁定时为 0
				if m, ok := p.ReadU32(obj + 4); !ok || m != 0 {
					continue
				}
				seen[obj] = true
				hits = append(hits, hit{obj, vt})
			}
		}
	}
	fmt.Printf("objof %q -> %d 实例 [%.2fs]\n", target, len(hits), time.Since(t0).Seconds())
	for i, h := range hits {
		if i >= 8 {
			fmt.Printf("  ... %d more\n", len(hits)-i)
			break
		}
		fmt.Printf("== 实例 0x%08X vtable 0x%08X\n", h.at, h.vt)
		dumpFields(p, h.at, n)
	}
}

// classOf 解析对象地址 -> (类型名, klass)。
//
// 判据: 对象在堆带、vtable 合法、klass 自指（mono MonoClass 固有签名
// u32(klass)==klass）。自指校验可排除堆上垃圾数据凑出的假 klass。
// vtable 允许位于元数据带（纯托管类）或堆带（MonoBehaviour 派生类）。
func classOf(p *core.Process, obj uint32) (string, uint32, bool) {
	if obj < 0x40000000 || obj >= 0xFFFF0000 {
		return "", 0, false
	}
	vt, ok := p.ReadU32(obj)
	if !ok || vt < 0x08000000 || vt >= 0xFFFF0000 || vt&3 != 0 {
		return "", 0, false
	}
	k, ok := p.ReadU32(vt)
	if !ok || k < 0x08000000 || k >= 0xFFFF0000 || k == vt {
		return "", 0, false
	}
	if k0, ok := p.ReadU32(k); !ok || k0 != k { // klass 自指
		return "", 0, false
	}
	np, ok := p.ReadU32(k + 0x30)
	if !ok || np < 0x08000000 {
		return "", 0, false
	}
	s := readCStr(p, np)
	if !isIdent(s) {
		return "", 0, false
	}
	return s, k, true
}

// cmdFields2 权威字段表读取：遍历 MonoClass 的 fields 数组（元素为
// MonoClassField* 指针），逐项输出 名称 / 类型 / 对象内偏移。
//
//	probe fields2 <ClassName>
//
// MonoClassField（本版 mono）实测布局:
//
//	+0x00 const char *name
//	+0x04 MonoType   *type
//	+0x08 int         offset
//
// 已用 m_numberOfJumpsAvailable -> 0x40 交叉验证。
func cmdFields2(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe fields2 <ClassName>")
		return
	}
	name := os.Args[2]
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q\n", name)
		return
	}
	fmt.Printf("klass=0x%08X (%q)\n", k, name)

	// 定位 fields 数组: klass[0x20..0x100] 中的指针 A，满足
	// u32(A) 是指向 ASCII 标识符的指针，且 u32(u32(A)+8) 是小于 0x10000 的整数。
	type arrCand struct {
		off, arr uint32
	}
	var cands []arrCand
	for off := uint32(0x20); off < 0x120; off += 4 {
		arr, ok := p.ReadU32(k + off)
		if !ok || arr < 0x10000 || arr&3 != 0 {
			continue
		}
		d0, ok := p.ReadU32(arr)
		if !ok || d0 < 0x10000 {
			continue
		}
		s0 := readCStr(p, d0)
		if !isIdent(s0) {
			continue
		}
		o0, ok := p.ReadU32(d0 + 8)
		if !ok || o0 > 0x40000 {
			continue
		}
		cands = append(cands, arrCand{off, arr})
	}
	if len(cands) == 0 {
		fmt.Println("  未找到字段数组")
		return
	}
	for _, c := range cands {
		fmt.Printf("--- klass+0x%02X -> fields 数组 0x%08X ---\n", c.off, c.arr)
		for i := 0; i < 300; i++ {
			desc, ok := p.ReadU32(c.arr + uint32(i*4))
			if !ok || desc < 0x10000 {
				break
			}
			nmPtr, _ := p.ReadU32(desc)
			nm := readCStr(p, nmPtr)
			if !isIdent(nm) {
				break
			}
			typ, _ := p.ReadU32(desc + 4)
			ofs, _ := p.ReadU32(desc + 8)
			if ofs > 0x40000 {
				break
			}
			tn := ""
			if typ >= 0x10000 {
				if tp, ok := p.ReadU32(typ + 4); ok && tp >= 0x10000 {
					tn = readCStr(p, tp)
				}
			}
			fmt.Printf("  +0x%04X  %-36s %s\n", ofs, nm, tn)
		}
	}
}

// cmdStrRef 查找所有"指向指定字符串"的槽位，并打印其后 8 字节。
//
//	probe strref <string>
//
// 用途: 定位 mono 字段描述符（MonoClassField: name*, type*, offset）。
// 命中槽位 H 若满足 H+4 是合法指针、H+8 是小的整数，则 H+8 很可能就是
// 该字段在对象内的字节偏移 —— 这是与 CE "mono dissect" 等价的权威来源。
func cmdStrRef(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe strref <string>")
		return
	}
	target := os.Args[2]
	needle := append([]byte(target), 0)
	t0 := time.Now()

	regs := normRegions(p.Handle)
	var strs []uint32
	for _, r := range regs {
		if r.Base >= 0x40000000 {
			continue
		}
		strs = append(strs, scanRegion(p, r, needle, 1)...)
	}
	fmt.Printf("字符串 %q: %d 处 [%.2fs]\n", target, len(strs), time.Since(t0).Seconds())

	seen := map[uint32]bool{}
	for _, s := range strs {
		sn := make([]byte, 4)
		binary.LittleEndian.PutUint32(sn, s)
		for _, r := range regs {
			for _, h := range scanRegion(p, r, sn, 4) {
				if h < 0x10000 || seen[h] {
					continue
				}
				seen[h] = true
				typ, _ := p.ReadU32(h + 4)
				ofs, _ := p.ReadU32(h + 8)
				mark := ""
				if typ >= 0x10000 && ofs <= 0x1000 {
					mark = "  <== 疑似字段描述符"
				}
				fmt.Printf("  槽位 0x%08X  字符串 0x%08X  +4=0x%08X  +8=0x%08X(%d)%s\n",
					h, s, typ, ofs, ofs, mark)
			}
		}
	}
}

// findSingleton 在 mono 静态数据带（低区）中查找存放指定类型实例的槽位。
//
//	probe singleton <ClassName>
func cmdSingleton(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe singleton <ClassName>")
		return
	}
	target := os.Args[2]
	t0 := time.Now()
	type hit struct{ at, val uint32 }
	var hits []hit
	for _, r := range normRegions(p.Handle) {
		if r.Base >= 0x10000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+4 <= int(sz); off += 4 {
				v := u32atb(buf, off)
				if v < 0x40000000 || v >= 0xFFFF0000 || v&3 != 0 {
					continue
				}
				if nm, _, ok := classOf(p, v); ok && nm == target {
					hits = append(hits, hit{uint32(base) + uint32(off), v})
				}
			}
		}
	}
	fmt.Printf("singleton %q -> %d 槽位 [%.2fs]\n", target, len(hits), time.Since(t0).Seconds())
	for _, h := range hits {
		fmt.Printf("  slot 0x%08X -> 0x%08X\n", h.at, h.val)
		dumpFields(p, h.val, 0x60)
	}
}

// cmdFindField 从 mono 元数据中查找某类中指定名字的字段及其偏移。
//
//	probe findfield <ClassName> <FieldName> [FieldName2 ...]
//
// 原理: MonoClass 持有 fields 数组（klass+0x48 起），每项若为 8 字节
// （name 指针 + type 指针 + offset 紧邻其后），逐项匹配字段名。
// 采用"锚定法"：先用已知字段（如 Current/Max）确定 name 指针出现的形式，
// 再输出同表中其它字段的偏移。
func cmdFindField(p *core.Process) {
	if len(os.Args) < 4 {
		fmt.Println("usage: probe findfield <ClassName> <FieldName>...")
		return
	}
	name := os.Args[2]
	want := os.Args[3:]
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q\n", name)
		return
	}
	fmt.Printf("klass=0x%08X (%q)\n", k, name)

	// 收集本类所有字段名字符串指针出现的槽位，作为字段描述符候选。
	// 字段描述符布局（Mono 32 位）: name*(+0) type*(+4) offset(+8)
	type fld struct {
		desc, namePtr, off uint32
	}
	var fields []fld
	seen := map[uint32]bool{}
	for _, wantName := range want {
		needle := append([]byte(wantName), 0)
		for _, r := range normRegions(p.Handle) {
			if r.Base >= 0x30000000 {
				continue
			}
			for _, s := range scanRegion(p, r, needle, 1) {
				sn := make([]byte, 4)
				binary.LittleEndian.PutUint32(sn, s)
				for _, r2 := range normRegions(p.Handle) {
					for _, h := range scanRegion(p, r2, sn, 4) {
						if h < 0x10000 || seen[h] {
							continue
						}
						ofs, ok := p.ReadU32(h + 8)
						typ, ok2 := p.ReadU32(h + 4)
						if !ok || !ok2 {
							continue
						}
						if ofs > 0x40000 || typ < 0x10000 {
							continue
						}
						seen[h] = true
						fields = append(fields, fld{h, s, ofs})
					}
				}
			}
		}
	}
	if len(fields) == 0 {
		fmt.Println("未找到任何字段描述符")
		return
	}
	for _, f := range fields {
		fmt.Printf("  字段 %-28s offset=0x%-5X desc=0x%08X namePtr=0x%08X\n",
			readCStr(p, f.namePtr), f.off, f.desc, f.namePtr)
	}
}

// cmdFindClass 定位某类型的活体实例与静态引用槽位。
//
//	probe findclass <ClassName>
func cmdFindClass(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe findclass <ClassName>")
		return
	}
	name := os.Args[2]
	t0 := time.Now()
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q\n", name)
		return
	}
	fmt.Printf("klass=0x%08X (%q) [%.1fs]\n", k, name, time.Since(t0).Seconds())

	// vtable 候选: 指向 klass 且不等于 klass 的槽位
	kn := make([]byte, 4)
	binary.LittleEndian.PutUint32(kn, k)
	var vts []uint32
	for _, r := range normRegions(p.Handle) {
		for _, s := range scanRegion(p, r, kn, 4) {
			if s == k || s&3 != 0 {
				continue
			}
			vts = append(vts, s)
		}
	}
	fmt.Printf("vtable 候选: %d\n", len(vts))

	// 实例: [0]==vtable 且 [4]==0 且位于堆区 (>=0x40000000)
	seen := map[uint32]bool{}
	var insts []uint32
	for _, vt := range vts {
		vn := make([]byte, 4)
		binary.LittleEndian.PutUint32(vn, vt)
		for _, r := range normRegions(p.Handle) {
			for _, o := range scanRegion(p, r, vn, 4) {
				if o < 0x40000000 || o&3 != 0 || seen[o] {
					continue
				}
				if s, ok := p.ReadU32(o + 4); !ok || s != 0 {
					continue
				}
				seen[o] = true
				insts = append(insts, o)
			}
		}
	}
	fmt.Printf("实例: %d\n", len(insts))
	for i, o := range insts {
		if i >= 20 {
			fmt.Printf("  ... %d more\n", len(insts)-i)
			break
		}
		// 谁引用它（静态槽）
		on := make([]byte, 4)
		binary.LittleEndian.PutUint32(on, o)
		var refs []uint32
		for _, r := range normRegions(p.Handle) {
			if r.Base >= 0x10000000 {
				continue // 只找静态区引用
			}
			refs = append(refs, scanRegion(p, r, on, 4)...)
		}
		refStr := ""
		for i2, rf := range refs {
			if i2 >= 4 {
				refStr += "..."
				break
			}
			refStr += fmt.Sprintf("0x%08X ", rf)
		}
		fmt.Printf("  inst=0x%08X 静态引用: %s\n", o, refStr)
	}
}

// cmdFields 从目标进程的 mono 元数据读取某类的字段表（权威名称+偏移）。
//
//	probe fields 0xVTABLE|TypeName
func cmdFields(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe fields 0xVTABLE|<TypeName>")
		return
	}
	arg := os.Args[2]
	var klass uint32
	if strings.HasPrefix(arg, "0x") || strings.HasPrefix(arg, "0X") {
		vt := uint32(parseHex(arg))
		k, ok := p.ReadU32(vt)
		if !ok || k < 0x10000 {
			fmt.Printf("bad vtable 0x%08X\n", vt)
			return
		}
		klass = k
	} else {
		klass = findKlassByName(p, arg)
		if klass == 0 {
			fmt.Printf("未找到类 %q\n", arg)
			return
		}
	}
	clsName := ""
	if np, ok := p.ReadU32(klass + 0x30); ok {
		clsName = readCStr(p, np)
	}
	fmt.Printf("klass 0x%08X (%q)\n", klass, clsName)

	type layout struct {
		stride, nameOff int
	}
	layouts := []layout{{12, 0}, {16, 0}, {12, 4}, {16, 4}, {12, 8}, {8, 0}, {8, 4}}
	best := 0
	for _, lay := range layouts {
		for off := uint32(0x30); off < 0x100; off += 4 {
			arr, ok := p.ReadU32(klass + off)
			if !ok || arr < 0x10000 || arr&3 != 0 {
				continue
			}
			n := 0
			vals := []string{}
			for i := 0; i < 6; i++ {
				ent := arr + uint32(i*lay.stride)
				np, ok := p.ReadU32(ent + uint32(lay.nameOff))
				if !ok || np < 0x10000 {
					break
				}
				s := readCStr(p, np)
				if !isIdent(s) {
					break
				}
				ofs, _ := p.ReadU32(ent + uint32(lay.stride-4))
				vals = append(vals, fmt.Sprintf("%s@0x%X", s, ofs))
				n++
			}
			if n >= 3 {
				fmt.Printf("  [layout stride=%d nameOff=%d] klass+0x%02X -> 0x%08X : %v\n",
					lay.stride, lay.nameOff, off, arr, vals)
				best++
			}
		}
	}
	if best == 0 {
		fmt.Println("  未识别字段表布局")
	}
}

func isIdent(s string) bool {
	if len(s) == 0 || len(s) > 96 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

func parseHex(s string) uint64 {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	n, _ := strconv.ParseUint(s, 16, 64)
	return n
}

// cmdBases 打印 CE 表所需的全部基址符号。
//
//	probe bases        人读格式
//	probe bases -xml   直接输出 <UserdefinedSymbols> 块，粘贴进 .ct 即可
//
// 用途: ctables/*.ct 的地址都写成 "符号+偏移"（如 level+24），
// 换会话时只要重新跑一次本命令、把符号值更新进去，整张表即可用。
// 这比在 CE 里手工重新找基址快得多，也保证表与修改器用同一套定位结果。
func cmdBases(p *core.Process) {
	asXML := false
	for _, a := range os.Args {
		if a == "-xml" {
			asXML = true
		}
	}
	prof := probeProfile()
	rt := &ori.Runtime{Prof: &prof}
	rt.SetProcess(p)
	if !rt.Refresh() {
		fmt.Println("定位失败（请先进入存档）")
		return
	}
	// 附属单例是后台异步定位的，等一会儿
	for i := 0; i < 12; i++ {
		if rt.DeathCounterAddr() != 0 && rt.GameWorldAddr() != 0 {
			break
		}
		time.Sleep(time.Second)
		rt.Refresh()
	}
	rd := func(a uint32) uint32 {
		if a == 0 {
			return 0
		}
		v, _ := p.ReadU32(a)
		return v
	}
	sein := rt.SeinCharacterAddr()
	mor := rd(sein + ori.OffSeinMortality)
	names := []string{
		"sein", "abilities", "level", "energy", "mortality", "health",
		"soulflame", "jump", "doublejump", "playerab", "death", "gw", "diffc",
	}
	vals := []uint32{
		sein,
		rd(sein + ori.OffSeinAbilities),
		rt.SeinLevelAddr(),
		rd(sein + ori.OffSeinEnergy),
		mor,
		rd(mor + ori.OffMortalityHealth),
		rt.SubAddr("soulflame"),
		rt.SubAddr("jump"),
		rt.SubAddr("doublejump"),
		rd(sein + ori.OffSeinPlayerAbil),
		rt.DeathCounterAddr(),
		rt.GameWorldAddr(),
		rt.SubAddr("diff"),
	}
	if asXML {
		fmt.Println("  <UserdefinedSymbols>")
		for i, n := range names {
			if vals[i] == 0 {
				continue
			}
			fmt.Printf("    <SymbolEntry>\n      <Name>%s</Name>\n      <Address>%08X</Address>\n    </SymbolEntry>\n", n, vals[i])
		}
		fmt.Println("  </UserdefinedSymbols>")
		return
	}
	for i, n := range names {
		note := ""
		if vals[i] == 0 {
			note = "   (未定位)"
		}
		fmt.Printf("%-11s = 0x%08X%s\n", n, vals[i], note)
	}
}

// cmdFeats 打印修改器的功能与键位表，并检测键位重复。
//
//	probe feats
func cmdFeats() {
	fs := ori.BuildFeatures()
	fmt.Printf("共 %d 项功能（含一命保护共 %d 项）:\n", len(fs), len(fs)+1)
	seen := map[string]bool{}
	dup := 0
	for _, f := range fs {
		label := f.HotkeyLabel()
		mark := ""
		if seen[label] {
			mark = "   <== 键位重复!"
			dup++
		}
		seen[label] = true
		fmt.Printf("  %-16s %s%s\n", label, f.Name, mark)
	}
	olLabel := ori.OneLifeHotkeyLabel()
	mark := ""
	if seen[olLabel] {
		mark = "   <== 键位重复!"
		dup++
	}
	seen[olLabel] = true
	fmt.Printf("  %-16s %s%s\n", olLabel, ori.OneLife().Name(), mark)
	if dup == 0 {
		fmt.Println("键位检查: 无重复 ✓")
	} else {
		fmt.Printf("键位检查: 发现 %d 处重复 ✗\n", dup)
	}
}

// cmdHeapFind 在堆区按类名枚举实例（不限 DifficultyController）。
//
//	probe heapfind <ClassName> [dumpWords]
//
// 用于确认某个单例对象是否真的存在于内存，以及它的字段现值。
func cmdHeapFind(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe heapfind <ClassName> [dumpWords]")
		return
	}
	target := os.Args[2]
	words := 8
	if len(os.Args) >= 4 {
		if n, err := strconv.Atoi(os.Args[3]); err == nil {
			words = n
		}
	}
	t0 := time.Now()
	n := 0
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+4 <= int(sz); off += 4 {
				obj := uint32(base) + uint32(off)
				if nm, _, ok := classOf(p, obj); !ok || nm != target {
					continue
				}
				n++
				fmt.Printf("  obj=0x%08X monitor=0x%08X :", obj, u32atb(buf, off+4))
				for w := 1; w <= words && off+(w+1)*4 <= int(sz); w++ {
					fmt.Printf(" [%02X]=0x%08X", (w+1)*4, u32atb(buf, off+(w+1)*4))
				}
				fmt.Println()
			}
		}
	}
	fmt.Printf("heapfind %q -> %d 个实例 [%.1fs]\n", target, n, time.Since(t0).Seconds())
}

// cmdDiffName 无过滤地列出所有"类名 == DifficultyController"的堆对象，
// 用于确认活体单例是否存在（不校验 monitor/字段值/委托）。
func cmdDiffName(p *core.Process) {
	t0 := time.Now()
	n := 0
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0x28 <= int(sz); off += 4 {
				obj := uint32(base) + uint32(off)
				if nm, _, ok := classOf(p, obj); !ok || nm != "DifficultyController" {
					continue
				}
				n++
				fmt.Printf("  obj=0x%08X  +4=0x%08X  +0x18(i32)=%d  +0x1C(i32)=%d  +0x20=0x%08X\n",
					obj, u32atb(buf, off+4),
					int32(u32atb(buf, off+0x18)), int32(u32atb(buf, off+0x1C)),
					u32atb(buf, off+0x20))
			}
		}
	}
	fmt.Printf("diffname 命中 %d 个 [%.1fs]\n", n, time.Since(t0).Seconds())
}

// cmdDiffStrict 严格签名扫描 DifficultyController 活体实例。
//
// 判据（全部满足）: classOf(obj)=="DifficultyController"、
// +0x18 Difficulty ∈ [0,3]、+0x1C LowestDifficulty ∈ [0,3]、
// +0x08 OnDifficultyChanged 非空（构造已完成）。
func cmdDiffStrict(p *core.Process) {
	t0 := time.Now()
	n := 0
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0x24 <= int(sz); off += 4 {
				d1 := int32(u32atb(buf, off+0x18))
				if d1 < 0 || d1 > 3 {
					continue
				}
				d2 := int32(u32atb(buf, off+0x1C))
				if d2 < 0 || d2 > 3 {
					continue
				}
				if u32atb(buf, off+8) == 0 {
					continue
				}
				obj := uint32(base) + uint32(off)
				cn, _, ok := classOf(p, obj)
				if !ok {
					continue
				}
				n++
				fmt.Printf("  ★ obj=0x%08X class=%q Difficulty=%d Lowest=%d\n", obj, cn, d1, d2)
			}
		}
	}
	fmt.Printf("diffstrict 命中 %d 个 [%.1fs]\n", n, time.Since(t0).Seconds())
}

// cmdRaceTest 复现"会话切换 + 引擎并发"时序，配合 -race 使用。
//
//	probe racetest
//
// 模拟 UI 反复重建功能表（BuildFeatures）的同时，另一协程持续 TickAll，
// 用于验证全局执行器列表的并发保护是否有效。
func cmdRaceTest(p *core.Process) {
	rt := &ori.Runtime{}
	rt.SetProcess(p)
	rt.Refresh()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			ori.TickAll(rt)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			rt.Refresh()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	for i := 0; i < 30; i++ {
		fs := ori.BuildFeatures()
		ori.SetActiveRuntime(rt)
		if len(fs) > 0 {
			ori.ActivateFeature(fs[0])
			ori.DeactivateFeature(fs[0], rt)
		}
		time.Sleep(30 * time.Millisecond)
	}
	close(done)
	time.Sleep(100 * time.Millisecond)
	fmt.Println("racetest 完成（若存在数据竞争，-race 会在上方输出报告）")
}

// cmdConIn 端到端自测控制台输入通路（与修改器 main.go 的实现保持一致）。
//
//	probe conin
//
// 步骤: 打开 CONIN$ → 设置输入模式 → 用 WriteConsoleInputW 注入合成按键
// （VK 0x41 按下+抬起）→ ReadConsoleInputW 读回并检查。全程不需要人工按键，
// 可确定性地验证句柄获取、输入模式与 INPUT_RECORD 结构布局。
func cmdConIn() {
	k := syscall.NewLazyDLL("kernel32.dll")
	getMode := k.NewProc("GetConsoleMode")
	setMode := k.NewProc("SetConsoleMode")
	createFile := k.NewProc("CreateFileW")
	readCI := k.NewProc("ReadConsoleInputW")
	writeCI := k.NewProc("WriteConsoleInputW")

	type ker struct {
		down   int32
		repeat uint16
		vk     uint16
		scan   uint16
		ch     uint16
		state  uint32
	}
	type rec struct {
		typ uint16
		_   uint16
		key ker
	}

	conin, _ := syscall.UTF16PtrFromString("CONIN$")
	h, _, _ := createFile.Call(uintptr(unsafe.Pointer(conin)),
		0xC0000000, 0x3, 0, 3, 0, 0)
	if h == 0 || h == ^uintptr(0) {
		fmt.Println("conin: 无法打开 CONIN$（本环境无控制台）→ 修改器会走全局热键兜底")
		return
	}
	var mode uint32
	if r, _, _ := getMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		fmt.Println("conin: CONIN$ 上 GetConsoleMode 失败")
		return
	}
	const want = 0x0001 | 0x0008 | 0x0040 | 0x0080
	setMode.Call(h, uintptr(want))

	recs := []rec{
		{1, 0, ker{1, 1, 0x41, 0x1E, 'A', 0}},
		{1, 0, ker{0, 1, 0x41, 0x1E, 'A', 0}},
	}
	var written uint32
	if r, _, _ := writeCI.Call(h, uintptr(unsafe.Pointer(&recs[0])),
		uintptr(len(recs)), uintptr(unsafe.Pointer(&written))); r == 0 {
		fmt.Println("conin: WriteConsoleInputW 失败")
		return
	}

	buf := make([]rec, 16)
	var n uint32
	if r, _, _ := readCI.Call(h, uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)), uintptr(unsafe.Pointer(&n))); r == 0 {
		fmt.Println("conin: ReadConsoleInputW 失败")
		return
	}
	seen := 0
	for i := 0; i < int(n); i++ {
		if buf[i].typ == 1 && buf[i].key.down != 0 && buf[i].key.vk == 0x41 {
			seen++
		}
	}
	if seen == 0 {
		fmt.Printf("conin: 失败 ✗  读回 %d 条记录，但未识别到 VK 0x41 按下事件\n", n)
		return
	}
	fmt.Printf("conin: 通过 ✓  注入 VK 0x41，读回 %d 条记录，识别到 %d 条按下事件\n", n, seen)

	// 关键: 验证"空缓冲时不阻塞"。ReadConsoleInputW 在无事件时会阻塞，
	// 修改器必须先查事件数再读，否则界面会卡死到下一次按键。
	getNum := k.NewProc("GetNumberOfConsoleInputEvents")
	var avail uint32
	getNum.Call(h, uintptr(unsafe.Pointer(&avail)))
	start := time.Now()
	if avail > 0 {
		var got uint32
		readCI.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&got)))
	}
	el := time.Since(start)
	if avail == 0 {
		fmt.Printf("conin: 通过 ✓  排空后事件数为 0，先查计数再读 → 立即返回（耗时 %v），不会阻塞界面\n", el)
	} else {
		fmt.Printf("conin: 注意  排空后仍有 %d 条事件（本环境输入缓冲非空）\n", avail)
	}
}

// cmdCoreReg 打印修改器 core.ReadableRegions() 实际枚举到的区域，
// 并可查询指定地址是否被覆盖（诊断"扫描漏段"问题）。
//
//	probe corereg [0xADDR ...]
func cmdCoreReg(p *core.Process) {
	regs := p.ReadableRegions()
	var total uint64
	for _, r := range regs {
		total += uint64(r.Size)
	}
	fmt.Printf("core.ReadableRegions: %d 段, 合计 %.1f MB\n", len(regs), float64(total)/(1<<20))
	for _, a := range os.Args[2:] {
		addr := uint32(parseHex(a))
		hit := false
		for _, r := range regs {
			if addr >= r.Base && addr < r.Base+r.Size {
				fmt.Printf("  0x%08X 在 base=0x%08X size=0x%X 段内\n", addr, r.Base, r.Size)
				hit = true
			}
		}
		if !hit {
			fmt.Printf("  0x%08X **未被任何段覆盖**\n", addr)
		}
	}
	fmt.Printf("--- 低区段（<0x10000000）---\n")
	n := 0
	var lowTotal uint64
	for _, r := range regs {
		if r.Base >= 0x10000000 {
			continue
		}
		lowTotal += uint64(r.Size)
		n++
		if n <= 25 {
			fmt.Printf("  0x%08X size=0x%-8X\n", r.Base, r.Size)
		}
	}
	fmt.Printf("  低区共 %d 段, 合计 %.1f MB\n", n, float64(lowTotal)/(1<<20))
}

// cmdStaticBlock 在指定类的 MonoClass 结构内寻找静态数据块，用于定位
// 静态字段（如 Characters.Sein）。
//
//	probe staticblock <ClassName> <hexTargetValue>
//
// 方法: 先按类名解析 klass，再在 klass 的前后内存与它包含的指针目标中
// 搜索等于 target 的槽位。找到的槽位就是该静态字段的真实存储地址。
func cmdStaticBlock(p *core.Process) {
	if len(os.Args) < 4 {
		fmt.Println("usage: probe staticblock <ClassName> <hexValue>")
		return
	}
	name := os.Args[2]
	target := uint32(parseHex(os.Args[3]))
	t0 := time.Now()
	k := findKlassByName(p, name)
	if k == 0 {
		fmt.Printf("未找到类 %q [%.1fs]\n", name, time.Since(t0).Seconds())
		return
	}
	fmt.Printf("klass=0x%08X (%s) [%.1fs]\n", k, name, time.Since(t0).Seconds())

	// ① klass 内部直接槽 / 一级间接（窗口放宽到 0x2000，静态块可能在 vtable 之后）
	for off := uint32(0); off < 0x400; off += 4 {
		if v, ok := p.ReadU32(k + off); ok && v == target {
			fmt.Printf("  ★ klass+0x%03X 直接存放目标\n", off)
		}
		w, ok := p.ReadU32(k + off)
		if !ok || w < 0x10000 || w >= 0x80000000 || w&3 != 0 {
			continue
		}
		for o2 := uint32(0); o2 < 0x2000; o2 += 4 {
			if v, ok := p.ReadU32(w + o2); ok && v == target {
				fmt.Printf("  ★ klass+0x%03X -> 0x%08X + 0x%03X = 目标 (槽 0x%08X)\n", off, w, o2, w+o2)
			}
		}
	}
	// ② klass 前后裸内存（静态块可能紧邻 klass）
	for _, base := range []uint32{k - 0x2000, k + 0x1000} {
		for off := uint32(0); off < 0x4000; off += 4 {
			if v, ok := p.ReadU32(base + off); ok && v == target {
				fmt.Printf("  ★ 0x%08X (klass%+d) 存放目标\n", base+off, int32(base+off-k))
			}
		}
	}
	// ③ 查 MonoVTable: 全地址空间搜索 dword == klass 的槽位，
	//    再在其后窗口内找目标（静态字段数据内联在 vtable 之后）。
	needle := make([]byte, 4)
	binary.LittleEndian.PutUint32(needle, k)
	vtHits := 0
	for _, r := range normRegions(p.Handle) {
		for _, at := range scanRegion(p, r, needle, 4) {
			vtHits++
			if vtHits > 64 {
				break
			}
			for o2 := uint32(0); o2 < 0x2000; o2 += 4 {
				if v, ok := p.ReadU32(at + o2); ok && v == target {
					fmt.Printf("  ★ vtable候选 0x%08X + 0x%03X = 目标\n", at, o2)
				}
			}
		}
	}
	fmt.Printf("  (vtable 候选命中 %d 处)\n", vtHits)
	fmt.Printf("staticblock 完成 [%.1fs]\n", time.Since(t0).Seconds())
}

// cmdAllRefs 全地址带查找指向指定地址/值的槽位（不限低区）。
//
//	probe allrefs 0xADDR [maxShow]
func cmdAllRefs(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe allrefs 0xADDR [maxShow]")
		return
	}
	target := uint32(parseHex(os.Args[2]))
	maxShow := 40
	if len(os.Args) >= 4 {
		maxShow, _ = strconv.Atoi(os.Args[3])
	}
	needle := make([]byte, 4)
	binary.LittleEndian.PutUint32(needle, target)
	n := 0
	for _, r := range normRegions(p.Handle) {
		for _, a := range scanRegion(p, r, needle, 4) {
			n++
			if n > maxShow {
				continue
			}
			fmt.Printf("  0x%08X  (region base 0x%08X type 0x%X)\n", a, r.Base, r.Type)
		}
	}
	fmt.Printf("allrefs 0x%08X -> %d 处\n", target, n)
}

// cmdOneScan 按原始数值模式定位"一命难度"的 DifficultyController，
// 不依赖类名解析（用于排查 classOf 失效的场景）。
//
//	probe onescan [lowest]
//
// 判据: 对象位于堆带、[0] 是合法 vtable、+0x1C == lowest（默认 3=OneLife）、
// +0x18 ∈ [0,3]、+0x20 是堆指针（OnDifficultyChanged 委托）。
func cmdOneScan(p *core.Process) {
	lowest := int32(3)
	if len(os.Args) >= 3 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			lowest = int32(n)
		}
	}
	t0 := time.Now()
	n := 0
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0x24 <= int(sz); off += 4 {
				// 快筛: +0x1C == lowest
				if int32(u32atb(buf, off+0x1C)) != lowest {
					continue
				}
				d1 := int32(u32atb(buf, off+0x18))
				if d1 < 0 || d1 > 3 {
					continue
				}
				del := u32atb(buf, off+0x20)
				if del < 0x40000000 || del >= 0xFFFF0000 {
					continue
				}
				obj := uint32(base) + uint32(off)
				// [0] 必须是合法 vtable
				vt := u32atb(buf, off)
				if vt < 0x08000000 || vt >= 0xFFFF0000 || vt&3 != 0 {
					continue
				}
				kn, ok := p.ReadU32(vt)
				if !ok || kn < 0x08000000 || kn == vt {
					continue
				}
				np, _ := p.ReadU32(kn + 0x30)
				cn := readCStr(p, np)
				n++
				fmt.Printf("  ★ obj=0x%08X Difficulty=%d Lowest=%d delegate=0x%08X class=%q\n",
					obj, d1, lowest, del, cn)
				// 输出该对象前后 8 字节（确认对象边界）
				for j := -2; j < 0; j++ {
					off2 := off + j*4
					if off2 >= 0 {
						fmt.Printf("       [0x%08X] = 0x%08X\n", obj+uint32(j*4), u32atb(buf, off2))
					}
				}
			}
		}
	}
	fmt.Printf("onescan(lowest=%d) 命中 %d 个 [%.1fs]\n", lowest, n, time.Since(t0).Seconds())
}

// cmdFeat 端到端功能测试: 直接对活体进程运行真功能引擎。
//
//	probe feat <digit> [seconds] [-ctrl]
//
// 流程: 解析 -> 激活指定功能 -> 连续 Tick -> 打印状态与目标内存原值 ->
// 关闭功能（还原）-> 打印还原后原值。全部写入可逆，安全。
func cmdFeat(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe feat <digit> [seconds] [-ctrl]")
		return
	}
	digit, _ := strconv.Atoi(os.Args[2])
	secs := 3
	useCtrl := false
	useShift := false
	for _, a := range os.Args[3:] {
		if a == "-ctrl" {
			useCtrl = true
			continue
		}
		if a == "-shift" {
			useShift = true
			continue
		}
		if n, err := strconv.Atoi(a); err == nil {
			secs = n
		}
	}
	prof := probeProfile()
	rt := &ori.Runtime{Prof: &prof}
	rt.SetProcess(p)
	if !rt.Refresh() {
		fmt.Println("解析失败")
		return
	}
	fmt.Printf("活体 SeinCharacter=0x%08X Level=0x%08X Jump=0x%08X DoubleJump=0x%08X SoulFlame=0x%08X Death=0x%08X\n",
		rt.SeinCharacterAddr(), rt.SeinLevelAddr(), rt.SubAddr("jump"), rt.SubAddr("doublejump"),
		rt.SubAddr("soulflame"), rt.DeathCounterAddr())
	// 等待后台单例定位（死亡计数/难度控制）
	if rt.DeathCounterAddr() == 0 {
		for i := 0; i < 10; i++ {
			time.Sleep(time.Second)
			rt.Refresh()
			if rt.DeathCounterAddr() != 0 {
				break
			}
		}
		fmt.Printf("等待后 Death=0x%08X Diff=0x%08X\n", rt.DeathCounterAddr(), rt.SubAddr("diff"))
	}

	feats := ori.BuildFeatures()
	var target *ori.Feature
	for _, f := range feats {
		if f.Digit == digit && f.NeedCtrl == useCtrl && f.NeedShift == useShift {
			target = f
			break
		}
	}
	if target == nil {
		mod := ""
		if useCtrl {
			mod = "Ctrl+"
		}
		if useShift {
			mod = "Ctrl+Shift+"
		}
		fmt.Printf("未找到 %s小键盘 %d 对应的功能\n", mod, digit)
		return
	}
	fmt.Printf("目标功能: [%s] %s\n", target.HotkeyLabel(), target.Name)

	// 记录测试前相关字段值（用于还原）
	before := captureValues(rt)

	ori.ActivateFeature(target)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		ori.TickAll(rt)
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Printf("激活后状态: %s\n", target.Status())

	ori.DeactivateFeature(target, rt)
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("关闭后状态: %s\n", target.Status())

	snap := rt.Read()
	fmt.Printf("快照: SP=%d EXP=%d energy=%.2f/%.2f hp=%.2f/%d deaths=%d\n",
		snap.SkillPoints, snap.Experience, snap.EnergyCur, snap.EnergyMax,
		snap.HealthCur, snap.HealthMax, snap.Deaths)
	_ = before
}

// captureValues 记录测试前会被功能改动的字段（诊断打印用）。
func captureValues(rt *ori.Runtime) string {
	s := rt.Read()
	return fmt.Sprintf("SP=%d EXP=%d", s.SkillPoints, s.Experience)
}

// cmdDiffObj 全堆扫描 DifficultyController 活体实例:
// 类名匹配 + Difficulty(+0x18)/LowestDifficulty(+0x1C) ∈ [0,3]。
// 用于判断当前是否存在可用的难度控制器（主菜单下通常尚未创建）。
func cmdDiffObj(p *core.Process) {
	t0 := time.Now()
	n := 0
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0x24 <= int(sz); off += 4 {
				obj := uint32(base) + uint32(off)
				if nm, _, ok := classOf(p, obj); !ok || nm != "DifficultyController" {
					continue
				}
				if m := u32atb(buf, off+4); m != 0 {
					continue
				}
				d1 := int32(u32atb(buf, off+0x18))
				d2 := int32(u32atb(buf, off+0x1C))
				if d1 < 0 || d1 > 3 || d2 < 0 || d2 > 3 {
					continue
				}
				n++
				del := u32atb(buf, off+0x20)
				fmt.Printf("  ★ 0x%08X Difficulty=%d Lowest=%d delegate=0x%08X\n", obj, d1, d2, del)
			}
		}
	}
	fmt.Printf("diffobj 命中 %d 个活体 DifficultyController [%.1fs]\n", n, time.Since(t0).Seconds())
}

// cmdDiffScan2 用"UberDelegate 委托对象反查"定位活体 DifficultyController。
//
// 原理: DifficultyController.OnDifficultyChanged 是 UberDelegate 实例，
// 其内部 List<Action> 的每个 Action.Target 都指向该 DifficultyController。
// 步骤: 找所有 UberDelegate 实例 -> 取其 List<Action> -> 取 Action.Target
// -> 校验 Target 的类名是 DifficultyController，且 Difficulty/Lowest ∈ [0,3]。
func cmdDiffScan2(p *core.Process) {
	t0 := time.Now()
	kt := findKlassByName(p, "DifficultyController")
	if kt == 0 {
		fmt.Println("未找到 DifficultyController 类")
		return
	}
	kUber := findKlassByName(p, "UberDelegate")
	if kUber == 0 {
		fmt.Println("未找到 UberDelegate 类")
		return
	}
	fmt.Printf("klass DifficultyController=0x%08X UberDelegate=0x%08X [%.2fs]\n", kt, kUber, time.Since(t0).Seconds())

	// ① 找 UberDelegate 实例（对象[0]解析出的 klass == kUber）
	var ubers []uint32
	for _, r := range normRegions(p.Handle) {
		if r.Base < 0x40000000 {
			continue
		}
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+4 <= int(sz); off += 4 {
				obj := uint32(base) + uint32(off)
				if _, k, ok := classOf(p, obj); ok && k == kUber {
					if m, ok2 := p.ReadU32(obj + 4); ok2 && m == 0 {
						ubers = append(ubers, obj)
					}
				}
			}
		}
	}
	fmt.Printf("UberDelegate 实例: %d [%.2fs]\n", len(ubers), time.Since(t0).Seconds())

	targets := map[uint32]bool{}
	for _, u := range ubers {
		// UberDelegate: +0x08 = List<Action> m_registers
		lst, ok := p.ReadU32(u + 0x08)
		if !ok || lst < 0x40000000 {
			continue
		}
		// List<T>: +0x08 = T[] items, +0x0C = size
		items, ok := p.ReadU32(lst + 0x08)
		size, ok2 := p.ReadU32(lst + 0x0C)
		if !ok || !ok2 || items < 0x40000000 || size == 0 || size > 64 {
			continue
		}
		for i := uint32(0); i < size; i++ {
			// Action: +0x08 = Target, +0x0C = Method
			act, ok := p.ReadU32(items + 0x10 + i*4)
			if !ok || act < 0x40000000 {
				continue
			}
			tgt, ok := p.ReadU32(act + 0x08)
			if !ok || tgt < 0x40000000 {
				continue
			}
			targets[tgt] = true
		}
	}
	fmt.Printf("Action.Target 候选: %d\n", len(targets))
	for tgt := range targets {
		nm, k, ok := classOf(p, tgt)
		if !ok {
			continue
		}
		if k != kt && nm != "DifficultyController" {
			continue
		}
		d, _ := p.ReadI32(tgt + 0x18)
		l, _ := p.ReadI32(tgt + 0x1C)
		del, _ := p.ReadU32(tgt + 0x20)
		fmt.Printf("  ★ DifficultyController 0x%08X Difficulty=%d Lowest=%d delegate=0x%08X\n",
			tgt, d, l, del)
	}
	fmt.Printf("完成 [%.2fs]\n", time.Since(t0).Seconds())
}

// cmdFindAll 全地址带扫描某类的所有实例（带 vtable 名称缓存）。
//
//	probe findall <ClassName> [dumpDwords]
func cmdFindAll(p *core.Process) {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe findall <ClassName> [dumpDwords]")
		return
	}
	target := os.Args[2]
	dump := 6
	if len(os.Args) >= 4 {
		dump, _ = strconv.Atoi(os.Args[3])
	}
	t0 := time.Now()
	type hit struct{ obj, vt uint32 }
	var hits []hit
	nameCache := map[uint32]string{}
	seen := map[uint32]bool{}
	for _, r := range normRegions(p.Handle) {
		const chunk = 8 << 20
		for base := r.Base; base < r.Base+r.Size; base += chunk {
			sz := uint64(chunk)
			if r.Base+r.Size-base < sz {
				sz = r.Base + r.Size - base
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+8 <= int(sz); off += 4 {
				vt := u32atb(buf, off)
				if vt < 0x08000000 || vt >= 0xFFFF0000 || vt&3 != 0 {
					continue
				}
				obj := uint32(base) + uint32(off)
				nm, ok := nameCache[vt]
				if !ok {
					nm = ""
					if s, _, ok2 := classOf(p, obj); ok2 {
						nm = s
					}
					nameCache[vt] = nm
				}
				if nm != target || seen[obj] {
					continue
				}
				seen[obj] = true
				hits = append(hits, hit{obj, vt})
			}
		}
	}
	fmt.Printf("findall %q -> %d 实例 [%.2fs]\n", target, len(hits), time.Since(t0).Seconds())
	for i, h := range hits {
		if i >= 12 {
			fmt.Printf("  ... %d more\n", len(hits)-i)
			break
		}
		fmt.Printf("== 0x%08X (vt 0x%08X)\n", h.obj, h.vt)
		for j := 0; j < dump; j++ {
			off := uint32(j * 4)
			v, ok := p.ReadU32(h.obj + off)
			if !ok {
				break
			}
			note := fmt.Sprintf("i32=%-12d f32=%-13.5g", int32(v), math.Float32frombits(v))
			if cn := className(p, v); cn != "" {
				note += "  -> " + cn
			}
			fmt.Printf("   +0x%02X %08X %s\n", off, v, note)
		}
	}
}

// cmdRawRegions 转储原始 64 位 VQEx 视图区域（诊断 WoW64 双地址带）。
func cmdRawRegions(p *core.Process) {
	var addr uint64
	var m mbi
	sz := uintptr(unsafe.Sizeof(m))
	n := 0
	for {
		r1, _, _ := procVirtualQueryEx.Call(p.Handle, uintptr(addr), uintptr(unsafe.Pointer(&m)), sz)
		if r1 == 0 {
			break
		}
		if m.State == memCommit && m.RegionSize >= 0x10000 {
			fmt.Printf("  raw=0x%016X size=0x%08X prot=0x%02X type=0x%X lo32=0x%08X\n",
				uint64(m.BaseAddress), uint64(m.RegionSize), m.Protect, m.Type, uint64(m.BaseAddress)&0xFFFFFFFF)
			n++
			if n >= 60 {
				fmt.Println("  ...")
				break
			}
		}
		next := uint64(m.BaseAddress) + uint64(m.RegionSize)
		if next <= addr {
			break
		}
		addr = next
		if addr >= 0x800000000000 {
			break
		}
	}
}
