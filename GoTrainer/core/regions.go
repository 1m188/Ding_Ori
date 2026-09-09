package core

import "unsafe"

// AllRegions 枚举全部已提交区域（任意保护类型）。
func (p *Process) AllRegions() []Region {
	var out []Region
	var addr uintptr
	var m mbi
	mbiLen := uintptr(unsafe.Sizeof(m))
	for {
		r1, _, _ := procVirtualQueryEx.Call(p.Handle, addr, uintptr(unsafe.Pointer(&m)), mbiLen)
		if r1 == 0 {
			break
		}
		if m.State == memCommit && m.RegionSize > 0 {
			out = append(out, Region{Base: uint32(m.BaseAddress), Size: uint32(m.RegionSize)})
		}
		addr = m.BaseAddress + m.RegionSize
		if addr >= 0x7FFF0000 {
			break
		}
	}
	return out
}
