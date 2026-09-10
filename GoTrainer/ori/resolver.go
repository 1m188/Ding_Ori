// Package ori —— 运行期对象解析: 全堆两阶段扫描定位活体游戏对象（两版通用）。
package ori

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"oritrainer/core"
)

// Runtime 保存一次会话内的解析结果与缓存。
type Runtime struct {
	mu sync.Mutex

	Prof *Profile
	Proc *core.Process

	SeinLevel     uint32 // 活体 SeinLevel 实例（m_sein != 0）
	SeinCharacter uint32 // 活体 SeinCharacter（= SeinLevel.m_sein）
	DeathCounter  uint32 // SeinDeathCounter 实例
	DiffController uint32 // DifficultyController 实例（一命保护用）

	// 子状态对象（ScanSubObjects 用"回指 Sein"签名定位）
	SoulFlame  uint32 // SeinSoulFlame（+0x64 -> Sein）
	SeinJump   uint32 // SeinJump（+0x40 -> Sein）
	DoubleJump uint32 // SeinDoubleJump（+0x30 -> Sein）

	LastScanError string
}

// SubAddr 返回已定位子对象的地址（诊断/测试用）。
func (r *Runtime) SubAddr(which string) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch which {
	case "soulflame":
		return r.SoulFlame
	case "jump":
		return r.SeinJump
	case "doublejump":
		return r.DoubleJump
	}
	return 0
}

// SetProcess 绑定进程并重置解析状态。
func (r *Runtime) SetProcess(p *core.Process) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Proc = p
	r.SeinLevel = 0
	r.SeinCharacter = 0
	r.DeathCounter = 0
	r.DiffController = 0
	r.SoulFlame = 0
	r.SeinJump = 0
	r.DoubleJump = 0
	r.LastScanError = ""
}

// HasSein 是否已定位活体 Sein 对象。
func (r *Runtime) HasSein() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinLevel != 0 && r.SeinCharacter != 0
}

// DiffAddress 返回 DifficultyController.Difficulty 字段地址（一命保护用）。
// 注意: 只暴露 Difficulty 字段地址，调用方绝不应触碰 +0x1C 的 LowestDifficulty。
func (r *Runtime) DiffAddress() (uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.DiffController == 0 {
		return 0, false
	}
	return r.DiffController + OffDiffDifficulty, true
}

// LowestDiffAddress 返回 LowestDifficulty 字段地址（只读校验用）。
func (r *Runtime) LowestDiffAddress() (uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.DiffController == 0 {
		return 0, false
	}
	return r.DiffController + OffDiffLowest, true
}

// SlotsValid 兼容字段（历史遗留，现恒 true）。
func (r *Runtime) SlotsValid() bool { return true }

const chunk = 16 << 20

func u32at(buf []byte, off int) uint32 {
	return uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
}

