// OriTrainer (Go) — 奥日与迷失森林（原版）风灵月影风格 TUI 修改器。
// 纯标准库实现: Windows console ANSI 渲染 + GetAsyncKeyState 全局热键轮询。
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

// ---------- console (ANSI + WriteConsoleW) ----------

var (
	k32      = syscall.NewLazyDLL("kernel32.dll")
	pStdout  = k32.NewProc("GetStdHandle")
	pWriteW  = k32.NewProc("WriteConsoleW")
	pGetMode = k32.NewProc("GetConsoleMode")
	pSetMode = k32.NewProc("SetConsoleMode")
	pSetCur  = k32.NewProc("SetConsoleCursorPosition")
	pSetTitle = k32.NewProc("SetConsoleTitleW")
)

const stdOutputHandle = ^uintptr(10) // STD_OUTPUT_HANDLE = (DWORD)-11

var stdoutHandle uintptr

func init() {
	h, _, _ := pStdout.Call(^uintptr(10))
	stdoutHandle = h
	var mode uint32
	pGetMode.Call(stdoutHandle, uintptr(unsafe.Pointer(&mode)))
	pSetMode.Call(stdoutHandle, uintptr(mode|0x0004)) // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if t, err := syscall.UTF16PtrFromString("OriTrainer — 奥日与迷失森林 (原版) 修改器"); err == nil {
		pSetTitle.Call(uintptr(unsafe.Pointer(t)))
	}
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
)

// ---------- 应用状态 ----------

type app struct {
	mu   sync.Mutex
	proc *core.Process
	r    *ori.Runtime
	msg  atomic.Value // string
	help atomic.Bool
}

func (a *app) setMsg(s string) { a.msg.Store(s) }
func (a *app) getMsg() string  { s, _ := a.msg.Load().(string); return s }

// render 渲染一帧。
func (a *app) render() {
	var b strings.Builder
	b.WriteString("\x1b[2J")
	homeCursor()

	b.WriteString(cTitle + "  OriTrainer v1.0 — 奥日与迷失森林 (原版) 修改器\n" + cReset)
	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)

	s := a.r.Read()
	if !s.Attached {
		b.WriteString("  " + cRed + "○ 未检测到 ori.exe — 请先启动游戏（修改器每 2 秒自动附加）" + cReset + "\n")
	} else {
		st := fmt.Sprintf("  %s● 已附加 %s (PID %d)%s   ", cGreen, ori.ProcessName, s.Pid, cReset)
		if s.SlotsOK {
			st += cGreen + "静态槽校验 OK" + cReset + "   "
		} else {
			st += cRed + "静态槽校验失败（构建变更?）" + cReset + "   "
		}
		if s.SeinOK {
			st += cGreen + fmt.Sprintf("Sein 已定位 (%08X)", s.SeinLevel) + cReset
		} else {
			st += cYel + "Sein 扫描中…（进入存档后可定位）" + cReset
		}
		b.WriteString(st + "\n")
		if s.SeinOK {
			b.WriteString(fmt.Sprintf("  实时: 生命 %.0f/%d   能量 %.1f/%.1f   技能点 %d   经验 %d   死亡 %d\n",
				s.HealthCur, s.HealthMax, s.EnergyCur, s.EnergyMax, s.SkillPoints, s.Experience, s.Deaths))
		}
	}

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)

	if a.help.Load() {
		b.WriteString(cYel + "  [帮助] 冻结=激活时捕获当前值并持续写回; 进入存档后生效; 全局热键无需切换窗口\n" + cReset)
	}

	for _, f := range feats {
		box := cWhite + "[ ]" + cReset
		if f.Active() {
			box = cGreen + "[x]" + cReset
		}
		st := f.Status()
		stCol := cDim
		switch {
		case strings.HasPrefix(st, "已冻结"):
			stCol = cGreen
		case st != "未激活":
			stCol = cRed
		}
		b.WriteString(fmt.Sprintf("  %s  %s数字键 %d%s   %s   %s%s%s\n",
			box, cWhite, f.Num, cReset, f.Name, stCol, st, cReset))
	}

	b.WriteString(cBox + "  ────────────────────────────────────────────────────────\n" + cReset)
	b.WriteString("  " + cWhite + "数字键 1-5" + cReset + " 开关功能   " + cWhite + "HOME" + cReset + " 关闭全部   " +
		cWhite + "F12" + cReset + " 重新附加/重扫   " + cWhite + "F1" + cReset + " 帮助   " + cWhite + "END" + cReset + " 退出\n")
	b.WriteString("  " + cDim + a.getMsg() + cReset + "\n")

	writeConsole(b.String())
}

