// OriTrainer (Go) — 奥日与迷失森林 双版本修改器（原版 + 终极版）。
// 纯标准库实现: Windows console ANSI TUI。
//
// 按键模型（两条规则，互不干扰）:
//  1. 界面按键（↑↓/回车/ESC/F1/HOME/END）读**控制台输入事件**。控制台
//     输入缓冲只在本窗口拥有键盘焦点时才会收到事件，因此天然只在
//     修改器前台时响应，无需猜测窗口归属。
//  2. 功能热键（小键盘 1-9/0、Ctrl+小键盘）用 GetAsyncKeyState 全局轮询，
//     只要修改器进程在运行就生效，前后台无关。
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"oritrainer/core"
	"oritrainer/ori"
)

// ---------- console ----------

var (
	k32       = syscall.NewLazyDLL("kernel32.dll")
	pStdout   = k32.NewProc("GetStdHandle")
	pWriteW   = k32.NewProc("WriteConsoleW")
	pGetMode  = k32.NewProc("GetConsoleMode")
	pSetMode  = k32.NewProc("SetConsoleMode")
	pSetCur   = k32.NewProc("SetConsoleCursorPosition")
	pSetTitle = k32.NewProc("SetConsoleTitleW")
	pReadCI   = k32.NewProc("ReadConsoleInputW")
	pCreateW  = k32.NewProc("CreateFileW")
	pNumCI    = k32.NewProc("GetNumberOfConsoleInputEvents")
)

const stdOutputHandle = ^uintptr(10) // STD_OUTPUT_HANDLE = (DWORD)-11

var stdoutHandle uintptr

func init() {
	h, _, _ := pStdout.Call(^uintptr(10))
	stdoutHandle = h
	var mode uint32
	pGetMode.Call(stdoutHandle, uintptr(unsafe.Pointer(&mode)))
	pSetMode.Call(stdoutHandle, uintptr(mode|0x0004))
	// 启动即设置标题，便于在任务栏/终端标签里识别。
	if t, err := syscall.UTF16PtrFromString("OriTrainer — 版本选择"); err == nil {
		pSetTitle.Call(uintptr(unsafe.Pointer(t)))
	}
}

// ---------- 控制台输入（界面按键的唯一来源） ----------

// INPUT_RECORD / KEY_EVENT_RECORD（x86/x64 同布局，20 字节）。
type keyEventRecord struct {
	KeyDown int32
	Repeat  uint16
	VK      uint16
	Scan    uint16
	Char    uint16
	State   uint32
}

type inputRecord struct {
	Type uint16
	_    uint16
	Key  keyEventRecord
}

const inputRecordKeyEvent = 1

// consoleKeys 每轮从控制台输入缓冲读取按键事件。
//
// 控制台只在拥有键盘焦点时才会收到事件，所以这里读到的按键天然
// 等价于"修改器窗口在前台"——比窗口标题/父进程链等启发式可靠得多
// （Windows Terminal 默认终端下 GetConsoleWindow 返回隐藏的 ConPTY
// 窗口、父链也不经过终端进程，那些启发式曾导致界面按键全部失灵）。
type consoleKeys struct {
	handle uintptr
	recs   []inputRecord
	press  map[int]bool // 本轮采样到的按下事件
	ok     bool
}

const (
	// SetConsoleMode 的目标掩码（显式设定而非叠加，避免宿主遗留的
	// ENABLE_VIRTUAL_TERMINAL_INPUT 等标志影响按键上报）:
	// 处理输入（Ctrl+C 语义）+ 窗口输入（非字符键如 F1/方向键）。
	ciEnableProcessedInput = 0x0001
	ciEnableWindowInput    = 0x0008
	ciEnableExtendedFlags  = 0x0080
)