// ScanObjects 全堆两阶段扫描:
//   - SeinLevel: 回指签名 + Energy/Health 双重加固验证
//   - SeinDeathCounter: 稳定魔数 u32(X+8)==0xFFFF18A6
func (r *Runtime) ScanObjects() error {
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
	type diffCand struct {
		x   uint32
		cls uint32
		d1  uint32
		d2  uint32
		del uint32
	}
	var lvlCands []lvlCand
	var deathCands []uint32
	var diffCands []diffCand

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
				if ms >= 0x1000000 {
					sp := u32at(buf, off+0x24)
					ex := u32at(buf, off+0x2C)
					if sp <= MaxSkillPoints && ex < MaxExperience {
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

				// --- DifficultyController 快筛（一命保护用）---
				// 结构（CE findInstances 权威 dump）:
				//   +0x00 类指针  +0x04==0  +0x08..+0x14 管理指针
				//   +0x18 Difficulty  +0x1C LowestDifficulty  +0x20 delegate
				//   +0x24==0  +0x28/+0x2C 管理指针
				// 强判别: +0x0C/+0x10/+0x14 必须是真实堆地址（>= 0x40000000），
				//   用来排除 3F800000/BF800000/FFFFFFFF 这类浮点常量位模式巧合。
				{
					d1 := u32at(buf, off+OffDiffDifficulty)
					d2 := u32at(buf, off+OffDiffLowest)
					if d1 <= DiffOneLife && d2 <= DiffOneLife &&
						u32at(buf, off+4) == 0 && u32at(buf, off) >= 0x20000000 &&
						u32at(buf, off+0x24) == 0 &&
						u32at(buf, off+0x0C) >= 0x40000000 &&
						u32at(buf, off+0x10) >= 0x40000000 &&
						u32at(buf, off+0x14) >= 0x40000000 &&
						u32at(buf, off+OffDiffDelegate) >= 0x20000000 {
						diffCands = append(diffCands, diffCand{
							x: x, cls: u32at(buf, off), d1: d1, d2: d2, del: u32at(buf, off+OffDiffDelegate),
						})
					}
				}
			}
		}
	}

	// --- 阶段2: SeinLevel 回指 + Energy/Health 加固 ---
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
		// 注意: Energy.Max 允许为 0 —— 游戏早期（尚未获得能量容器时）
		// 上限就是 0，硬性要求 >=1 会导致新存档扫描失败。
		mx, ok3 := p.ReadF32(en + OffEnergyMax)
		if !ok3 || mx < 0 || mx > EnergyMaxMax {
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
		if !ok6 || maxhp < 4 || maxhp > MaxHealthMax {
			continue
		}
		liveLevel, liveSein = c.x, c.ms
		break
	}

	// --- 阶段2: 死亡计数验证（+0x10 MoonGuid 指针有效性）---
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

	// --- 阶段2: DifficultyController 定位 ---
	// 首选: Instance 静态字段的真实存储地址（实测可直接读取，返回权威实例指针）。
	//   该地址由 CE AOB 反查"指向实例的引用"得到，与 CE 的 mono 视图一致。
	// 回退: 堆扫描候选（当静态槽因版本差异失效时使用）。
	var liveDiff uint32
	if inst, ok := p.ReadU32(StaticDiffController); ok && inst > 0x10000 {
		// 自检: 难度值必须合法，否则视为无效指针
		if d1, ok1 := p.ReadI32(inst + OffDiffDifficulty); ok1 && d1 >= 0 && d1 <= DiffOneLife {
			if d2, ok2 := p.ReadI32(inst + OffDiffLowest); ok2 && d2 >= 0 && d2 <= DiffOneLife {
				liveDiff = inst
			}
		}
	}
	if liveDiff == 0 && len(diffCands) > 0 {
		// 回退: 堆扫描（delegate 频次 + 堆地址约束）
		freq := map[uint32]int{}
		for _, c := range diffCands {
			freq[c.del]++
		}
		bestDel, bestN := uint32(0), 0
		for del, n := range freq {
			if n > bestN {
				bestDel, bestN = del, n
			}
		}
		for _, c := range diffCands {
			if c.del == bestDel && c.d1 == c.d2 {
				liveDiff = c.x
				break
			}
		}
		if liveDiff == 0 {
			for _, c := range diffCands {
				if c.del == bestDel {
					liveDiff = c.x
					break
				}
			}
		}
	}

	r.mu.Lock()
	r.SeinLevel = liveLevel
	r.SeinCharacter = liveSein
	r.DeathCounter = liveDeath
	r.DiffController = liveDiff
	if liveLevel == 0 {
		r.LastScanError = fmt.Sprintf("扫描完成未找到活体 SeinLevel（候选 %d）—— 请进入存档后 F12 重扫", len(lvlCands))
	} else {
		r.LastScanError = ""
	}
	r.mu.Unlock()

	if liveLevel == 0 {
		return fmt.Errorf("未找到活体 SeinLevel")
	}

	// 子状态对象（跳跃/二段跳/灵魂链接）—— 依赖 SeinCharacter 已定位
	r.ScanSubObjects()

	_ = start
	return nil
}

