// Package core —— Windows 进程内存读写核心（纯 syscall，零外部依赖）。
package core

import (
	"fmt"
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

// WritablePrivateRegions 枚举已提交、可写的堆区域（含 mono 的 MEM_MAPPED 堆段）。
// 注意: ① 保护属性须掩码低 8 位判断（高维修饰位会干扰精确匹配）；
// ② 64 位 Windows 上 mono 的 32 位堆使用 MEM_MAPPED section 分配
//    （实测 0x57913000 区域 type=MEM_MAPPED protect=PAGE_EXECUTE_READWRITE），
//    因此不能限定 Type==MEM_PRIVATE，只要求可写即可。
// ③ 游戏暂停/运行切换时区域保护属性可能变化，扫描场景请改用 ReadableRegions。
func (p *Process) WritablePrivateRegions() []Region {
	var out []Region
	var addr uintptr
	var m mbi
	mbiLen := uintptr(unsafe.Sizeof(m))
	for {
		r1, _, _ := procVirtualQueryEx.Call(p.Handle, addr, uintptr(unsafe.Pointer(&m)), mbiLen)
		if r1 == 0 {
			break
		}
		baseProt := m.Protect & 0xFF
		writable := baseProt == pageReadWrite || baseProt == pageWriteCopy || baseProt == 0x40 /*PAGE_EXECUTE_READWRITE*/
		if m.State == memCommit && writable && m.Protect&pageGuard == 0 && m.RegionSize > 0 {
			out = append(out, Region{Base: uint32(m.BaseAddress), Size: uint32(m.RegionSize)})
		}
		addr = m.BaseAddress + m.RegionSize
		if addr >= 0x7FFF0000 {
			break
		}
	}
	return out
}

// ReadableRegions 枚举全部已提交且可读的区域（任意类型/保护，含映像）。
// RPM 对只读页同样有效；对象扫描应使用本函数，避免游戏状态切换导致的
// 区域保护属性时变造成漏扫。
func (p *Process) ReadableRegions() []Region {
	var out []Region
	var addr uintptr
	var m mbi
	mbiLen := uintptr(unsafe.Sizeof(m))
	for {
		r1, _, _ := procVirtualQueryEx.Call(p.Handle, addr, uintptr(unsafe.Pointer(&m)), mbiLen)
		if r1 == 0 {
			break
		}
		if m.State == memCommit && m.Protect != pageNoAccess && m.Protect&pageGuard == 0 && m.RegionSize > 0 {
			out = append(out, Region{Base: uint32(m.BaseAddress), Size: uint32(m.RegionSize)})
		}
		addr = m.BaseAddress + m.RegionSize
		if addr >= 0x7FFF0000 {
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
