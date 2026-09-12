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
	"os"
	"sync"
	"time"

	"oritrainer/core"
)

// debugAux 为真时输出辅助定位诊断（设置环境变量 ORITRAINER_DEBUG=1）。
var debugAux = os.Getenv("ORITRAINER_DEBUG") != ""

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
	deathSlot      uint32 // 上述单例的静态槽地址（重读用）
	diffSlot       uint32

	// 定位诊断
	LastScanError string // 失败原因（UI 显示）
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
	r.deathSlot = 0
	r.diffSlot = 0
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

// ---------- 地址空间常量 ----------

// 关键教训（2026-09 实机）: 本进程是 32 位、带 /LARGEADDRESSAWARE 的 Unity mono
// 游戏，托管堆横跨 0x50xxxxxx-0x7Bxxxxxx（角色重生后会落到 0x73xxxxxx 这类
// 高地址）。早期实现把"堆指针"上限写成 0x70000000，导致玩家对象一旦分配到
// 高位就永远扫描不到——表现为"一开始能用，死亡重生后彻底失效"。
//
// 因此上界必须覆盖完整 32 位用户空间。
const (
	maxUserAddr = 0xFFFF0000 // 32 位用户空间上限（保守留出保留区）
	minObjAddr  = 0x40000000 // 托管堆对象下界（低区为代码/JIT/元数据）
)

// isHeapPtr 判断是否可能是 32 位托管堆对象指针。
func isHeapPtr(v uint32) bool {
	return v >= minObjAddr && v < maxUserAddr && v&3 == 0
}

// isPtr 判断是否可能是任意用户空间指针（vtable/klass 等，要求 4 字节对齐）。
func isPtr(v uint32) bool {
	return v >= 0x00010000 && v < maxUserAddr && v&3 == 0
}

// isTextPtr 判断是否可能是字符串地址。
//
// 不要求 4 字节对齐: mono 的名字符串在元数据区是紧凑拼接的，起始地址
// 往往不是 4 的倍数（实测 SeinDeathCounter 的名字在 0x2AFB8F4D）。若对
// 字符串指针做对齐校验，会导致所有类名解析静默失败。
func isTextPtr(v uint32) bool {
	return v >= 0x00010000 && v < maxUserAddr
}

// classOf 解析对象地址 -> (类型名, klass)。
//
// 对象布局: [0]=vtable -> klass, klass+0x30 = 类型名字符串指针。
//
// 判据（逐条实测校准）:
//  1. 对象本身在堆带（0x40000000-0x70000000）。
//  2. vtable 是合法指针。注意 vtable 可能位于两处: MonoBehaviour 派生类在
//     堆带（如 SeinCharacter 的 0x529CC60C），纯托管类在元数据带
//     （如 DifficultyController 的 0x2B4D7A30）—— 不能用地址带区分。
//  3. **klass 必须自指**: u32(klass) == klass。这是 mono MonoClass 的
//     固有签名（element_class/cast_class 指向自身），已验证 SeinCharacter、
//     SeinEnergy、SeinLevel、SeinDeathCounter、DifficultyController 五类全部满足；
//     而堆上垃圾数据偶然凑出的"假 klass"几乎不可能满足，实测用它排除了
//     所有假阳性。
func (r *Runtime) classOf(p *core.Process, obj uint32) (string, uint32, bool) {
	if !isHeapPtr(obj) {
		return "", 0, false
	}
	vt, ok := p.ReadU32(obj)
	if !ok || !isPtr(vt) {
		return "", 0, false
	}
	k, ok := p.ReadU32(vt)
	if !ok || !isPtr(k) || k == vt {
		return "", 0, false
	}
	// klass 自指校验（mono MonoClass 固有签名）
	if k0, ok := p.ReadU32(k); !ok || k0 != k {
		return "", 0, false
	}
	np, ok := p.ReadU32(k + 0x30)
	if !ok || !isTextPtr(np) {
		return "", 0, false
	}
	s := readIdent(p, np)
	if s == "" {
		return "", 0, false
	}
	return s, k, true
}