func newConsoleKeys() *consoleKeys {
	c := &consoleKeys{
		recs:  make([]inputRecord, 32),
		press: make(map[int]bool),
	}
	// 优先直接打开控制台输入缓冲 CONIN$: 即使标准输入被重定向
	// （某些启动器/脚本会这么做），界面按键也照常工作。
	if t, err := syscall.UTF16PtrFromString("CONIN$"); err == nil {
		h, _, _ := pCreateW.Call(uintptr(unsafe.Pointer(t)),
			0xC0000000 /*GENERIC_READ|GENERIC_WRITE*/, 0x3 /*共享读写*/, 0, 3 /*OPEN_EXISTING*/, 0, 0)
		if h != 0 && h != ^uintptr(0) {
			c.handle = h
		}
	}
	if c.handle == 0 {
		h, _, _ := pStdout.Call(^uintptr(10)) // STD_INPUT_HANDLE = (DWORD)-10
		if h == 0 || h == ^uintptr(0) {
			return c
		}
		c.handle = h
	}
	var mode uint32
	if r, _, _ := pGetMode.Call(c.handle, uintptr(unsafe.Pointer(&mode))); r == 0 {
		c.handle = 0 // 不是控制台输入缓冲（被重定向且无控制台）
		return c
	}
	// 关闭行输入/回显（没人按行读，避免缓冲被行编辑器吃掉）；保留处理输入
	// （Ctrl+C 语义不变）；打开窗口输入（否则收不到 F1/方向键等非字符键）。
	//
	// 不启用 QuickEdit: 鼠标拖选会让后续 WriteConsole 阻塞、界面卡死
	// （本程序持续重绘，拖选极易触发）。代价是不能用鼠标选择复制文本。
	const want = ciEnableProcessedInput | ciEnableWindowInput | ciEnableExtendedFlags
	pSetMode.Call(c.handle, uintptr(want))
	c.ok = true
	return c
}

// poll 读取并清空本轮按键事件。只记录按下事件；
// 长按产生的重复事件也只是持续置位，边沿检测仍只触发一次。
//
// 必须先查事件数: ReadConsoleInputW 在缓冲为空时会**阻塞**，直接调用会
// 让整个界面卡死在读取上直到有按键（严重且不易察觉）。
func (c *consoleKeys) poll() {
	for k := range c.press {
		delete(c.press, k)
	}
	if c.handle == 0 {
		return
	}
	var avail uint32
	if r, _, _ := pNumCI.Call(c.handle, uintptr(unsafe.Pointer(&avail))); r == 0 || avail == 0 {
		return
	}
	var read uint32
	r1, _, _ := pReadCI.Call(c.handle, uintptr(unsafe.Pointer(&c.recs[0])),
		uintptr(len(c.recs)), uintptr(unsafe.Pointer(&read)))
	if r1 == 0 {
		return
	}
	for i := 0; i < int(read); i++ {
		rec := &c.recs[i]
		if rec.Type != inputRecordKeyEvent {
			continue
		}
		if rec.Key.KeyDown != 0 {
			c.press[int(rec.Key.VK)] = true
		}
	}
}

// available 控制台输入是否可用（不可用时界面按键退化为全局热键 + 游戏前台抑制）。
func (c *consoleKeys) available() bool { return c.ok }

// pressed 本轮是否收到该虚拟键的按下事件。
func (c *consoleKeys) pressed(vk int) bool { return c.press[vk] }

func writeConsole(s string) {
	u16 := utf16.Encode([]rune(s))
	if len(u16) == 0 {
		return
	}
	var written uintptr
	pWriteW.Call(stdoutHandle, uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)), uintptr(unsafe.Pointer(&written)))
}

type coord struct{ X, Y int16 }

func homeCursor() {
	c := coord{0, 0}
	pSetCur.Call(stdoutHandle, uintptr(unsafe.Pointer(&c)))
}

// ---------- 颜色 ----------

const (
	cReset = "\x1b[0m"
	cDim   = "\x1b[90m"
	cWhite = "\x1b[97m"
	cGreen = "\x1b[92m"
	cRed   = "\x1b[91m"
	cYel   = "\x1b[93m"
	cTitle = "\x1b[1;38;5;208m"
	cBox   = "\x1b[38;5;240m"
	cCyan  = "\x1b[96m"
)

// ---------- 按键（边沿检测） ----------

// keyEdges 把"当前是否按下"折叠成"这一轮刚按下"。
type keyEdges struct {
	prev  map[int]bool
	state func(int) bool
}

func newKeyEdges(state func(int) bool) *keyEdges {
	return &keyEdges{prev: make(map[int]bool), state: state}
}

func (e *keyEdges) pressed(vk int) bool {
	down := e.state(vk)
	was := e.prev[vk]
	e.prev[vk] = down
	return down && !was
}

// ---------- 会话状态 ----------

