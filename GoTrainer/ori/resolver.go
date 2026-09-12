// Package ori —— 运行期对象解析（两版通用）。
//
// 设计要点（2026-09 重写）:
//
//	旧版做法是全堆盲目扫描 + 弱校验，结果锁定到"传送门克隆体"这类
//	已初始化但游戏不使用的副本 Sein 上（其静态引用为零），所有写入
//	自然全部无效。
//
//	新做法利用一个稳定的结构事实：游戏自己的静态字段
//
//	    Characters.Sein    （活体玩家）
//	    Characters.Current （同一个对象，紧随其后 12 字节）
//
//	在 mono 静态数据带（低地址 < 0x10000000）里形成
//	"[P, …, P]" 的相邻双引用。扫描该模式并做结构校验，即可在
//	0.5-1 秒内定位真正的活体玩家对象，随后所有子对象沿对象链直读：
//
//	    SeinCharacter +0x38 -> SeinLevel
//	                 +0x3C -> SeinEnergy
//	                 +0x40 -> SeinMortality -> +0x0C -> SeinHealthController
//	                 +0x28 -> SeinSoulFlame
//	                 +0x10 -> SeinAbilities -> +0x0C SeinJump / +0x08 SeinDoubleJump
//
//	死亡计数器与难度控制器用类名校验（读 vtable -> klass -> name）在
//	静态带内定位，后台异步进行，失败可重试且不阻塞 UI。
package ori

import (
	"fmt"
	"math"
	"sync"
	"time"

	"oritrainer/core"
)

// Runtime 保存一次会话内的解析结果与缓存。
type Runtime struct {
	mu sync.Mutex

	Prof *Profile
	Proc *core.Process

	// --- 活体玩家对象链（Refresh 持续维护）---
	SeinCharacter uint32 // Characters.Sein —— 唯一的活体玩家
	SeinLevel     uint32 // +0x38
	SoulFlame     uint32 // +0x28
	SeinJump      uint32 // Abilities(+0x10) +0x0C
	DoubleJump    uint32 // Abilities(+0x10) +0x08

	// --- 独立单例（后台异步定位，可重试）---
	DeathCounter   uint32 // SeinDeathCounter.Instance
	DiffController uint32 // DifficultyController.Instance

	// 定位诊断
	LastScanError string   // 失败原因（UI 显示）
	LastSeinFind  time.Time
	seinAnchor    uint32 // 上次命中的静态引用槽位（加速复用）
	auxBusy       bool
	auxTried      time.Time
	auxFails      int // 连续失败次数（指数退避用）
}

// Addrs 一次性取回全部已定位对象地址（加锁，供功能执行器使用）。
// 执行器不得直接读取 Runtime 的字段（后台 Refresh 会并发改写）。
func (r *Runtime) Addrs() (sein, level, soul, jump, dbl, death uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinCharacter, r.SeinLevel, r.SoulFlame, r.SeinJump, r.DoubleJump, r.DeathCounter
}

// SetProcess 绑定进程并重置解析状态。
func (r *Runtime) SetProcess(p *core.Process) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Proc = p
	r.SeinCharacter = 0
	r.SeinLevel = 0
	r.SoulFlame = 0
	r.SeinJump = 0
	r.DoubleJump = 0
	r.DeathCounter = 0
	r.DiffController = 0
	r.LastScanError = ""
	r.auxBusy = false
	r.auxTried = time.Time{}
	r.auxFails = 0
}

// HasSein 是否已定位活体玩家对象。
func (r *Runtime) HasSein() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinCharacter != 0 && r.SeinLevel != 0
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
	case "death":
		return r.DeathCounter
	case "diff":
		return r.DiffController
	case "level":
		return r.SeinLevel
	}
	return 0
}

// SeinCharacterAddr / SeinLevelAddr / DeathCounterAddr 诊断用。
func (r *Runtime) SeinCharacterAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinCharacter
}

func (r *Runtime) SeinLevelAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.SeinLevel
}

func (r *Runtime) DeathCounterAddr() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.DeathCounter
}

// ---------- 基础读取辅助 ----------

func u32at(buf []byte, off int) uint32 {
	return uint32(buf[off]) | uint32(buf[off+1])<<8 | uint32(buf[off+2])<<16 | uint32(buf[off+3])<<24
}

func f32at(buf []byte, off int) float32 {
	return math.Float32frombits(u32at(buf, off))
}

// isHeapPtr 判断是否可能是 32 位 mono 堆对象指针。
// 实测堆对象落在 0x4xxxxxxx-0x5xxxxxxx，静态/元数据在低区与 0x2A-0x2B 段。
func isHeapPtr(v uint32) bool {
	return v >= 0x40000000 && v < 0x70000000 && v&3 == 0
}

