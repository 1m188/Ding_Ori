// Package core —— Windows 进程内存读写核心（纯 syscall，零外部依赖）。
package core

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	moduser32   = syscall.NewLazyDLL("user32.dll")

	procOpenProcess       = modkernel32.NewProc("OpenProcess")
	procReadProcessMemory = modkernel32.NewProc("ReadProcessMemory")
	procWriteProcessMem   = modkernel32.NewProc("WriteProcessMemory")
	procCloseHandle       = modkernel32.NewProc("CloseHandle")
	procVirtualQueryEx    = modkernel32.NewProc("VirtualQueryEx")
	procGetExitCodeProc   = modkernel32.NewProc("GetExitCodeProcess")
	procGetAsyncKeyState  = moduser32.NewProc("GetAsyncKeyState")
)

const (
	procVMRead         = 0x0010
	procVMWrite        = 0x0020
	procVMOperation    = 0x0008
	procQueryInfo      = 0x0400
	procAccess         = procVMRead | procVMWrite | procVMOperation | procQueryInfo
	memCommit          = 0x1000
	memPrivate         = 0x40000
	pageReadonly       = 0x02
	pageReadWrite      = 0x04
	pageWriteCopy      = 0x08
	pageNoAccess       = 0x01
	pageGuard          = 0x100
)

// Process 已打开的目标进程句柄。
type Process struct {
	Handle    uintptr
	Pid       uint32
	Name      string
	closed    bool
}

// Attach 按进程名附加（取第一个匹配）。
func Attach(name string) (*Process, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("Process32First: %w", err)
	}
	var pid uint32
	for {
		if syscall.UTF16ToString(entry.ExeFile[:]) == name {
			pid = entry.ProcessID
			break
		}
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	if pid == 0 {
		return nil, fmt.Errorf("进程 %s 未运行", name)
	}
	h, _, _ := procOpenProcess.Call(procAccess, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("OpenProcess(%d) 失败（需要管理员权限）", pid)
	}
	return &Process{Handle: h, Pid: pid, Name: name}, nil
}

// Close 释放句柄。
func (p *Process) Close() {
	if p != nil && !p.closed {
		procCloseHandle.Call(p.Handle)
		p.closed = true
	}
}

// ReadBytes 读取任意字节。
func (p *Process) ReadBytes(addr uint32, buf []byte) bool {
	if p == nil || p.Handle == 0 || addr == 0 {
		return false
	}
	var read uintptr
	ok, _, _ := procReadProcessMemory.Call(p.Handle, uintptr(addr),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&read)))
	return ok != 0 && read == uintptr(len(buf))
}