type session struct {
	prof  ori.Profile
	r     *ori.Runtime
	feats []*ori.Feature

	// procMu 保护 proc: 看护协程与主循环（F12/ESC/END）都会改写它。
	procMu sync.Mutex
	proc   *core.Process
}

func (s *session) getProc() *core.Process {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	return s.proc
}

func (s *session) setProc(p *core.Process) {
	s.procMu.Lock()
	s.proc = p
	s.procMu.Unlock()
}

// closeProc 关闭并清空当前进程句柄（可安全重复调用）。
func (s *session) closeProc() {
	s.procMu.Lock()
	p := s.proc
	s.proc = nil
	s.procMu.Unlock()
	if p != nil {
		p.Close()
	}
}

type app struct {
	mu        sync.Mutex
	sess      *session
	selCursor int // 版本选择光标
	navCursor int // 修改器界面：当前选中的功能项（前台导航用）
	chosen    bool
	msg       atomic.Value
	help      atomic.Bool
}

// navEntry 一个导航项（功能项或一命保护）。
type navEntry struct {
	feat   *ori.Feature // nil 表示一命保护
	label  string
	name   string
	active bool
	status string
}

// navList 按显示顺序构建导航项：
// 小键盘 1-9/0 各功能 → 一命保护（Ctrl+小键盘 1）→ 其余 Ctrl+小键盘 功能。
func navList(s *session) []navEntry {
	if s == nil {
		return nil
	}
	ol := ori.OneLife()
	olEntry := navEntry{
		label:  fmt.Sprintf("Ctrl+小键盘 %d", ori.CtrlOneLifeDigit),
		name:   ol.Name(),
		active: ol.Active(),
		status: ol.Status(),
	}
	var out []navEntry
	for _, f := range s.feats {
		// 一命保护插在第一个 Ctrl 项之前（即小键盘组之后）
		if f.NeedCtrl && olEntry.feat == nil && !hasOneLife(out) {
			out = append(out, olEntry)
		}
		out = append(out, navEntry{
			feat: f, label: f.HotkeyLabel(), name: f.Name,
			active: f.Active(), status: f.Status(),
		})
	}
	if !hasOneLife(out) {
		out = append(out, olEntry)
	}
	return out
}

func hasOneLife(list []navEntry) bool {
	for _, e := range list {
		if e.feat == nil {
			return true
		}
	}
	return false
}

// navToggle 切换第 idx 个导航项。
func (a *app) navToggle(s *session, idx int) {
	list := navList(s)
	if idx < 0 || idx >= len(list) {
		return
	}
	e := list[idx]
	if e.feat == nil {
		toggleOneLife(a)
		return
	}
	toggleFeature(a, s, e.feat)
}

// toggleOneLife 切换一命保护。
func toggleOneLife(a *app) {
	ol := ori.OneLife()
	on := !ol.Active()
	ol.SetActive(on)
	if on {
		a.setMsg("已激活: 一命保护 — 死亡如普通模式在检查点复活，成就资格保留")
	} else {
		a.setMsg("已关闭: 一命保护")
	}
}

// toggleFeature 切换单个功能（含可还原型的状态回滚）。
func toggleFeature(a *app, s *session, f *ori.Feature) {
	if f.Active() {
		ori.DeactivateFeature(f, s.r)
		a.setMsg(fmt.Sprintf("已关闭: %s", f.Name))
		return
	}
	ori.ActivateFeature(f)
	if f.FixedTarget != nil {
		a.setMsg(fmt.Sprintf("已激活: %s — 锁定为 %d", f.Name, *f.FixedTarget))
	} else {
		a.setMsg(fmt.Sprintf("已激活: %s", f.Name))
	}
}

func (a *app) setMsg(s string) { a.msg.Store(s) }
func (a *app) getMsg() string  { s, _ := a.msg.Load().(string); return s }

func (a *app) current() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sess
}