// classOf 解析对象地址 -> (类型名, klass)。
//
// 对象布局: [0]=vtable -> klass, klass+0x30 = 类型名字符串指针。
//
// 关键约束: 对象本身必须位于堆带（0x40000000-0x70000000）。mono 的类
// 元数据块（0x2A-0x2B 段）内部含指向自身 klass 的指针，若不排除，
// 会把元数据块误判成对象实例（实测踩到过）。
//
// 注意 vtable 本身**不能**限制在堆带: MonoBehaviour 派生类的 vtable
// 在堆上（如 SeinCharacter 的 0x529CC60C），而纯托管类的 vtable 在
// 元数据带（如 DifficultyController 的 0x2B4D7A30），两者都合法。
func (r *Runtime) classOf(p *core.Process, obj uint32) (string, uint32, bool) {
	if !isHeapPtr(obj) {
		return "", 0, false
	}
	vt, ok := p.ReadU32(obj)
	if !ok || vt < 0x08000000 || vt >= 0x70000000 || vt&3 != 0 {
		return "", 0, false
	}
	k, ok := p.ReadU32(vt)
	if !ok || k < 0x08000000 || k >= 0x70000000 || k == vt {
		return "", 0, false
	}
	np, ok := p.ReadU32(k + 0x30)
	if !ok || np < 0x08000000 {
		return "", 0, false
	}
	s := readIdent(p, np)
	if s == "" {
		return "", 0, false
	}
	return s, k, true
}

// readIdent 读取以 NUL 结尾且形如标识符的 ASCII 字符串（非标识符返回空）。
func readIdent(p *core.Process, addr uint32) string {
	buf := make([]byte, 96)
	if !p.ReadBytes(addr, buf) {
		return ""
	}
	n := 0
	for ; n < len(buf); n++ {
		c := buf[n]
		if c == 0 {
			break
		}
		ok := c == '_' || c == '.' || c == '<' || c == '>' || c == '`' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (n > 0 && c >= '0' && c <= '9')
		if !ok {
			return ""
		}
	}
	if n == 0 || n >= len(buf) {
		return ""
	}
	return string(buf[:n])
}

// ---------- 活体 Sein 定位 ----------

// validateSein 结构校验: 指针图完整且数值合理。
func validateSein(p *core.Process, v uint32) bool {
	if !isHeapPtr(v) {
		return false
	}
	vt, ok := p.ReadU32(v)
	if !ok || vt < 0x08000000 || vt >= 0x70000000 {
		return false
	}
	lvl, ok1 := p.ReadU32(v + OffSeinLevel)
	en, ok2 := p.ReadU32(v + OffSeinEnergy)
	mor, ok3 := p.ReadU32(v + OffSeinMortality)
	if !ok1 || !ok2 || !ok3 || !isHeapPtr(lvl) || !isHeapPtr(en) || !isHeapPtr(mor) {
		return false
	}
	// SeinLevel 回指本体
	if back, ok := p.ReadU32(lvl + OffLevelMSein); !ok || back != v {
		return false
	}
	// 能量: 0 <= Current <= Max, Max 合理
	cur, okc := p.ReadF32(en + OffEnergyCurrent)
	max, okm := p.ReadF32(en + OffEnergyMax)
	if !okc || !okm || cur < -0.01 || max < 0 || max > 1000 || cur > max+0.01 {
		return false
	}
	// 生命: 0 <= Amount <= MaxHealth, MaxHealth 合理
	h, okh := p.ReadU32(mor + OffMortalityHealth)
	if !okh || !isHeapPtr(h) {
		return false
	}
	amt, oka := p.ReadF32(h + OffHealthAmount)
	mh, okx := p.ReadI32(h + OffHealthMaxHealth)
	if !oka || !okx || amt < -0.01 || mh < 4 || mh > 400 || amt > float32(mh)+0.01 {
		return false
	}
	return true
}

// locateSein 定位活体 SeinCharacter。
//
// 首选: 低区（mono 静态数据带）中 [P, …, P] 相邻双引用模式 ——
// 对应 Characters.Sein 与 Characters.Current 两个静态字段；再做
// 结构校验与类名校验。
// 回退: 静态锚点（版本相关，作为快路径）。
func (r *Runtime) locateSein(p *core.Process) uint32 {
	// 快路径: 已知锚点仍有效则直接复用
	if a := r.seinAnchor; a != 0 {
		if v, ok := p.ReadU32(a); ok && validateSein(p, v) {
			if nm, _, ok2 := r.classOf(p, v); ok2 && nm == "SeinCharacter" {
				return v
			}
		}
	}

	// 主路径: 低区相邻双引用扫描
	type region struct {
		base uint32
		data []byte
	}
	var bands []region
	for _, reg := range p.ReadableRegions() {
		if reg.Base >= 0x10000000 {
			continue
		}
		buf := make([]byte, reg.Size)
		if !p.ReadBytes(reg.Base, buf) {
			continue
		}
		bands = append(bands, region{reg.Base, buf})
	}

	seen := map[uint32]bool{}
	var best uint32
	for _, b := range bands {
		for off := 0; off+16 <= len(b.data); off += 4 {
			v := u32at(b.data, off)
			if !isHeapPtr(v) {
				continue
			}
			if u32at(b.data, off+12) != v || seen[v] {
				continue
			}
			seen[v] = true
			if !validateSein(p, v) {
				continue
			}
			if nm, _, ok := r.classOf(p, v); ok && nm == "SeinCharacter" {
				r.seinAnchor = b.base + uint32(off)
				return v
			}
			if best == 0 && validateSein(p, v) {
				best = v
			}
		}
	}
	if best != 0 {
		r.seinAnchor = 0
		return best
	}
	return 0
}

