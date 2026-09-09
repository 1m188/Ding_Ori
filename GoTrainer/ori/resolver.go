// Package ori —— 运行期对象解析: 全堆两阶段扫描定位活体游戏对象。
//
// 背景注记: CE 的 mono dissect 报告的"类静态槽地址"位于 MonoDataCollector
// 的注入层，外部读取时该地址区域为 MEM_FREE（已实测铁证），不可用。
// 因此本训练器采用纯外部方案:
//   1. SeinLevel: 回指签名扫描 —— 对象 X 满足 u32(X+0x20)=P(有效指针)
//      且 u32(P+0x38)==X（SeinCharacter.Level 回指），再以
//      Sein.Energy.Max ∈ [1,50] 与 Mortality.Health.MaxHealth ∈ [12,400]
//      双重验证挑出活体（排除教学/UI 副本）。
//   2. SeinDeathCounter: 稳定魔数签名 —— 对象 X 满足 u32(X+0x08)==0xFFFF18A6
//      且 u32(X+0x04)==0 且 u32(X+0x14) 为 [0,99999] 的死亡计数。
// 全堆扫描采用两阶段批处理（本地快筛 + 仅对候选做 RPM 验证），
// 实测 ~14 秒完成（含 800+MB 可读内存遍历）。
package ori

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"oritrainer/core"
)

// Runtime 保存一次会话内的解析结果与缓存。
type Runtime struct {
	mu sync.Mutex

	Proc *core.Process

	DeathSlotOK bool // 死亡计数对象已定位
	GCSlotOK    bool // 兼容字段: 无静态槽方案后恒 true（占位）

	SeinLevel     uint32 // 活体 SeinLevel 实例（m_sein != 0）
	SeinCharacter uint32 // 活体 SeinCharacter（= SeinLevel.m_sein）
	DeathCounter  uint32 // SeinDeathCounter 实例

	LastScanError string
}

// SetProcess 绑定进程并重置解析状态。
func (r *Runtime) SetProcess(p *core.Process) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Proc = p
	r.DeathSlotOK = false
	r.GCSlotOK = true
	r.SeinLevel = 0
	r.SeinCharacter = 0
	r.DeathCounter = 0
	r.LastScanError = ""
}

// ValidateStaticSlots 兼容保留: 现在直接报告 true（无静态槽依赖）。
func (r *Runtime) ValidateStaticSlots() {
	r.mu.Lock()
	r.GCSlotOK = true
	r.mu.Unlock()
}

// HasSein 是否已定位活体 Sein 对象。
func (r *Runtime) HasSein() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinLevel != 0 && r.SeinCharacter != 0
}

// SlotsValid 静态槽校验是否通过（兼容字段）。
func (r *Runtime) SlotsValid() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.GCSlotOK
}

const chunk = 16 << 20

func u32at(buf []byte, off int) uint32 {
	return uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
}