func (a *app) choose(prof ori.Profile) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := &session{prof: prof, r: &ori.Runtime{}, feats: ori.BuildFeatures()}
	ori.SetActiveRuntime(s.r) // 供经验倍率单选互斥还原数值
	a.sess = s
	a.chosen = true
	if p, err := core.Attach(prof.ProcessName); err == nil {
		s.setProc(p)
		s.r.SetProcess(p)
		a.msg.Store(fmt.Sprintf("已附加 %s (PID %d)。", prof.ProcessName, p.Pid))
	} else {
		a.msg.Store(fmt.Sprintf("游戏 %s 未运行——启动游戏后自动附加。", prof.ProcessName))
	}
	if t, err := syscall.UTF16PtrFromString("OriTrainer — " + prof.DisplayName); err == nil {
		pSetTitle.Call(uintptr(unsafe.Pointer(t)))
	}
	// 启动会话协程: 附加看护/堆扫描 + 功能引擎
	go a.sessionWatch(s)
	go a.engineLoop(s)
}

// ---------- 版本选择界面 ----------

func renderSelector(a *app) {
	var b strings.Builder
	b.WriteString("\x1b[2J")
	homeCursor()

	b.WriteString(cTitle + "  OriTrainer — 奥日与迷失森林 双版本修改器\n\n" + cReset)
	b.WriteString(cWhite + "  请选择游戏版本（↑↓/WS 选择，回车确认）:\n\n" + cReset)

	profiles := []ori.Profile{ori.Profiles[ori.Vanilla], ori.Profiles[ori.Definitive]}
	for i, prof := range profiles {
		cursor := "   "
		color := cDim
		if i == a.selCursor {
			cursor = cCyan + " ▶ " + cReset
			color = cWhite
		}
		b.WriteString(cursor + color + prof.DisplayName + cReset + "\n")
	}

	b.WriteString("\n" + cBox + "  ────────────────────────────────────────────────────────\n" + cReset)
	b.WriteString("  " + cWhite + "↑/↓ 或 W/S" + cReset + " 选择   " + cWhite + "回车" + cReset + " 进入   " + cWhite + "ESC" + cReset + " 退出\n")
	b.WriteString("  " + cDim + a.getMsg() + cReset + "\n")

	writeConsole(b.String())
}

// ---------- 修改器界面 ----------

func renderTrainer(a *app, s *session) {
	var b strings.Builder
	b.WriteString("\x1b[2J")
	homeCursor()

	b.WriteString(cTitle + "  OriTrainer v2.0 — " + s.prof.DisplayName + " 修改器\n" + cReset)
	b.WriteString(cDim + "  [ESC] 返回版本选择\n" + cReset)
	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)

	snap := s.r.Read()
	if !snap.Attached {
		b.WriteString("  " + cRed + fmt.Sprintf("○ 未检测到 %s — 请先启动游戏（每 2 秒自动附加）", s.prof.ProcessName) + cReset + "\n")
	} else {
		st := fmt.Sprintf("  %s● 已附加 %s (PID %d)%s   ", cGreen, s.prof.ProcessName, snap.Pid, cReset)
		if snap.SeinOK {
			st += cGreen + fmt.Sprintf("Sein 已定位 (%08X)", snap.SeinLevel) + cReset
		} else {
			st += cYel + "Sein 扫描中…（进入存档后可定位）" + cReset
		}
		b.WriteString(st + "\n")
		if snap.SeinOK {
			b.WriteString(fmt.Sprintf("  实时: 生命 %g/%d 球   能量 %.1f/%.1f   技能点 %d   死亡 %d\n",
				snap.HealthCells, snap.HealthMaxCells, snap.EnergyCur, snap.EnergyMax,
				snap.SkillPoints, snap.Deaths))
		}
	}

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)

	if a.help.Load() {
		b.WriteString(cYel + "  [帮助] 冻结=激活时捕获当前值并持续写回; 进入存档后生效; 全局热键免切窗\n" + cReset)
	}

	// 导航光标始终显示: 界面按键来自控制台输入，仅在本窗口有焦点时才生效，
	// 因此无法（也无需）提前判断焦点状态。
	rows := navList(s)
	for i, e := range rows {
		cursor := "  "
		if a.navCursor == i {
			cursor = cCyan + "▶" + cReset + " "
		}
		box := cWhite + "[ ]" + cReset
		if e.active {
			box = cGreen + "[x]" + cReset
		}
		// 热键标签固定宽度（"Ctrl+小键盘 N" = 12 个显示字符）
		b.WriteString(fmt.Sprintf("  %s%s  %s%-12s%s %s   %s%s%s\n",
			cursor, box, cWhite, e.label, cReset, e.name,
			statusColor(e.status, e.active), e.status, cReset))
	}
	if a.navCursor >= len(rows) {
		a.navCursor = 0
	}

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)
	b.WriteString("  " + cDim + "本窗口内: ↑↓ 选择 · 回车/空格 开关   |   " + cReset +
		cWhite + "小键盘 1-9/0" + cReset + " 前 10 项   " + cWhite + "Ctrl+小键盘" + cReset + " 后 6 项（全局，免切窗）\n")
	b.WriteString("  " + cWhite + "HOME" + cReset + " 全关   " + cWhite + "F12" + cReset + " 重扫   " +
		cWhite + "F1" + cReset + " 帮助   " + cWhite + "ESC" + cReset + " 返回   " + cWhite + "END" + cReset + " 退出\n")
	b.WriteString("  " + cDim + a.getMsg() + cReset + "\n")

	writeConsole(b.String())
}