// ---------- 对象链刷新 ----------

// refreshChain 从活体 SeinCharacter 重新读取全部子对象地址。
// 场景切换后子对象可能重建，因此每次刷新都重读指针。
func (r *Runtime) refreshChain(p *core.Process) {
	sein := r.SeinCharacter
	if sein == 0 || !validateSein(p, sein) {
		return
	}
	lvl, _ := p.ReadU32(sein + OffSeinLevel)
	soul, _ := p.ReadU32(sein + OffSeinSoulFlame)
	ab, _ := p.ReadU32(sein + OffSeinAbilities)

	var jump, dbl uint32
	if isHeapPtr(ab) {
		jump, _ = p.ReadU32(ab + OffAbilitiesJump)
		dbl, _ = p.ReadU32(ab + OffAbilitiesDoubleJump)
		if !isHeapPtr(jump) {
			jump = 0
		}
		if !isHeapPtr(dbl) {
			dbl = 0
		}
	}
	if !isHeapPtr(soul) {
		soul = 0
	}

	r.mu.Lock()
	r.SeinLevel = lvl
	r.SoulFlame = soul
	r.SeinJump = jump
	r.DoubleJump = dbl
	r.mu.Unlock()
}

// Refresh 周期性维护：确保活体 Sein 已定位并刷新对象链。
// 返回是否已就绪。
func (r *Runtime) Refresh() bool {
	r.mu.Lock()
	p := r.Proc
	cur := r.SeinCharacter
	r.mu.Unlock()
	if p == nil || !p.Alive() {
		return false
	}

	if cur != 0 && validateSein(p, cur) {
		r.refreshChain(p)
		r.mu.Lock()
		n := r.SeinCharacter
		r.mu.Unlock()
		if n != 0 {
			return true
		}
	}

	// 需要重新定位
	sein := r.locateSein(p)
	r.mu.Lock()
	r.SeinCharacter = sein
	if sein == 0 {
		r.SeinLevel = 0
		r.SoulFlame = 0
		r.SeinJump = 0
		r.DoubleJump = 0
		r.LastScanError = "未找到活体玩家对象（请进入存档后按 F12 重试）"
	} else {
		r.LastScanError = ""
		r.LastSeinFind = time.Now()
	}
	r.mu.Unlock()
	if sein == 0 {
		return false
	}
	r.refreshChain(p)
	r.resolveAux(p)
	return true
}

// ScanObjects 兼容旧调用点: 等价于 Refresh。
func (r *Runtime) ScanObjects() error {
	if r.Refresh() {
		return nil
	}
	return fmt.Errorf("定位失败")
}

// ---------- 单例定位（死亡计数 / 难度控制）----------

// resolveAux 后台定位死亡计数器与难度控制器（不阻塞主循环）。
//
// 节流采用指数退避: 连续失败时重试间隔 3s→6s→12s→…→60s。
// 这样主菜单等"对象尚未创建"的场景不会持续占用 CPU；一旦定位成功
// 或被 F12 重置（SetProcess），退避计数清零。
func (r *Runtime) resolveAux(p *core.Process) {
	r.mu.Lock()
	if r.auxBusy || time.Since(r.auxTried) < r.auxBackoff() {
		r.mu.Unlock()
		return
	}
	needDeath := r.DeathCounter == 0
	needDiff := r.DiffController == 0
	sein := r.SeinCharacter
	r.auxTried = time.Now()
	r.auxBusy = true
	if needDeath || needDiff {
		r.auxFails++
	}
	r.mu.Unlock()

	if !needDeath && !needDiff {
		r.mu.Lock()
		r.auxBusy = false
		r.mu.Unlock()
		return
	}

	go func() {
		death, diff := scanLowBandAux(p, sein)
		r.mu.Lock()
		gotAny := false
		if needDeath && death != 0 {
			r.DeathCounter = death
			gotAny = true
		}
		if needDiff && diff != 0 {
			r.DiffController = diff
			gotAny = true
		}
		if gotAny {
			r.auxFails = 0
		}
		r.auxBusy = false
		r.mu.Unlock()
	}()
}