// readIdent 读取以 NUL 结尾且形如标识符的 ASCII 字符串（非标识符返回空）。
//
// 采用 4 字节步进读取并在遇到 NUL 时立即停止。不能一次性读固定 96 字节：
// 类名字符串常位于映射区末尾，越界读取会让整次 RPM 失败，从而解析不出
// 任何类名（实测导致 SeinDeathCounter 等单例定位失败）。
func readIdent(p *core.Process, addr uint32) string {
	var out []byte
	var b [4]byte
	for i := 0; i < 24; i++ { // 上限 96 字节
		if !p.ReadBytes(addr+uint32(i*4), b[:]) {
			return "" // 读取中断且未见 NUL → 视为无效
		}
		for _, c := range b {
			if c == 0 {
				if len(out) == 0 {
					return ""
				}
				return string(out)
			}
			ok := c == '_' || c == '.' || c == '<' || c == '>' || c == '`' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (len(out) > 0 && c >= '0' && c <= '9')
			if !ok {
				return ""
			}
			out = append(out, c)
		}
	}
	return "" // 超过上限未见 NUL
}

// ---------- 活体 Sein 定位 ----------

// validateSein 结构校验: 指针图完整且数值合理。
func validateSein(p *core.Process, v uint32) bool {
	if !isHeapPtr(v) {
		return false
	}
	vt, ok := p.ReadU32(v)
	if !ok || !isPtr(vt) {
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
// 两级策略:
//  1. 复用已缓存的"静态锚点槽"（游戏 Characters.Sein / Characters.Current
//     两个静态字段所在的相邻槽位），直接重读该槽 —— 毫秒级，且角色重生
//     后槽内容会指向新对象，自动跟上；
//  2. 锚点未知或失效时，在低地址区按 "[P, …, P]" 相邻双引用模式重新发现
//     锚点槽（低区扫描约 1 秒）；仍失败则全地址空间兜底。
func (r *Runtime) locateSein(p *core.Process) uint32 {
	// ① 锚点槽重读
	if v := r.seinFromAnchor(p); v != 0 {
		return v
	}
	// ② 低区模式扫描（mono 静态/分配区集中在低地址）
	if v, slot := r.scanSeinPattern(p, 0x10000000); v != 0 {
		r.seinAnchor = slot
		return v
	}
	// ③ 全地址空间兜底（慢，仅在极端情况触发）
	if v, slot := r.scanSeinPattern(p, 0x100000000); v != 0 {
		r.seinAnchor = slot
		return v
	}
	r.seinAnchor = 0
	return 0
}

// seinFromAnchor 重读缓存的锚点槽，返回其中有效的活体 SeinCharacter。
// 返回 0 表示锚点不可用，需要重新扫描。
func (r *Runtime) seinFromAnchor(p *core.Process) uint32 {
	a := r.seinAnchor
	if a == 0 {
		return 0
	}
	v, ok := p.ReadU32(a)
	if !ok || !validateSein(p, v) {
		return 0
	}
	// 优先信任"相邻双引用"仍在的槽（Characters.Sein + Current）
	if v2, ok2 := p.ReadU32(a + 12); ok2 && v2 == v {
		return v
	}
	// Current 可能已切到别的角色，退化为类名校验
	if nm, _, ok := r.classOf(p, v); ok && nm == "SeinCharacter" {
		return v
	}
	return 0
}

// scanSeinPattern 在低于 limit 的可读区域里扫描 "[P, …, P]"（间隔 12 字节的
// 同值堆指针）模式，返回 (对象地址, 槽位地址)。
//
// 采用 4MB 分块读取并保留 12 字节重叠，避免一次性分配整段区域
// （旧实现 make([]byte, 区域大小) 在堆变大后可致进程 OOM 退出）。
func (r *Runtime) scanSeinPattern(p *core.Process, limit uint64) (uint32, uint32) {
	const chunk = 4 << 20
	seen := map[uint32]bool{}
	var bestAddr, bestSlot uint32
	for _, reg := range p.ReadableRegions() {
		if uint64(reg.Base) >= limit {
			continue
		}
		buf := make([]byte, chunk+16)
		for off := uint32(0); off < reg.Size; off += chunk {
			n := reg.Size - off
			if n > chunk {
				n = chunk
			}
			if n < 16 {
				break
			}
			b := buf[:n]
			if !p.ReadBytes(reg.Base+off, b) {
				continue
			}
			for i := 0; i+16 <= len(b); i += 4 {
				v := u32at(b, i)
				if !isHeapPtr(v) || seen[v] {
					continue
				}
				if u32at(b, i+12) != v {
					continue
				}
				seen[v] = true
				if !validateSein(p, v) {
					continue
				}
				slot := reg.Base + off + uint32(i)
				if nm, _, ok := r.classOf(p, v); ok && nm == "SeinCharacter" {
					return v, slot
				}
				if bestAddr == 0 {
					bestAddr, bestSlot = v, slot
				}
			}
		}
	}
	return bestAddr, bestSlot
}

// ---------- 对象链刷新 ----------

// refreshChain 从活体 SeinCharacter 重新读取全部子对象地址。
// 场景切换/重生后子对象会重建，因此每次刷新都重读指针。
func (r *Runtime) refreshChain(p *core.Process, sein uint32) {
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

// Refresh 周期性维护：以游戏静态字段（锚点槽）为准重读活体对象，
// 再刷新对象链。返回是否已就绪。
//
// 重要: 每次都以锚点槽当前值为准，而不是长期信任缓存的对象指针。
// 角色死亡重生时游戏会创建新的 SeinCharacter 并改写静态字段；旧对象
// 内存仍然"看起来合法"（被 GC 回收前），若继续写它就会毫无效果。
func (r *Runtime) Refresh() bool {
	r.mu.Lock()
	p := r.Proc
	r.mu.Unlock()
	if p == nil || !p.Alive() {
		return false
	}

	// ① 以锚点槽为准重读；失效则完整重新定位
	sein := r.seinFromAnchor(p)
	if sein == 0 {
		sein = r.locateSein(p)
	}
	if sein == 0 {
		r.mu.Lock()
		r.SeinCharacter = 0
		r.SeinLevel = 0
		r.SoulFlame = 0
		r.SeinJump = 0
		r.DoubleJump = 0
		r.LastScanError = "未找到活体玩家对象（请进入存档后按 F12 重试）"
		r.mu.Unlock()
		return false
	}

	r.mu.Lock()
	changed := r.SeinCharacter != sein
	r.SeinCharacter = sein
	if changed {
		r.LastSeinFind = time.Now()
	}
	r.LastScanError = ""
	r.mu.Unlock()

	// ② 重读附属单例的静态槽（对象同样可能被重建/迁移到高地址）
	r.refreshAuxSlots(p)

	r.refreshChain(p, sein)
	r.mu.Lock()
	needAux := changed || r.DeathCounter == 0 || r.DiffController == 0
	r.mu.Unlock()
	if needAux {
		r.resolveAux(p, sein)
	}
	return true
}

// refreshAuxSlots 以已缓存的静态槽为准重读死亡计数/难度控制器；
// 槽失效时清空，交由 resolveAux 重新扫描。
func (r *Runtime) refreshAuxSlots(p *core.Process) {
	r.mu.Lock()
	ds, dfs := r.deathSlot, r.diffSlot
	r.mu.Unlock()

	if ds != 0 {
		if v := r.auxFromSlot(p, ds, "SeinDeathCounter"); v != 0 {
			r.mu.Lock()
			r.DeathCounter = v
			r.mu.Unlock()
		} else {
			r.mu.Lock()
			r.DeathCounter = 0
			r.deathSlot = 0
			r.mu.Unlock()
		}
	}
	if dfs != 0 {
		if v := r.auxFromSlot(p, dfs, "DifficultyController"); v != 0 {
			r.mu.Lock()
			r.DiffController = v
			r.mu.Unlock()
		} else {
			r.mu.Lock()
			r.DiffController = 0
			r.diffSlot = 0
			r.mu.Unlock()
		}
	}
}

// auxFromSlot 重读静态槽并确认其中对象仍属于期望类型。
func (r *Runtime) auxFromSlot(p *core.Process, slot uint32, want string) uint32 {
	v, ok := p.ReadU32(slot)
	if !ok || !isHeapPtr(v) {
		return 0
	}
	if nm, _, ok := r.classOf(p, v); !ok || nm != want {
		return 0
	}
	return v
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
func (r *Runtime) resolveAux(p *core.Process, sein uint32) {
	r.mu.Lock()
	if r.auxBusy {
		r.mu.Unlock()
		return
	}
	needDeath := r.DeathCounter == 0
	needDiff := r.DiffController == 0
	if !needDeath && !needDiff {
		r.mu.Unlock()
		return
	}
	if time.Since(r.auxTried) < r.auxBackoff() {
		r.mu.Unlock()
		return
	}
	r.auxTried = time.Now()
	r.auxBusy = true
	r.auxFails++
	r.mu.Unlock()

	go func() {
		// panic 兜底: 后台扫描协程若崩溃会终止整个进程。
		defer func() {
			recover()
			r.mu.Lock()
			r.auxBusy = false
			r.mu.Unlock()
		}()
		death, diff, dSlot, dfSlot := scanLowBandAux(p, sein)
		r.mu.Lock()
		gotAny := false
		if needDeath && death != 0 {
			r.DeathCounter = death
			r.deathSlot = dSlot
			gotAny = true
		}
		if needDiff && diff != 0 {
			r.DiffController = diff
			r.diffSlot = dfSlot
			gotAny = true
		}
		if gotAny {
			r.auxFails = 0
		}
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
func scanLowBandAux(p *core.Process, sein uint32) (death, diff, deathSlot, diffSlot uint32) {
	const chunk = 4 << 20
	cache := map[uint32]string{}
	classOfAt := func(obj uint32) string {
		if !isHeapPtr(obj) {
			return ""
		}
		vt, ok := p.ReadU32(obj)
		if !ok || !isPtr(vt) {
			return ""
		}
		if nm, ok := cache[vt]; ok {
			return nm
		}
		vk, ok := p.ReadU32(vt)
		nm := ""
		if ok && isPtr(vk) && vk != vt {
			if k0, ok3 := p.ReadU32(vk); ok3 && k0 == vk { // klass 自指
				if np, ok2 := p.ReadU32(vk + 0x30); ok2 && isTextPtr(np) {
					nm = readIdent(p, np)
				}
			}
		}
		cache[vt] = nm
		return nm
	}

	seen := map[uint32]bool{}
	var deathRef, deathAny, deathAnySlot, deathRefSlot uint32
	buf := make([]byte, chunk+16)
	for _, reg := range p.ReadableRegions() {
		if reg.Base >= 0x10000000 {
			continue
		}
		for off := uint32(0); off < reg.Size; off += chunk {
			n := reg.Size - off
			if n > chunk {
				n = chunk
			}
			if n < 4 {
				break
			}
			b := buf[:n]
			if !p.ReadBytes(reg.Base+off, b) {
				continue
			}
			for i := 0; i+4 <= len(b); i += 4 {
				v := u32at(b, i)
				if !isHeapPtr(v) || seen[v] {
					continue
				}
				seen[v] = true
				slot := reg.Base + off + uint32(i)
				if deathAny == 0 {
					if d, ok := p.ReadI32(v + OffDeathCounterValue); ok && d >= 0 && d <= 1000000 {
						if classOfAt(v) == "SeinDeathCounter" {
							if deathRef == 0 && sein != 0 {
								if ref, ok2 := p.ReadU32(v + 0x28); ok2 && ref == sein {
									deathRef, deathRefSlot = v, slot
								}
							}
							deathAny, deathAnySlot = v, slot
						}
					}
				}
				if diff == 0 {
					d1, ok1 := p.ReadI32(v + OffDiffDifficulty)
					d2, ok2 := p.ReadI32(v + OffDiffLowest)
					if ok1 && ok2 && d1 >= 0 && d1 <= 3 && d2 >= 0 && d2 <= 3 {
						if classOfAt(v) == "DifficultyController" {
							diff, diffSlot = v, slot
						}
					}
				}
			}
		}
	}
	if deathRef != 0 {
		death, deathSlot = deathRef, deathRefSlot
	} else {
		death, deathSlot = deathAny, deathAnySlot
	}
	if debugAux {
		fmt.Fprintf(os.Stderr, "[aux] sein=%08X cand=%d death=%08X slot=%08X diff=%08X dslot=%08X\n",
			sein, len(seen), death, deathSlot, diff, diffSlot)
	}
	return death, diff, deathSlot, diffSlot
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