// statusColor 根据状态文本返回配色。
func statusColor(status string, active bool) string {
	switch {
	case strings.HasPrefix(status, "已冻结"), strings.HasPrefix(status, "已锁定"),
		strings.HasPrefix(status, "已归零"), strings.HasPrefix(status, "已放大"),
		strings.HasPrefix(status, "冷却已归零"), strings.HasPrefix(status, "已强制蓄力"),
		strings.HasPrefix(status, "保护中"):
		return cGreen
	case status == "未激活":
		return cDim
	default:
		return cRed
	}
}

// ---------- 会话协程 ----------

func (a *app) sessionWatch(s *session) {
	// 看护协程是长期运行的后台任务: 任何未捕获 panic 都会让整个进程退出。
	// 这里兜底恢复并提示，避免"修改器无故消失"。
	defer func() {
		if rec := recover(); rec != nil {
			a.setMsg(fmt.Sprintf("内部错误（看护协程已恢复）: %v", rec))
		}
	}()
	for {
		a.mu.Lock()
		stillCurrent := a.sess == s
		a.mu.Unlock()
		if !stillCurrent {
			return
		}
		if s.getProc() == nil || !s.getProc().Alive() {
			s.closeProc()
			s.r.SetProcess(nil)
			if p, err := core.Attach(s.prof.ProcessName); err == nil {
				s.setProc(p)
				s.r.SetProcess(p)
				a.setMsg(fmt.Sprintf("已附加 %s (PID %d)。", s.prof.ProcessName, p.Pid))
				time.Sleep(time.Second)
			}
		}
		if !s.r.HasSein() {
			if s.r.Refresh() {
				a.setMsg("已定位游戏对象，可以使用功能了。")
			}
		} else {
			s.r.Refresh()
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *app) engineLoop(s *session) {
	defer func() {
		if rec := recover(); rec != nil {
			a.setMsg(fmt.Sprintf("内部错误（功能引擎已恢复）: %v", rec))
		}
	}()
	for {
		a.mu.Lock()
		stillCurrent := a.sess == s
		a.mu.Unlock()
		if !stillCurrent {
			return
		}
		if p := s.getProc(); p != nil && p.Alive() {
			ori.TickAll(s.r)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------- main ----------

func main() {
	a := &app{}
	a.setMsg("")
	ck := newConsoleKeys()
	// 界面按键: 控制台输入事件（仅本窗口有焦点时才有事件）。
	// 若控制台输入不可用（标准输入被重定向等），退化为"全局热键 + 游戏前台抑制"。
	uiKeys := newKeyEdges(func(vk int) bool {
		if ck.available() {
			return ck.pressed(vk)
		}
		if core.GameFocusedAny(ori.Profiles[ori.Vanilla].ProcessName, ori.Profiles[ori.Definitive].ProcessName) {
			return false
		}
		return core.GetAsyncKeyDown(vk)
	})
	// 功能热键: 全局轮询，修改器进程存活期间始终生效。
	sysKeys := newKeyEdges(func(vk int) bool { return core.GetAsyncKeyDown(vk) })

	const (
		vkUp     = 0x26
		vkDown   = 0x28
		vkReturn = 0x0D
		vkSpace  = 0x20
		vkEscape = 0x1B
		vkW      = 0x57
		vkS      = 0x53
		vkHome   = 0x24
		vkEnd    = 0x23
		vkF1     = 0x70
		vkF12    = 0x7B
	)

	for {
		ck.poll()

		if !a.chosen {
			selUp := uiKeys.pressed(vkUp) || uiKeys.pressed(vkW)
			selDown := uiKeys.pressed(vkDown) || uiKeys.pressed(vkS)
			selRet := uiKeys.pressed(vkReturn)
			selEsc := uiKeys.pressed(vkEscape)
			if selUp {
				a.selCursor = (a.selCursor + 1) % 2
			}
			if selDown {
				a.selCursor = (a.selCursor + 1) % 2
			}
			if selRet {
				prof := ori.Profiles[ori.Vanilla]
				if a.selCursor == 1 {
					prof = ori.Profiles[ori.Definitive]
				}
				a.choose(prof)
			}
			if selEsc {
				fmt.Println("\n退出。")
				os.Exit(0)
			}
			renderSelector(a)
			time.Sleep(80 * time.Millisecond)
			continue
		}

		s := a.current()
		if s == nil {
			a.chosen = false
			continue
		}

		// 界面按键（控制台输入）: 只有修改器窗口有焦点时才有事件，
		// 因此无需再做前台判断。先统一采样，再依次处理。
		f1Pressed := uiKeys.pressed(vkF1)
		endPressed := uiKeys.pressed(vkEnd)
		escPressed := uiKeys.pressed(vkEscape)
		homePressed := uiKeys.pressed(vkHome)
		f12Pressed := uiKeys.pressed(vkF12)

		if f1Pressed {
			a.help.Store(!a.help.Load())
		}
		if endPressed {
			ori.DeactivateAll(s.feats, s.r)
			s.closeProc()
			fmt.Println("\nOriTrainer 已退出。")
			os.Exit(0)
		}
		if escPressed {
			ori.DeactivateAll(s.feats, s.r)
			s.closeProc()
			a.mu.Lock()
			a.sess = nil
			a.chosen = false
			a.mu.Unlock()
			a.setMsg("已返回版本选择。")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if homePressed {
			ori.DeactivateAll(s.feats, s.r)
			a.setMsg("已关闭全部功能")
		}
		if f12Pressed {
			s.closeProc()
			s.r.SetProcess(nil)
			a.setMsg("已重置，重新附加中…")
		}
		// ---- 全局热键：仅小键盘数字键（可配 Ctrl）----
		// 每个功能只有一个快捷键；小键盘 1-9/0 为前 10 项，
		// Ctrl+小键盘 1-6 为后 6 项（Ctrl+1 是一命保护）。
		const vkControl = 0x11
		ctrlDown := core.GetAsyncKeyDown(vkControl)

		// 小键盘 1-9 是 0x61-0x69，小键盘 0 是 0x60（全局热键，与焦点无关）
		for i := 0; i < 9; i++ {
			if sysKeys.pressed(0x61 + i) {
				digit := i + 1
				hit := false
				for _, f := range s.feats {
					if f.Digit == digit && f.NeedCtrl == ctrlDown {
						toggleFeature(a, s, f)
						hit = true
					}
				}
				// Ctrl+小键盘 1 = 一命保护（不在 feats 列表中）
				if !hit && ctrlDown && digit == ori.CtrlOneLifeDigit {
					toggleOneLife(a)
				}
			}
		}
		if sysKeys.pressed(0x60) {
			for _, f := range s.feats {
				if f.Digit == 0 && f.NeedCtrl == ctrlDown {
					toggleFeature(a, s, f)
				}
			}
		}

		// ---- 前台导航：用 ↑↓ 选择 + 回车/空格切换 ----
		// 供没有小键盘的键盘使用（全局热键仍可用）。
		// 与其他界面按键一样走控制台输入，仅在修改器有焦点时生效。
		{
			n := len(navList(s))
			if uiKeys.pressed(vkUp) || uiKeys.pressed(vkW) {
				if n > 0 {
					a.navCursor = (a.navCursor - 1 + n) % n
				}
			}
			if uiKeys.pressed(vkDown) || uiKeys.pressed(vkS) {
				if n > 0 {
					a.navCursor = (a.navCursor + 1) % n
				}
			}
			if uiKeys.pressed(vkReturn) || uiKeys.pressed(vkSpace) {
				a.navToggle(s, a.navCursor)
			}
		}

		renderTrainer(a, s)
		time.Sleep(80 * time.Millisecond)
	}
}
