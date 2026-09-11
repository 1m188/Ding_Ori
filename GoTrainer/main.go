// OriTrainer (Go) — 奥日与迷失森林 双版本修改器（原版 + 终极版）。
// 纯标准库实现: Windows console ANSI TUI + GetAsyncKeyState 全局热键。
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
)

const stdOutputHandle = ^uintptr(10) // STD_OUTPUT_HANDLE = (DWORD)-11

var stdoutHandle uintptr

func init() {
	h, _, _ := pStdout.Call(^uintptr(10))
	stdoutHandle = h
	var mode uint32
	pGetMode.Call(stdoutHandle, uintptr(unsafe.Pointer(&mode)))
	pSetMode.Call(stdoutHandle, uintptr(mode|0x0004))
}

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

// ---------- 热键（边沿检测） ----------

type keyEdges struct{ prev map[int]bool }

func newKeyEdges() *keyEdges { return &keyEdges{prev: make(map[int]bool)} }

func (e *keyEdges) pressed(vk int) bool {
	down := core.GetAsyncKeyDown(vk)
	was := e.prev[vk]
	e.prev[vk] = down
	return down && !was
}

// ---------- 会话状态 ----------

type session struct {
	prof  ori.Profile
	proc  *core.Process
	r     *ori.Runtime
	feats []*ori.Feature
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

// navItems 修改器界面的可选项目总数（功能数 + 1 个"一命保护"）。
func navItems(s *session) int {
	if s == nil {
		return 0
	}
	return len(s.feats) + 1
}

// navToggleAt 切换第 idx 个导航项（idx == len(feats) 时是一命保护）。
func (a *app) navToggleAt(s *session, idx int) {
	if s == nil {
		return
	}
	if idx < 0 || idx >= navItems(s) {
		return
	}
	if idx == len(s.feats) {
		ol := ori.OneLife()
		on := !ol.Active()
		ol.SetActive(on)
		if on {
			a.setMsg("已激活: 一命保护 — 死亡如普通模式在检查点复活，成就资格保留")
		} else {
			a.setMsg("已关闭: 一命保护")
		}
		return
	}
	toggleFeature(a, s, s.feats[idx])
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
	a.sess = s
	a.chosen = true
	if p, err := core.Attach(prof.ProcessName); err == nil {
		s.proc = p
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
			b.WriteString(fmt.Sprintf("  实时: 生命 %.0f/%d   能量 %.1f/%.1f   技能点 %d   经验 %d   死亡 %d\n",
				snap.HealthCur, snap.HealthMax, snap.EnergyCur, snap.EnergyMax, snap.SkillPoints, snap.Experience, snap.Deaths))
		}
	}

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)

	if a.help.Load() {
		b.WriteString(cYel + "  [帮助] 冻结=激活时捕获当前值并持续写回; 进入存档后生效; 全局热键免切窗\n" + cReset)
	}

	// 前台导航: 窗口在前台时显示光标
	fg := core.WindowIsForeground()

	// 统一渲染: 功能项 + 一命保护（后者固定在末尾）
	renderRow := func(idx int, box, label, name, status string, statusActive bool) {
		cursor := "  "
		if fg && a.navCursor == idx {
			cursor = cCyan + "▶" + cReset + " "
		} else {
			cursor = "  "
		}
		// 热键标签固定宽度（Ctrl+小键盘 N = 12 个显示字符）
		b.WriteString(fmt.Sprintf("  %s%s  %s%-12s%s %s   %s%s%s\n",
			cursor, box, cWhite, label, cReset, name, statusColor(status, statusActive), status, cReset))
	}

	for i, f := range s.feats {
		box := cWhite + "[ ]" + cReset
		if f.Active() {
			box = cGreen + "[x]" + cReset
		}
		renderRow(i, box, f.HotkeyLabel(), f.Name, f.Status(), f.Active())
	}

	// 一命保护（Ctrl+小键盘 3，仅终极版生效）
	ol := ori.OneLife()
	olBox := cWhite + "[ ]" + cReset
	if ol.Active() {
		olBox = cGreen + "[x]" + cReset
	}
	renderRow(len(s.feats), olBox, "Ctrl+小键盘 3", ol.Name(), ol.Status(), ol.Active())

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)
	if fg {
		b.WriteString("  " + cCyan + "前台模式" + cReset + ": " + cWhite + "↑↓" + cReset + " 选择   " +
			cWhite + "回车/空格" + cReset + " 开关   |   " +
			cWhite + "小键盘 1-8" + cReset + " 功能   " + cWhite + "Ctrl+小键盘" + cReset + " 组合功能\n")
	} else {
		b.WriteString("  " + cDim + "全局热键: 小键盘 1-8 功能 / Ctrl+小键盘 组合功能（切到本窗口可用 ↑↓ 导航）" + cReset + "\n")
	}
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
	for {
		a.mu.Lock()
		stillCurrent := a.sess == s
		a.mu.Unlock()
		if !stillCurrent {
			return
		}
		if s.proc == nil || !s.proc.Alive() {
			if s.proc != nil {
				s.proc.Close()
				s.proc = nil
				s.r.SetProcess(nil)
			}
			if p, err := core.Attach(s.prof.ProcessName); err == nil {
				a.mu.Lock()
				s.proc = p
				a.mu.Unlock()
				s.r.SetProcess(p)
				a.setMsg(fmt.Sprintf("已附加 %s (PID %d)。", s.prof.ProcessName, p.Pid))
				time.Sleep(time.Second)
			}
		}
		if !s.r.HasSein() {
			a.setMsg("正在全堆扫描定位 Sein 对象（约 15 秒）…")
			if err := s.r.ScanObjects(); err == nil {
				a.setMsg("Sein 对象已定位，可以使用功能了。")
			} else {
				a.setMsg("扫描未命中（需进入存档）—— 15 秒后自动重试")
				time.Sleep(15 * time.Second)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *app) engineLoop(s *session) {
	for {
		a.mu.Lock()
		stillCurrent := a.sess == s
		a.mu.Unlock()
		if !stillCurrent {
			return
		}
		if s.proc != nil && s.proc.Alive() {
			ori.TickAll(s.r)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------- main ----------

func main() {
	a := &app{}
	a.setMsg("")
	keys := newKeyEdges()

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
		if !a.chosen {
			if keys.pressed(vkUp) || keys.pressed(vkW) {
				a.selCursor = (a.selCursor + 1) % 2
			}
			if keys.pressed(vkDown) || keys.pressed(vkS) {
				a.selCursor = (a.selCursor + 1) % 2
			}
			if keys.pressed(vkReturn) {
				prof := ori.Profiles[ori.Vanilla]
				if a.selCursor == 1 {
					prof = ori.Profiles[ori.Definitive]
				}
				a.choose(prof)
			}
			if keys.pressed(vkEscape) {
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

		if keys.pressed(vkF1) {
			a.help.Store(!a.help.Load())
		}
		if keys.pressed(vkEnd) {
			ori.DeactivateAll(s.feats, s.r)
			if s.proc != nil {
				s.proc.Close()
			}
			fmt.Println("\nOriTrainer 已退出。")
			os.Exit(0)
		}
		if keys.pressed(vkEscape) {
			ori.DeactivateAll(s.feats, s.r)
			if s.proc != nil {
				s.proc.Close()
			}
			a.mu.Lock()
			a.sess = nil
			a.chosen = false
			a.mu.Unlock()
			a.setMsg("已返回版本选择。")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if keys.pressed(vkHome) {
			ori.DeactivateAll(s.feats, s.r)
			a.setMsg("已关闭全部功能")
		}
		if keys.pressed(vkF12) {
			a.mu.Lock()
			if s.proc != nil {
				s.proc.Close()
				s.proc = nil
			}
			s.r.SetProcess(nil)
			a.mu.Unlock()
			a.setMsg("已重置，重新附加中…")
		}
		// ---- 全局热键：仅小键盘数字键（可配 Ctrl）----
		const vkControl = 0x11
		ctrlDown := core.GetAsyncKeyDown(vkControl)

		// 小键盘 1-9 是 0x61-0x69，小键盘 0 是 0x60
		for i := 0; i < 9; i++ {
			if keys.pressed(0x61 + i) {
				digit := i + 1
				for _, f := range s.feats {
					if f.Digit == digit && f.NeedCtrl == ctrlDown {
						toggleFeature(a, s, f)
					}
				}
			}
		}
		if keys.pressed(0x60) {
			for _, f := range s.feats {
				if f.Digit == 0 && f.NeedCtrl == ctrlDown {
					toggleFeature(a, s, f)
				}
			}
		}
		// 一命保护：Ctrl + 小键盘 3（仅终极版有意义）
		if keys.pressed(0x63) && ctrlDown {
			ol := ori.OneLife()
			newState := !ol.Active()
			ol.SetActive(newState)
			if newState {
				a.setMsg("已激活: 一命保护 — 死亡如普通模式在检查点复活，成就资格保留")
			} else {
				a.setMsg("已关闭: 一命保护")
			}
		}

		// ---- 前台导航：修改器窗口在前台时，用 ↑↓ 选择 + 回车/空格切换 ----
		// 供没有小键盘的键盘使用（全局热键仍可用）。
		if core.WindowIsForeground() {
			if keys.pressed(vkUp) || keys.pressed(vkW) {
				n := navItems(s)
				if n > 0 {
					a.navCursor = (a.navCursor - 1 + n) % n
				}
			}
			if keys.pressed(vkDown) || keys.pressed(vkS) {
				n := navItems(s)
				if n > 0 {
					a.navCursor = (a.navCursor + 1) % n
				}
			}
			if keys.pressed(vkReturn) || keys.pressed(vkSpace) {
				a.navToggleAt(s, a.navCursor)
			}
		}

		renderTrainer(a, s)
		time.Sleep(80 * time.Millisecond)
	}
}