// ScanSeinObjects 全堆两阶段扫描: 定位活体 SeinLevel/SeinCharacter/SeinDeathCounter。
func (r *Runtime) ScanSeinObjects() error {
	r.mu.Lock()
	p := r.Proc
	r.mu.Unlock()
	if p == nil || !p.Alive() {
		return fmt.Errorf("未附加进程")
	}
	start := time.Now()

	regs := p.ReadableRegions()

	type lvlCand struct {
		x  uint32
		ms uint32
	}
	var lvlCands []lvlCand
	var deathCands []uint32

	for _, reg := range regs {
		for base := uint64(reg.Base); base < uint64(reg.Base)+uint64(reg.Size); base += chunk {
			sz := chunk
			if remain := int(uint64(reg.Base) + uint64(reg.Size) - base); sz > remain {
				sz = remain
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0x40 <= sz; off += 8 {
				x := uint32(base) + uint32(off)

				// --- SeinLevel 快筛 ---
				ms := u32at(buf, off+0x20)
				if ms >= 0x1000000 && ms <= 0x7FFFFFFF {
					sp := u32at(buf, off+0x24)
					ex := u32at(buf, off+0x2C)
					if sp <= 99 && ex < 1000000 {
						lvlCands = append(lvlCands, lvlCand{x: x, ms: ms})
					}
				}

				// --- SeinDeathCounter 快筛 ---
				if u32at(buf, off+0x08) == 0xFFFF18A6 && u32at(buf, off+0x04) == 0 {
					d := u32at(buf, off+0x14)
					if d <= 99999 {
						deathCands = append(deathCands, x)
					}
				}
			}
		}
	}

	// --- 阶段2: SeinLevel 回指 + 双重验证 ---
	sort.Slice(lvlCands, func(i, j int) bool { return lvlCands[i].ms < lvlCands[j].ms })
	var liveLevel, liveSein uint32
	for _, c := range lvlCands {
		if c.x == c.ms {
			continue
		}
		back, ok := p.ReadU32(c.ms + OffSeinLevel)
		if !ok || back != c.x {
			continue
		}
		en, ok2 := p.ReadU32(c.ms + OffSeinEnergy)
		if !ok2 || en == 0 {
			continue
		}
		mx, ok3 := p.ReadF32(en + OffEnergyMax)
		if !ok3 || mx < 1 || mx > 50 {
			continue
		}
		mor, ok4 := p.ReadU32(c.ms + OffSeinMortality)
		if !ok4 || mor == 0 {
			continue
		}
		h, ok5 := p.ReadU32(mor + OffMortalityHealth)
		if !ok5 || h == 0 {
			continue
		}
		maxhp, ok6 := p.ReadI32(h + OffHealthMaxHealth)
		if !ok6 || maxhp < 12 || maxhp > 400 {
			continue
		}
		liveLevel, liveSein = c.x, c.ms
		break
	}

	// --- 阶段2: 死亡计数验证（实例 +0x00 应为指向自身的类指针结构中的有效对象:
	// 用 +0x10 MoonGuid 指针可读性做验证）---
	var liveDeath uint32
	for _, x := range deathCands {
		guid, ok := p.ReadU32(x + 0x10)
		if !ok || guid < 0x1000000 {
			continue
		}
		if d, ok2 := p.ReadI32(x + OffDeathCounterValue); ok2 && d >= 0 {
			liveDeath = x
			break
		}
	}

	r.mu.Lock()
	r.SeinLevel = liveLevel
	r.SeinCharacter = liveSein
	r.DeathCounter = liveDeath
	r.DeathSlotOK = liveDeath != 0
	if liveLevel == 0 {
		r.LastScanError = fmt.Sprintf("堆扫描完成但未找到活体 SeinLevel（候选 %d 个）—— 请进入存档后按 F12 重扫", len(lvlCands))
	} else {
		r.LastScanError = ""
	}
	r.mu.Unlock()

	if liveLevel == 0 {
		return fmt.Errorf("未找到活体 SeinLevel")
	}
	_ = start
	return nil
}

// Snapshot 一致性快照（TUI 渲染用）。
type Snapshot struct {
	Attached    bool
	Pid         uint32
	SlotsOK     bool
	SeinOK      bool
	ScanError   string
	SeinLevel   uint32
	Deaths      int32
	SkillPoints int32
	Experience  int32
	EnergyCur   float32
	EnergyMax   float32
	HealthCur   float32
	HealthMax   int32
}

// Read 读取当前全部运行值（不改任何东西）。
func (r *Runtime) Read() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	var s Snapshot
	p := r.Proc
	if p == nil || !p.Alive() {
		return s
	}
	s.Attached = true
	s.Pid = p.Pid
	s.SlotsOK = r.DeathSlotOK && r.GCSlotOK
	s.SeinOK = r.SeinLevel != 0 && r.SeinCharacter != 0
	s.ScanError = r.LastScanError
	s.SeinLevel = r.SeinLevel

	if r.DeathCounter != 0 {
		s.Deaths, _ = p.ReadI32(r.DeathCounter + OffDeathCounterValue)
	}
	if r.SeinLevel != 0 {
		s.SkillPoints, _ = p.ReadI32(r.SeinLevel + OffLevelSkillPoints)
		s.Experience, _ = p.ReadI32(r.SeinLevel + OffLevelExperience)
	}
	if r.SeinCharacter != 0 {
		if en, ok := p.ReadU32(r.SeinCharacter + OffSeinEnergy); ok && en != 0 {
			s.EnergyCur, _ = p.ReadF32(en + OffEnergyCurrent)
			s.EnergyMax, _ = p.ReadF32(en + OffEnergyMax)
		}
		if mor, ok := p.ReadU32(r.SeinCharacter + OffSeinMortality); ok && mor != 0 {
			if h, ok2 := p.ReadU32(mor + OffMortalityHealth); ok2 && h != 0 {
				s.HealthCur, _ = p.ReadF32(h + OffHealthAmount)
				s.HealthMax, _ = p.ReadI32(h + OffHealthMaxHealth)
			}
		}
	}
	return s
}
