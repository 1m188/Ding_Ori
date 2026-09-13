package ori

import (
	"bytes"
	"fmt"
	"strings"

	"oritrainer/core"
)

// ---------- 字段偏移核验 / 快照诊断（供 cmd/probe 使用）----------
//
// 这些函数是"换版本/换游戏 build 后核验偏移"的入口，只读、不写内存:
//
//	probe snap -vanilla              用修改器自身的偏移常量打印每个功能依赖字段
//	probe fieldoff <类> <字段>       按"字段名+声明类名"从 mono 元数据反查偏移
//
// `snap` 的价值在于: 它和功能执行器读的是**同一批偏移常量**，所以只要输出值
// 合理（生命 8/12、跳跃高度 3.0、能力指针都能解析……），就说明这些偏移在该
// 版本可用，不需要逐个功能试写。

// DebugSnap 打印所有功能依赖字段的现值。只读。
func DebugSnap(r *Runtime) string {
	var b strings.Builder
	ver := Vanilla
	if r.Prof != nil {
		ver = r.Prof.Version
	}
	sein, level, soul, jump, dbl, death := r.Addrs()
	fmt.Fprintf(&b, "sein=0x%08X level=0x%08X soul=0x%08X jump=0x%08X dbl=0x%08X death=0x%08X\n",
		sein, level, soul, jump, dbl, death)
	if r.Proc == nil || sein == 0 {
		return b.String()
	}
	p := r.Proc

	if mor, ok := p.ReadU32(sein + OffSeinMortality); ok {
		if h, ok2 := p.ReadU32(mor + OffMortalityHealth); ok2 {
			cur, _ := p.ReadF32(h + OffHealthAmount)
			// 注意: 生命上限是 int（点数；1 球 = 4 点），不是 float。
			max, _ := p.ReadI32(h + OffHealthMaxHealth)
			fmt.Fprintf(&b, "health  obj=0x%08X cur(+0x%X)=%.2f max(+0x%X)=%d\n",
				h, OffHealthAmount, cur, OffHealthMaxHealth, max)
			dumpFloats(&b, p, "health", h, 0x40)
		}
	}
	if e, ok := p.ReadU32(sein + OffSeinEnergy); ok {
		cur, _ := p.ReadF32(e + OffEnergyCurrent)
		max, _ := p.ReadF32(e + OffEnergyMax)
		fmt.Fprintf(&b, "energy  obj=0x%08X cur(+0x%X)=%.2f max(+0x%X)=%.2f\n",
			e, OffEnergyCurrent, cur, OffEnergyMax, max)
		dumpFloats(&b, p, "energy", e, 0x40)
	}
	if level != 0 {
		sp, _ := p.ReadI32(level + OffLevelSkillPoints)
		lv, _ := p.ReadI32(level + OffLevelCurrent)
		exp, _ := p.ReadI32(level + OffLevelExperience)
		fmt.Fprintf(&b, "level   SP=%d Lv=%d EXP=%d\n", sp, lv, exp)
	}
	if death != 0 {
		d, _ := p.ReadI32(death + OffDeathCounterValue)
		fmt.Fprintf(&b, "deaths  %d\n", d)
	}
	if soul != 0 {
		cd, _ := p.ReadF32(soul + OffSoulFlameCooldownRemaining)
		hd, _ := p.ReadF32(soul + OffSoulFlameHoldDown)
		dur, _ := p.ReadF32(soul + OffSoulFlameCooldownDuration)
		cast, _ := p.ReadU8(soul + OffSoulFlameCastFlag)
		lock, _ := p.ReadU8(soul + OffSoulFlameLock)
		fmt.Fprintf(&b, "soul    cooldownRemaining=%.2f holdDown=%.2f cdDuration=%.2f isCasting=%d lock=%d\n",
			cd, hd, dur, cast, lock)
	}
	if jump != 0 {
		for i, off := range JumpHeightOffsets(ver) {
			v, _ := p.ReadF32(jump + off)
			fmt.Fprintf(&b, "jump[%d] +0x%X = %.3f\n", i, off, v)
		}
	}
	if dbl != 0 {
		n, _ := p.ReadI32(dbl + OffDoubleJumpCount)
		fmt.Fprintf(&b, "dbljump count=%d\n", n)
	}
	if pa, ok := p.ReadU32(sein + OffSeinPlayerAbil); ok && isHeapPtr(pa) {
		fmt.Fprintf(&b, "playerab obj=0x%08X\n", pa)
		for _, off := range BaseAbilityOffsets(ver) {
			obj, ok2 := p.ReadU32(pa + off)
			if !ok2 || !isHeapPtr(obj) {
				fmt.Fprintf(&b, "  +0x%X -> 无效\n", off)
				continue
			}
			ha, _ := p.ReadU8(obj + OffAbilityHasAbility)
			fmt.Fprintf(&b, "  +0x%X -> 0x%08X HasAbility=%d\n", off, obj, ha)
		}
	}
	if gw := r.GameWorldAddr(); gw != 0 {
		if areas, ok := p.ReadU32(gw + OffGameWorldRuntimeAreas); ok {
			if data, ok2 := p.ReadU32(areas + OffListItems); ok2 {
				size, _ := p.ReadI32(areas + OffListSize)
				fmt.Fprintf(&b, "gw=0x%08X areas size=%d data=0x%08X\n", gw, size, data)
				for i := 0; i < int(size) && i < 6; i++ {
					if a, ok3 := p.ReadU32(data + OffArrayData + uint32(i)*4); ok3 {
						c, _ := p.ReadF32(a + OffAreaCompletion)
						fmt.Fprintf(&b, "  area[%d] completion=%.4f\n", i, c)
					}
				}
			}
		}
	}
	if k := r.KeysAddr(); k != 0 {
		var buf [3]byte
		p.ReadBytes(k, buf[:])
		fmt.Fprintf(&b, "keys    0x%08X = %v\n", k, buf)
	}
	if mn := r.AreaMapNavAddr(); mn != 0 {
		v, _ := p.ReadU8(mn + OffAreaMapUndiscoveredMap)
		fmt.Fprintf(&b, "mapnav  0x%08X UndiscoveredMapVisible=%d\n", mn, v)
	}
	if t := r.TimerAddr(); t != 0 {
		v, _ := p.ReadF32(t + OffTimerCurrentTime)
		fmt.Fprintf(&b, "timer   0x%08X CurrentTime=%.2f\n", t, v)
	}
	return b.String()
}