// WriteBytes 写入任意字节。
func (p *Process) WriteBytes(addr uint32, data []byte) bool {
	if p == nil || p.Handle == 0 || addr == 0 {
		return false
	}
	var written uintptr
	ok, _, _ := procWriteProcessMem.Call(p.Handle, uintptr(addr),
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	return ok != 0 && written == uintptr(len(data))
}

// ReadU32 读取 4 字节（32 位进程指针尺寸）。
func (p *Process) ReadU32(addr uint32) (uint32, bool) {
	var b [4]byte
	if !p.ReadBytes(addr, b[:]) {
		return 0, false
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, true
}

// WriteU32 写入 4 字节。
func (p *Process) WriteU32(addr uint32, v uint32) bool {
	return p.WriteBytes(addr, []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

// ReadI32 / WriteI32 有符号 4 字节。
func (p *Process) ReadI32(addr uint32) (int32, bool) {
	v, ok := p.ReadU32(addr)
	return int32(v), ok
}

func (p *Process) WriteI32(addr uint32, v int32) bool {
	return p.WriteU32(addr, uint32(v))
}

// ReadF32 / WriteF32 单精度浮点。
func (p *Process) ReadF32(addr uint32) (float32, bool) {
	v, ok := p.ReadU32(addr)
	return *(*float32)(unsafe.Pointer(&v)), ok
}

func (p *Process) WriteF32(addr uint32, v float32) bool {
	return p.WriteU32(addr, *(*uint32)(unsafe.Pointer(&v)))
}

// WalkChain 指针链遍历: cur = base+offs[0]; 之后每步先解引用再加下一个偏移;
// 最终地址 = 最后一次解引用值 + 末偏移。与 CE 指针路径一致。
func (p *Process) WalkChain(base uint32, offs []uint32) (uint32, bool) {
	if p == nil || !p.Alive() || len(offs) == 0 {
		return 0, false
	}
	cur := base + offs[0]
	for i := 1; i < len(offs); i++ {
		v, ok := p.ReadU32(cur)
		if !ok || v == 0 {
			return 0, false
		}
		cur = v + offs[i]
	}
	return cur, true
}

// Alive 进程是否仍然存活（GetExitCodeProcess == STILL_ACTIVE）。
func (p *Process) Alive() bool {
	if p == nil || p.Handle == 0 || p.closed {
		return false
	}
	var code uint32
	ret, _, _ := procGetExitCodeProc.Call(p.Handle, uintptr(unsafe.Pointer(&code)))
	return ret != 0 && code == 259 // STILL_ACTIVE
}

// ---------- 内存区域枚举（堆扫描用） ----------

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

// Region 一段已提交的内存区域。
type Region struct {
	Base uint32
	Size uint32
}

// ReadableRegions 枚举全部已提交且可读的区域（任意类型/保护，含映像）。
// RPM 对只读页同样有效；对象扫描应使用本函数，避免游戏状态切换导致的
// 区域保护属性时变造成漏扫。
//
// WoW64 注意: 32 位目标进程的 mono 大堆位于 64 位地址空间的高位
// （如 0x800265C0，RPM 可正常读写），但 32 位视角的 VQEx 看不到它。
// 本程序是 64 位进程，VQEx 返回的是 64 位视图（BaseAddress 高位为
// 0xFFFFFFFF_xxxxxxxx），枚举上限必须放宽到 64 位，Region 取低 32 位。
func (p *Process) ReadableRegions() []Region {
	var out []Region
	var addr uint64
	var m mbi
	mbiLen := uintptr(unsafe.Sizeof(m))
	for {
		r1, _, _ := procVirtualQueryEx.Call(p.Handle, uintptr(addr), uintptr(unsafe.Pointer(&m)), mbiLen)
		if r1 == 0 {
			break
		}
		if m.State == memCommit && m.Protect != pageNoAccess && m.Protect&pageGuard == 0 && m.RegionSize > 0 {
			// 只取低 32 位有内容的区域（WoW64 的高位影子区形如 0xFFFFFFFF80026000）
			base := uint64(m.BaseAddress) & 0xFFFFFFFF
			size := m.RegionSize
			if base != 0 && size > 0 && base+uint64(size) <= 0x100000000 {
				out = append(out, Region{Base: uint32(base), Size: uint32(size)})
			}
		}
		addr = uint64(m.BaseAddress) + uint64(m.RegionSize)
		if addr >= 0x800000000000 {
			break
		}
	}
	return out
}

// GetAsyncKeyDown 查询虚拟键当前按下状态。
func GetAsyncKeyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// ---------- 窗口前台检测 ----------

var (
	procGetForegroundWindow = moduser32.NewProc("GetForegroundWindow")
	procGetConsoleWindow    = modkernel32.NewProc("GetConsoleWindow")
	procGetWindowThreadProc = moduser32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW      = moduser32.NewProc("GetWindowTextW")
	procGetWindowTextLen    = moduser32.NewProc("GetWindowTextLengthW")
)

// WindowIsForeground 判断当前控制台/终端窗口是否处于前台。
//
// 判定策略（按可靠性排序，任一命中即视为前台）:
//  1. GetConsoleWindow() == 前台窗口（传统 conhost 场景）
//  2. 前台窗口的 PID 沿本进程父链可达（进程健康时的常规场景）
//  3. 前台窗口标题含 "OriTrainer"（Windows Terminal 等宿主场景兜底：
//     GoTrainer 启动后会把宿主窗口标题设置为 "OriTrainer — ..."）
func WindowIsForeground() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return false
	}
	cw, _, _ := procGetConsoleWindow.Call()
	if cw != 0 && fg == cw {
		return true
	}

	// 策略 2: PID 父链
	var fgPid uint32
	procGetWindowThreadProc.Call(fg, uintptr(unsafe.Pointer(&fgPid)), 0, 0)
	if fgPid != 0 {
		me := uint32(os.Getpid())
		if fgPid == me {
			return true
		}
		cur := me
		for i := 0; i < 8; i++ {
			parent := processParentPid(cur)
			if parent == 0 || parent == cur {
				break
			}
			if parent == fgPid {
				return true
			}
			cur = parent
		}
	}

	// 策略 3: 窗口标题兜底（覆盖 Windows Terminal 宿主场景）
	return windowTitleContains(fg, "OriTrainer")
}

// windowTitleContains 判断窗口标题是否包含指定子串（大小写敏感，UTF-16）。
func windowTitleContains(hwnd uintptr, sub string) bool {
	length, _, _ := procGetWindowTextLen.Call(hwnd)
	if length == 0 {
		return false
	}
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := syscall.UTF16ToString(buf)
	return strings.Contains(title, sub)
}

// processParentPid 返回指定进程的父进程 PID（取不到返回 0）。
func processParentPid(pid uint32) uint32 {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return 0
	}
	for {
		if entry.ProcessID == pid {
			return entry.ParentProcessID
		}
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return 0
}