// ScanSubObjects 定位 SeinCharacter 的子状态对象。
// 方法: 这些对象的某个字段指回 SeinCharacter（回指签名），
// 再加物理参数合理性校验，纯外部定位、不依赖静态槽。
//
// 已知回指偏移（CE mono dissect 实测）:
//
//	SeinSoulFlame.m_sein    @ +0x64
//	SeinJump.Sein           @ +0x40
//	SeinDoubleJump.Sein     @ +0x30
func (r *Runtime) ScanSubObjects() {
	r.mu.Lock()
	p, sein := r.Proc, r.SeinCharacter
	r.mu.Unlock()
	if p == nil || sein == 0 {
		return
	}

	var soul, jump, dbl uint32

	for _, reg := range p.ReadableRegions() {
		for base := uint64(reg.Base); base < uint64(reg.Base)+uint64(reg.Size); base += chunk {
			sz := chunk
			if remain := int(uint64(reg.Base) + uint64(reg.Size) - base); sz > remain {
				sz = remain
			}
			buf := make([]byte, sz)
			if !p.ReadBytes(uint32(base), buf) {
				continue
			}
			for off := 0; off+0xB0 <= sz; off += 4 {
				x := uint32(base) + uint32(off)

				// SeinSoulFlame（严格判据，防假阳性）:
				//   m_sein@+0x64 == Sein
				//   CooldownDuration@+0xA8 ∈ [5,300]（类默认 60，实测 20）
				//   HoldDownDuration@+0x98 ∈ [0.1,10]（类默认 0.7）
				//   m_numberOfSoulFlamesCast@+0x90 <= 1000（计数值，排除垃圾数据）
				if soul == 0 && u32at(buf, off+0x64) == sein {
					cd := f32at(buf, off+0xA8)
					hd := f32at(buf, off+0x98)
					cast := u32at(buf, off+0x90)
					if cd >= 5 && cd <= 300 && hd >= 0.1 && hd <= 10 && cast <= 1000 {
						soul = x
					}
				}
				// SeinJump（严格判据）:
				//   Sein@+0x40 == Sein，且 4 个跳跃高度参数都在 [1,10]
				//   （类默认: Backflip=3, Crouch=4.5, First=3, Second=3.75）
				if jump == 0 && u32at(buf, off+0x40) == sein {
					a := f32at(buf, off+0x54)
					b := f32at(buf, off+0x58)
					c := f32at(buf, off+0x60)
					d := f32at(buf, off+0x70)
					inRange := func(v float32) bool { return v >= 1 && v <= 10 }
					if inRange(a) && inRange(b) && inRange(c) && inRange(d) {
						jump = x
					}
				}
				// SeinDoubleJump（严格判据，与 CE findInstances 权威值对齐）:
				//   Sein@+0x30 == Sein（本体的那个实例）
				//   JumpStrength@+0x38 == 10.0（类默认值，实测恒定）
				//   m_numberOfJumpsAvailable@+0x40 放宽到 <= 100000
				//     （"无限二段跳"功能会把它写成大值，不能作为上限约束）
				if dbl == 0 && u32at(buf, off+0x30) == sein {
					st := f32at(buf, off+0x38)
					if st >= 9.5 && st <= 10.5 {
						dbl = x
					}
				}
			}
		}
		if soul != 0 && jump != 0 && dbl != 0 {
			break
		}
	}

	r.mu.Lock()
	r.SoulFlame = soul
	r.SeinJump = jump
	r.DoubleJump = dbl
	r.mu.Unlock()
}

// f32at 从缓冲区读 float32。
func f32at(buf []byte, off int) float32 {
	bits := uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
	return math.Float32frombits(bits)
}

// Snapshot 一致性快照（TUI 渲染用）。
type Snapshot struct {
	Attached    bool
	Pid         uint32
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

// SeinCharacterAddr 返回已定位的 SeinCharacter 地址（测试/诊断用）。
func (r *Runtime) SeinCharacterAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinCharacter
}

// DeathCounterAddr 返回已定位的 SeinDeathCounter 地址（诊断/测试用）。
func (r *Runtime) DeathCounterAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.DeathCounter
}

// SeinLevelAddr 返回已定位的 SeinLevel 地址（诊断用）。
func (r *Runtime) SeinLevelAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinLevel
}