// DebugFieldOffset 按"字段名 + 声明类名"从 mono 元数据反查字段偏移。
//
// 原理同 findKlassByFieldName: 字段描述符布局为 {name*, klass*, offset}，
// 先按字段名字符串找到所有指向它的槽位，再读 +0x04 的 klass、校验类名，
// 命中后返回 (klass, offset)。
//
// ⚠ 字段名越常见（如 "Max"）越慢（每个出现位置都要做一次全空间槽位查找）。
func DebugFieldOffset(p *core.Process, fieldName, wantKlass string) (uint32, uint32) {
	needle := append([]byte(fieldName), 0)
	const chunk = 4 << 20
	buf := make([]byte, chunk)
	for _, reg := range p.ReadableRegions() {
		if reg.Base >= 0x40000000 {
			continue
		}
		for off := uint32(0); off < reg.Size; off += chunk {
			n := reg.Size - off
			if n > chunk {
				n = chunk
			}
			if n < uint32(len(needle)) {
				break
			}
			b := buf[:n]
			if !p.ReadBytes(reg.Base+off, b) {
				continue
			}
			from := 0
			for {
				j := bytes.Index(b[from:], needle)
				if j < 0 {
					break
				}
				sAddr := reg.Base + off + uint32(from+j)
				from += j + 1
				for _, h := range findSlotsWithValue(p, sAddr) {
					kl, ok := p.ReadU32(h + 4)
					if !ok || !isPtr(kl) {
						continue
					}
					ofs, ok2 := p.ReadU32(h + 8)
					if !ok2 || ofs > 0x4000 {
						continue
					}
					if nm, ok3 := klassNameOf(p, kl); ok3 && nm == wantKlass {
						return kl, ofs
					}
				}
			}
		}
	}
	return 0, 0
}

// DebugDumpField 打印一行字段偏移诊断结果（供 probe fieldoff 使用）。
func DebugDumpField(p *core.Process, klassName, fieldName string) string {
	k, o := DebugFieldOffset(p, fieldName, klassName)
	if k == 0 {
		return fmt.Sprintf("%-26s %-24s <未找到>\n", klassName, fieldName)
	}
	return fmt.Sprintf("%-26s %-24s +0x%X\n", klassName, fieldName, o)
}

// dumpFloats 以 float/int 双视角打印对象前 n 字节（每 4 字节一列），
// 用于人工判断某偏移上究竟是 float 还是 int/指针。
func dumpFloats(b *strings.Builder, p *core.Process, tag string, obj uint32, n uint32) {
	for off := uint32(0); off < n; off += 4 {
		v, _ := p.ReadF32(obj + off)
		u, _ := p.ReadU32(obj + off)
		fmt.Fprintf(b, "  %s+0x%02X: %.4f (0x%08X)\n", tag, off, v, u)
	}
}