// auxBackoff 当前重试间隔（指数退避，上限 60 秒）。须在持锁状态下调用。
func (r *Runtime) auxBackoff() time.Duration {
	d := 3 * time.Second
	for i := 0; i < r.auxFails && d < 60*time.Second; i++ {
		d *= 2
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// scanLowBandAux 在低区（静态数据带）扫描:
//   - SeinDeathCounter: 类名匹配 + m_deathCounter ∈ [0, 10^6]
//   - DifficultyController: 类名匹配 + Difficulty/Lowest ∈ [0,3]
//
// 多个候选时优先取引用活体 Sein 的那个（死亡计数）。
func scanLowBandAux(p *core.Process, sein uint32) (death, diff uint32) {
	type region struct {
		base uint32
		data []byte
	}
	var bands []region
	for _, reg := range p.ReadableRegions() {
		if reg.Base >= 0x10000000 {
			continue
		}
		buf := make([]byte, reg.Size)
		if !p.ReadBytes(reg.Base, buf) {
			continue
		}
		bands = append(bands, region{reg.Base, buf})
	}

	cache := map[uint32]string{}
	classOfAt := func(obj uint32) string {
		if !isHeapPtr(obj) {
			return ""
		}
		vt, ok := p.ReadU32(obj)
		if !ok || vt < 0x08000000 || vt >= 0x70000000 || vt&3 != 0 {
			return ""
		}
		if nm, ok := cache[vt]; ok {
			return nm
		}
		vk, ok := p.ReadU32(vt)
		nm := ""
		if ok && vk >= 0x08000000 && vk != vt {
			if np, ok2 := p.ReadU32(vk + 0x30); ok2 && np >= 0x08000000 {
				nm = readIdent(p, np)
			}
		}
		cache[vt] = nm
		return nm
	}

	seen := map[uint32]bool{}
	var deathRef, deathAny uint32
	for _, b := range bands {
		for off := 0; off+4 <= len(b.data); off += 4 {
			v := u32at(b.data, off)
			if !isHeapPtr(v) || seen[v] {
				continue
			}
			seen[v] = true
			if deathAny != 0 && diff != 0 {
				break
			}
			// 先做便宜的结构检查，再做类名解析
			if deathAny == 0 {
				if d, ok := p.ReadI32(v + OffDeathCounterValue); ok && d >= 0 && d <= 1000000 {
					if classOfAt(v) == "SeinDeathCounter" {
						if deathRef == 0 {
							if ref, ok2 := p.ReadU32(v + 0x28); ok2 && ref == sein {
								deathRef = v
							}
						}
						deathAny = v
					}
				}
			}
			if diff == 0 {
				d1, ok1 := p.ReadI32(v + OffDiffDifficulty)
				d2, ok2 := p.ReadI32(v + OffDiffLowest)
				if ok1 && ok2 && d1 >= 0 && d1 <= 3 && d2 >= 0 && d2 <= 3 {
					if classOfAt(v) == "DifficultyController" {
						diff = v
					}
				}
			}
		}
	}
	if deathRef != 0 {
		death = deathRef
	} else {
		death = deathAny
	}
	return death, diff
}

// ---------- 快照（TUI 渲染用）----------

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
	s.SeinOK = r.SeinCharacter != 0
	s.ScanError = r.LastScanError
	s.SeinLevel = r.SeinCharacter

	if r.DeathCounter != 0 {
		s.Deaths, _ = p.ReadI32(r.DeathCounter + OffDeathCounterValue)
	}
	if r.SeinLevel != 0 {
		s.SkillPoints, _ = p.ReadI32(r.SeinLevel + OffLevelSkillPoints)
		s.Experience, _ = p.ReadI32(r.SeinLevel + OffLevelExperience)
	}
	if r.SeinCharacter != 0 {
		if en, ok := p.ReadU32(r.SeinCharacter + OffSeinEnergy); ok && isHeapPtr(en) {
			s.EnergyCur, _ = p.ReadF32(en + OffEnergyCurrent)
			s.EnergyMax, _ = p.ReadF32(en + OffEnergyMax)
		}
		if mor, ok := p.ReadU32(r.SeinCharacter + OffSeinMortality); ok && isHeapPtr(mor) {
			if h, ok2 := p.ReadU32(mor + OffMortalityHealth); ok2 && isHeapPtr(h) {
				s.HealthCur, _ = p.ReadF32(h + OffHealthAmount)
				s.HealthMax, _ = p.ReadI32(h + OffHealthMaxHealth)
			}
		}
	}
	return s
}