// ---------- 热键（边沿检测） ----------

type keyEdges struct{ prev map[int]bool }

func newKeyEdges() *keyEdges { return &keyEdges{prev: make(map[int]bool)} }

func (e *keyEdges) pressed(vk int) bool {
	down := core.GetAsyncKeyDown(vk)
	was := e.prev[vk]
	e.prev[vk] = down
	return down && !was
}

// ---------- main ----------

var (
	a     = &app{r: &ori.Runtime{}}
	feats = ori.BuildFeatures()
	keys  = newKeyEdges()
	msgMu sync.Mutex
)

func setMsg(s string) {
	msgMu.Lock()
	a.setMsg(s)
	msgMu.Unlock()
}

func main() {
	a.setMsg("启动完成。请右键以管理员身份运行，并先启动游戏。")

	go attachWatcher()
	go engineLoop()

	// 数字键: 1..5 -> 功能 1..5
	toggleByNumber := func(num int) {
		for _, f := range feats {
			if f.Num == num {
				newState := !f.Active()
				f.SetActive(newState)
				if newState {
					setMsg(fmt.Sprintf("已激活: %s — 捕获当前值并冻结", f.Name))
				} else {
					setMsg(fmt.Sprintf("已关闭: %s", f.Name))
				}
			}
		}
	}

	for {
		if keys.pressed(0x70) { // F1
			a.help.Store(!a.help.Load())
		}
		if keys.pressed(0x23) { // END 退出
			ori.DeactivateAll(feats)
			a.mu.Lock()
			if a.proc != nil {
				a.proc.Close()
			}
			a.mu.Unlock()
			fmt.Println("\nOriTrainer 已退出。")
			os.Exit(0)
		}
		if keys.pressed(0x7B) { // F12 重新附加 + 重扫
			a.mu.Lock()
			if a.proc != nil {
				a.proc.Close()
				a.proc = nil
			}
			a.r.SetProcess(nil)
			a.mu.Unlock()
			setMsg("已重置，正在重新附加…")
		}
		if keys.pressed(0x24) { // HOME 关闭全部
			ori.DeactivateAll(feats)
			setMsg("已关闭全部功能")
		}
		for i := 0; i < 5; i++ {
			if keys.pressed(0x31 + i) {
				toggleByNumber(i + 1)
			}
		}

		a.render()
		time.Sleep(80 * time.Millisecond)
	}
}

// attachWatcher 自动附加 + 静态槽校验 + Sein 对象扫描（扫描在锁外执行）。
func attachWatcher() {
	for {
		a.mu.Lock()
		p := a.proc
		a.mu.Unlock()

		if p == nil || !p.Alive() {
			if p != nil {
				p.Close()
			}
			np, err := core.Attach(ori.ProcessName)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			a.mu.Lock()
			a.proc = np
			a.r.SetProcess(np)
			a.mu.Unlock()
			setMsg(fmt.Sprintf("已附加 %s (PID %d)，等待进入存档后扫描 Sein 对象…", ori.ProcessName, np.Pid))
			time.Sleep(1 * time.Second)
			continue
		}

		// 已附加: 周期校验 + 未定位则重扫（全堆扫描 ~14 秒，在锁外执行）
		if !a.r.HasSein() {
			setMsg("正在全堆扫描定位 Sein 对象（约 15 秒）…")
			if err := a.r.ScanSeinObjects(); err == nil {
				setMsg("Sein 对象已定位，可以使用功能了。")
			} else {
				setMsg("扫描未命中（需进入存档）—— 15 秒后自动重试")
				time.Sleep(15 * time.Second)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// engineLoop 功能引擎: 每 50ms 驱动激活中的功能。
func engineLoop() {
	for {
		a.mu.Lock()
		p := a.proc
		a.mu.Unlock()
		if p != nil && p.Alive() {
			ori.TickAll(feats, a.r)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
