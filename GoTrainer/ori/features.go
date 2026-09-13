// Package ori —— 功能实现: 冻结 / 归零 / 倍率 / 计数锁定 四种模式。
//
// 键位布局对齐风灵月影《奥日与黑暗森林:终极版》v1.0 Plus 13 修改器。
// 所有字段偏移经 CE mono dissect 实测（DE v1.0, Assembly-CSharp.dll）。
package ori

import (
	"fmt"
	"math"

	"sync"
	"sync/atomic"
)

// Feature 一个可开关的功能。
//
// 键位约定（只用小键盘，避免与笔记本键盘的 F 键/主键盘区冲突）。
// 现只用**两档**：
//   - NeedCtrl=false: 小键盘数字键 Digit（1..9, 0）—— 普通/基础功能
//   - NeedCtrl=true : Ctrl + 小键盘数字键 Digit —— 特殊功能
//
// NeedShift 字段保留，但没有功能使用（曾用于 Ctrl+Shift 档，因 Windows
// Terminal 默认占用 Ctrl+Shift+1..9 而放弃；见 README §8-23）。
// 档位互斥仍由"修饰键状态精确匹配"保证：按 Ctrl+N 时 NeedCtrl=false 的
// 功能不会触发；按住 Shift 时任何 Ctrl 档功能也不会触发。
//
// 另支持"前台导航"：修改器窗口在前台时，用 ↑↓ 选择、回车/空格切换
// （见 UI 层的 navCursor），供没有小键盘的键盘使用。
type Feature struct {
	Digit     int  // 1..9 或 0（小键盘数字键 0）
	NeedCtrl  bool // 是否需按住 Ctrl
	NeedShift bool // 是否需按住 Shift（当前无功能使用）
	Name      string

	// FixedTarget 非 nil 表示"固定目标值"模式（UI 显示用）
	FixedTarget *int32

	active atomic.Bool
	capI   atomic.Int32
	capF   atomic.Value // float32
	have   atomic.Bool
	status atomic.Value // string
}

// NewFeature 小键盘数字键功能（普通/基础功能）。
func NewFeature(digit int, name string) *Feature {
	f := &Feature{Digit: digit, Name: name}
	f.status.Store("未激活")
	return f
}

// NewCtrlFeature Ctrl + 小键盘数字键功能（特殊功能）。
func NewCtrlFeature(digit int, name string) *Feature {
	f := &Feature{Digit: digit, NeedCtrl: true, Name: name}
	f.status.Store("未激活")
	return f
}

// HotkeyLabel 键位显示文本。
func (f *Feature) HotkeyLabel() string {
	switch {
	case f.NeedCtrl && f.NeedShift:
		return fmt.Sprintf("Ctrl+Shift+小键盘 %d", f.Digit)
	case f.NeedCtrl:
		return fmt.Sprintf("Ctrl+小键盘 %d", f.Digit)
	default:
		return fmt.Sprintf("小键盘 %d", f.Digit)
	}
}

// Active 是否激活。
func (f *Feature) Active() bool { return f.active.Load() }

// SetActive 开关（关闭时清捕获状态）。
func (f *Feature) SetActive(on bool) {
	f.active.Store(on)
	if !on {
		f.have.Store(false)
		f.status.Store("未激活")
	}
}

// Status 状态文本。
func (f *Feature) Status() string { return f.status.Load().(string) }

func (f *Feature) setStatus(s string) { f.status.Store(s) }

// ---------- 执行器 ----------

type ticker interface{ Tick(r *Runtime) }

// reverter 关闭时需要还原原值的执行器。
type reverter interface{ OnDeactivate(r *Runtime) }

// freezeInt 冻结整型: fixed==nil 时捕获激活瞬间的值并保持；否则恒写 fixed。
type freezeInt struct {
	f     *Feature
	get   func(r *Runtime) (uint32, bool)
	fixed *int32
}

func (g *freezeInt) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		g.f.have.Store(false)
		return
	}
	var v int32
	if g.fixed != nil {
		v = *g.fixed
	} else {
		if !g.f.have.Load() {
			cur, ok2 := r.Proc.ReadI32(addr)
			if !ok2 {
				g.f.setStatus("读取失败")
				return
			}
			g.f.capI.Store(cur)
			g.f.have.Store(true)
		}
		v = g.f.capI.Load()
	}
	if r.Proc.WriteI32(addr, v) {
		g.f.setStatus(fmt.Sprintf("已锁定 = %d", v))
	} else {
		g.f.setStatus("写入失败")
	}
}

// freezeMaxFloat 「无限生命 / 无限能量」语义: 每周期把当前值拉满到上限。
//
// 与 freezeFloat（捕获激活瞬间的值并保持）的区别: 即使激活时当前值很低
// （残血、空能量），也会立即补满并持续保持，符合"无限"的直觉语义。
//
//	isInt=true  -> 上限是 int32（SeinHealthController.MaxHealth），写入 float
//	               （Amount 是 float，需转换）
//	isInt=false -> 上限是 float32（SeinEnergy.Max），直接写入
type freezeMaxFloat struct {
	f      *Feature
	get    func(r *Runtime) (uint32, bool) // 当前值字段
	getMax func(r *Runtime) (uint32, bool) // 上限字段
	isInt  bool
	// 仅用于状态显示: 内存值/scale + unit。生命以"点"存储、1 球 = 4 点，
	// 界面按球显示才与游戏一致；能量原样显示。
	scale float32
	unit  string
}

func (g *freezeMaxFloat) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	maxAddr, ok := g.getMax(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	var target float32
	if g.isInt {
		mv, ok := r.Proc.ReadI32(maxAddr)
		if !ok || mv <= 0 {
			g.f.setStatus("读取上限失败")
			return
		}
		target = float32(mv)
	} else {
		mv, ok := r.Proc.ReadF32(maxAddr)
		if !ok || mv < 0 {
			g.f.setStatus("读取上限失败")
			return
		}
		target = mv
	}
	if target <= 0 {
		// 上限本身为 0（例如角色尚未获得能量容器）: 补满没有意义，明确提示，
		// 避免误以为功能失效。
		g.f.setStatus("上限为 0，暂无可补满的量")
		return
	}
	if r.Proc.WriteF32(addr, target) {
		sc := g.scale
		if sc <= 0 {
			sc = 1
		}
		g.f.setStatus(fmt.Sprintf("已补满 = %g%s", target/sc, g.unit))
	} else {
		g.f.setStatus("写入失败")
	}
}

// freezeFloat 冻结浮点（语义同 freezeInt）。
type freezeFloat struct {
	f   *Feature
	get func(r *Runtime) (uint32, bool)
}

func (g *freezeFloat) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		g.f.have.Store(false)
		return
	}
	if !g.f.have.Load() {
		cur, ok2 := r.Proc.ReadF32(addr)
		if !ok2 {
			g.f.setStatus("读取失败")
			return
		}
		g.f.capF.Store(cur)
		g.f.have.Store(true)
	}
	v := g.f.capF.Load().(float32)
	if r.Proc.WriteF32(addr, v) {
		g.f.setStatus(fmt.Sprintf("已冻结 = %.2f", v))
	} else {
		g.f.setStatus("写入失败")
	}
}

// zeroFloat 恒写 0（灵魂链接冷却）。
type zeroFloat struct {
	f   *Feature
	get func(r *Runtime) (uint32, bool)
}

func (g *zeroFloat) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	if cur, ok2 := r.Proc.ReadF32(addr); ok2 && cur == 0 {
		g.f.setStatus("冷却已归零 ✓")
		return
	}
	if r.Proc.WriteF32(addr, 0) {
		g.f.setStatus("冷却已归零 ✓")
	} else {
		g.f.setStatus("写入失败")
	}
}

// setFloat 倍率放大: 激活时记录原值，持续写入 原值*Multiplier；关闭时还原。
// 用于超级跳/超级速度这类"放大既有参数"的功能。
type setFloat struct {
	f          *Feature
	get        func(r *Runtime) (uint32, bool)
	Multiplier float32
	orig       atomic.Value // float32
}

func (g *setFloat) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	if !g.f.have.Load() {
		cur, ok2 := r.Proc.ReadF32(addr)
		if !ok2 {
			g.f.setStatus("读取失败")
			return
		}
		g.orig.Store(cur)
		g.f.have.Store(true)
	}
	base := g.orig.Load().(float32)
	target := base * g.Multiplier
	if r.Proc.WriteF32(addr, target) {
		g.f.setStatus(fmt.Sprintf("已放大 %.0fx (%.2f→%.2f)", g.Multiplier, base, target))
	} else {
		g.f.setStatus("写入失败")
	}
}

func (g *setFloat) OnDeactivate(r *Runtime) {
	if !g.f.have.Load() {
		return
	}
	if addr, ok := g.get(r); ok {
		if orig, ok2 := g.orig.Load().(float32); ok2 {
			r.Proc.WriteF32(addr, orig)
		}
	}
	g.f.have.Store(false)
}

// setInt 恒写固定整数（无限二段跳等计数器）。
type setInt struct {
	f      *Feature
	get    func(r *Runtime) (uint32, bool)
	Target int32
}

func (g *setInt) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	if r.Proc.WriteI32(addr, g.Target) {
		g.f.setStatus(fmt.Sprintf("已锁定 = %d", g.Target))
	} else {
		g.f.setStatus("写入失败")
	}
}

// soulFlameAnywhere 「不安全区域也可建立灵魂链接」。
//
// 源码依据（DE v1.0 反编译，Assembly-CSharp.dll）:
//
//	// HandleCharging(): 蓄力只在"安全区域"判定通过时累加
//	if (m_isCasting && CanAffordSoulFlame && IsSafeToCastSoulFlame == Safe && ...)
//	    m_holdDownTime += Time.deltaTime / HoldDownDuration;
//	else
//	    m_holdDownTime -= Time.deltaTime / HoldDownDuration;   // 不安全时【回退】
//
//	// UpdateCharacterState(): 施放条件【不含任何安全检查】
//	if (m_holdDownTime == 1f && m_sein.IsOnGround && m_delayOnGround == 0f)
//	    CastSoulFlame();
//
// 即 IsSafeToCastSoulFlame（7 项判定：禁区/黑暗/存档台/敌人/重生点/无敌/地面射线）
// 只用于控制蓄力是否累加，真正的施放不检查安全性。
//
// ⚠ 只把 m_holdDownTime 写 1.0 是不够的（曾经这样实现，表现为"偶尔能建、多数不行"）:
// HandleCharging() 与施放判定在**同一帧内、且回退在前**，不安全区每帧都会把
// 我们写进去的 1.0 扣掉一点，判定 `== 1f` 就命中不了。
//
// 修法:
//  1. 按住期间把 HoldDownDuration 置为 **+Inf** → 回退量 delta/Inf = 0，
//     float32 下 1.0f 保持不变，1.0 能撑到施放判定（松开即还原原值）。
//  2. **一次按键只施放一次**：施放判定不含冷却/安全性，若每帧都写 1.0，
//     CastSoulFlame() 会被连续触发（它内部会 PerformSave + 计数 +1）→ 刷存档。
//     因此以 m_isCasting 的"按下沿"武装，观察到 m_holdDownTime 被清 0
//     （= CastSoulFlame 已执行）后本按键就不再写，直到松开再武装。
//  3. 轻点窗口（m_tapRemainingTime > 0）内绝不干预，保留"轻点开技能树"。
type soulFlameAnywhere struct {
	f        *Feature
	armed    bool    // 本次按住已写入 1.0，等待游戏施放
	castDone bool    // 本次按住已完成一次施放
	infDur   bool    // HoldDownDuration 当前是否被我们置为 +Inf
	durObj   uint32  // 被改写 HoldDownDuration 的 SeinSoulFlame 地址
	durOrig  float32 // HoldDownDuration 原值
}

// restoreDur 还原 HoldDownDuration（仅当对象未变，避免把旧值写到重建后的新对象）。
func (g *soulFlameAnywhere) restoreDur(r *Runtime) {
	if !g.infDur {
		return
	}
	if sf := r.SubAddr("soulflame"); sf != 0 && sf == g.durObj {
		r.Proc.WriteF32(sf+OffSoulFlameHoldDownDur, g.durOrig)
	}
	g.infDur = false
}

// OnDeactivate 关闭功能时还原 HoldDownDuration。
func (g *soulFlameAnywhere) OnDeactivate(r *Runtime) {
	g.restoreDur(r)
	g.armed, g.castDone = false, false
}

func (g *soulFlameAnywhere) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	sf := r.SubAddr("soulflame")
	if sf == 0 {
		g.f.setStatus("等待定位灵魂链接模块…")
		return
	}
	p := r.Proc

	// 对象被重建（重生/换场景）→ 之前改的是旧对象，放弃还原并重置状态
	if g.durObj != 0 && sf != g.durObj {
		g.infDur, g.armed, g.castDone = false, false, false
	}

	// 前置: 玩家正在按链接键（m_isCasting）
	flag, ok := p.ReadU8(sf + OffSoulFlameCastFlag)
	if !ok {
		g.f.setStatus("读取失败")
		return
	}
	if flag == 0 {
		// 松开: 还原蓄力时长，重置"本次按住"状态
		g.restoreDur(r)
		g.armed, g.castDone = false, false
		g.f.setStatus("待命（按住链接键时生效）")
		return
	}
	if g.castDone {
		g.f.setStatus("本次按键已建立链接（松开后可再建）")
		return
	}
	// 正在等待游戏施放: CastSoulFlame 会把 m_holdDownTime 清 0
	if g.armed {
		if cur, ok := p.ReadF32(sf + OffSoulFlameHoldDown); ok && cur == 0 {
			g.castDone = true
			g.f.setStatus("已建立链接 ✓（不安全区域/不稳定地面）")
		} else {
			g.f.setStatus("等待落地施放…（需站在地面上）")
		}
		return
	}
	// 轻点窗口内不干预（保护"轻点开技能树"）
	if tap, ok := p.ReadF32(sf + OffSoulFlameTapRemaining); ok && tap > 0 {
		g.f.setStatus("轻点窗口内（松开即开技能树）")
		return
	}

	// 武装本次长按: 抑制蓄力回退 + 清落地延迟 + 写满蓄力
	if !g.infDur {
		if orig, ok := p.ReadF32(sf + OffSoulFlameHoldDownDur); ok {
			// 防御: 若上次是"按住期间终端被关闭"残留的 +Inf/异常值，回退到默认
			// 0.5，避免把异常值当成"原值"再还原（否则会永久卡在 +Inf，蓄力再也满不了）。
			if math.IsInf(float64(orig), 0) || math.IsNaN(float64(orig)) || orig <= 0 {
				orig = 0.5
			}
			g.durObj, g.durOrig = sf, orig
			if p.WriteF32(sf+OffSoulFlameHoldDownDur, float32(math.Inf(1))) {
				g.infDur = true
			}
		}
	}
	p.WriteF32(sf+OffSoulFlameDelayOnGround, 0)
	if p.WriteF32(sf+OffSoulFlameHoldDown, 1.0) {
		g.armed = true
		g.f.setStatus("已强制蓄力 ✓ 等待施放…")
	} else {
		g.f.setStatus("写入失败")
	}
}

type OneLifeProtect struct {
	active atomic.Bool
	locked atomic.Bool  // 是否已成功锁定
	status atomic.Value // string
	lastLo atomic.Int32 // 最近读到的 LowestDifficulty（校验用）
}

// NewOneLifeProtect 创建一命保护。
func NewOneLifeProtect() *OneLifeProtect {
	o := &OneLifeProtect{}
	o.status.Store("未激活")
	return o
}

// Active 是否激活。
func (o *OneLifeProtect) Active() bool { return o.active.Load() }

// SetActive 开关。
func (o *OneLifeProtect) SetActive(on bool) {
	o.active.Store(on)
	if !on {
		o.locked.Store(false)
		o.status.Store("未激活")
	}
}

// Status 状态文本。
func (o *OneLifeProtect) Status() string { return o.status.Load().(string) }

// Name 功能名。
func (o *OneLifeProtect) Name() string { return "一命保护（死亡不清档）" }

// Tick 每周期执行: 若这是"一命"存档（LowestDifficulty == OneLife），
// 把当前 Difficulty 顶成 Easy。原理:
//
//   - 死亡时游戏读的是 `DifficultyController.Instance.Difficulty`，等于
//     OneLife 才会执行「WasKilled=true → 立即存档 → 删备份 → GameOver」，
//     并在下次读档时清档。把它变成 Easy，这条链整条不成立，死亡退化为
//     普通模式（在存档点复活）。
//   - Easy 顺带降低难度：非岩浆/尖刺伤害减半、敌人血量 ×0.65
//     （SeinDamageReciever 的难度 switch / Enemy.ScaleHealth，只有
//     Easy/Hard 有特殊分支；OneLife 与 Normal 走默认，伤害相同）。
//   - **绝不写 LowestDifficulty(+0x1C)**：成就是否算"一命通关"只读它
//     （BeatOneLifeAchievementAsset），写它会毁掉成就资格。
//   - 只在 `LowestDifficulty == OneLife` 时介入，普通/困难存档不受影响。
//
// 关闭功能时**不还原**难度（保护是"粘性"的）：避免误关一下就恢复一命。
func (o *OneLifeProtect) Tick(r *Runtime) {
	if !o.active.Load() {
		return
	}
	addr, ok := r.DiffAddress()
	if !ok {
		o.status.Store("等待定位难度控制器…")
		o.locked.Store(false)
		return
	}
	low, okL := r.Proc.ReadI32(addr + (OffDiffLowest - OffDiffDifficulty))
	if !okL {
		o.status.Store("读取失败")
		return
	}
	o.lastLo.Store(low)

	// 仅在一命存档下介入（其他难度无需保护，也避免改动玩家选择的难度）
	if low != DiffOneLife {
		o.status.Store(fmt.Sprintf("当前非一命难度(Lowest=%d)，未介入", low))
		o.locked.Store(false)
		return
	}

	cur, okC := r.Proc.ReadI32(addr)
	if !okC {
		o.status.Store("读取失败")
		return
	}
	if cur == DiffEasy {
		o.locked.Store(true)
		o.status.Store(fmt.Sprintf("保护中 ✓ 难度=Easy（Lowest=%d 一命记录已保留）", low))
		return
	}
	// cur 可能是 OneLife（刚读档/重开一命）或游戏重置成的 Normal —— 一律顶成 Easy。
	if r.Proc.WriteI32(addr, DiffEasy) {
		o.locked.Store(true)
		o.status.Store("已锁定 ✓ 死亡正常复活；难度=Easy（伤害减半/敌人血量降低）")
	} else {
		o.status.Store("写入失败")
	}
}

// ResetForNewProcess 重置状态。
func (o *OneLifeProtect) ResetForNewProcess() {
	o.locked.Store(false)
	if o.active.Load() {
		o.status.Store("等待定位难度控制器…")
	}
}

// ---------- 全局状态 ----------

var (
	// tickersMu 保护 allTickers / featureActivators。
	//
	// 必要性: 用户在"版本选择"与"修改器界面"之间来回切换时，新会话的
	// BuildFeatures 会重建这两个全局对象，而上一会话的功能引擎协程可能
	// 仍在 TickAll 中遍历旧切片。并发读写切片头会导致越界 panic 并让
	// 整个进程静默退出（实测症状: 修改器无故从任务栏消失）。
	tickersMu sync.RWMutex

	allTickers []ticker
	oneLife    = NewOneLifeProtect() // 一命保护（Ctrl+小键盘 6）

	// featureActivators 记录需要自定义激活/关闭副作用的执行器，
	// 由 UI 层通过 ActivateFeature/DeactivateFeature 调用。
	featureActivators = map[*Feature]func(on bool){}

	// activeRuntime 当前会话的 Runtime。UI 层在创建会话后调用
	// SetActiveRuntime 设置。
	activeRuntime *Runtime
)

// SetActiveRuntime 由 UI 层在建立会话时调用，供互斥逻辑还原数值。
func SetActiveRuntime(r *Runtime) {
	tickersMu.Lock()
	activeRuntime = r
	tickersMu.Unlock()
}

// OneLife 返回一命保护实例。
func OneLife() *OneLifeProtect { return oneLife }

// activatorFor 取某功能的自定义激活副作用（加锁读）。
func activatorFor(f *Feature) (func(bool), bool) {
	tickersMu.RLock()
	defer tickersMu.RUnlock()
	fn, ok := featureActivators[f]
	return fn, ok
}

// ActivateFeature 激活功能（先设置状态，再执行器特定的激活副作用）。
func ActivateFeature(f *Feature) {
	f.SetActive(true)
	if act, ok := activatorFor(f); ok {
		act(true)
	}
}

// DeactivateFeature 关闭功能。
// 顺序要求: 先还原原值（倍率型），再清状态，最后回滚激活副作用。
func DeactivateFeature(f *Feature, r *Runtime) {
	if !f.Active() {
		return
	}
	// 可还原型执行器: setFloat / multiFloatMul（倍率放大）、
	// infiniteDoubleJump（能力开关）、soulFlameAnywhere（HoldDownDuration）。
	// 还原要写回游戏内存，必须确认进程仍绑定（F12 会临时 SetProcess(nil)），
	// 否则 OnDeactivate 里的 r.Proc.* 会空指针 panic。
	canRevert := r != nil && r.HasProcess()
	tickersMu.RLock()
	snapshot := allTickers
	tickersMu.RUnlock()
	for _, t := range snapshot {
		switch ex := t.(type) {
		case *setFloat:
			if ex.f != f {
				continue
			}
			if canRevert {
				ex.OnDeactivate(r)
			} else {
				ex.f.have.Store(false)
			}
			goto done
		case *multiFloatMul:
			if ex.f != f {
				continue
			}
			if canRevert {
				ex.OnDeactivate(r)
			}
			goto done
		case *infiniteDoubleJump:
			if ex.f != f {
				continue
			}
			if canRevert {
				ex.OnDeactivate(r)
			}
			goto done
		case *soulFlameAnywhere:
			if ex.f != f {
				continue
			}
			if canRevert {
				ex.OnDeactivate(r)
			}
			goto done
		}
	}
done:
	f.SetActive(false)
	if act, ok := activatorFor(f); ok {
		act(false)
	}
}

// TickAll 驱动所有激活中的功能。
//
// 先对执行器列表做快照再遍历: BuildFeatures 可能在另一协程重建该列表
// （用户切换会话时），直接遍历全局切片会数据竞争。
func TickAll(r *Runtime) {
	tickersMu.RLock()
	snapshot := allTickers
	tickersMu.RUnlock()
	for _, t := range snapshot {
		t.Tick(r)
	}
	oneLife.Tick(r)
}

// DeactivateAll 关闭全部（含需要还原参数的功能）。
func DeactivateAll(fs []*Feature, r *Runtime) {
	for _, f := range fs {
		if !f.Active() {
			continue
		}
		DeactivateFeature(f, r)
	}
	oneLife.SetActive(false)
}

// ---------- 能力点数：不足则补满 ----------

// setIntMin 「不足才补满」整数执行器。
//
// 语义: 当前值 < target 时写入 target；已达到或超过则**不动**。
//
// 为什么不持续写: 游戏在升级时会自行 SkillPoints++（见 SeinLevel.LevelUp），
// 若每周期强行写回同一个值，会与游戏的结算反复互相覆盖，表现为点数持续
// 变化并反复触发升级动画。改为"不足才补"后，只在被消耗（学技能）时补满。
type setIntMin struct {
	f      *Feature
	get    func(r *Runtime) (uint32, bool)
	target int32
}

func (g *setIntMin) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	addr, ok := g.get(r)
	if !ok {
		g.f.setStatus("地址解析失败")
		return
	}
	cur, ok := r.Proc.ReadI32(addr)
	if !ok {
		g.f.setStatus("读取失败")
		return
	}
	if cur >= g.target {
		g.f.setStatus(fmt.Sprintf("已就绪 = %d", cur))
		return
	}
	if r.Proc.WriteI32(addr, g.target) {
		g.f.setStatus(fmt.Sprintf("已补满 %d → %d", cur, g.target))
	} else {
		g.f.setStatus("写入失败")
	}
}

// ---------- 多字段倍率（超级跳）----------

// multiFloatMul 同时放大多个浮点字段，关闭时逐个还原。
//
// 为什么需要多个字段: 跳跃高度不是单一变量。PerformJump 会按情形分支——
//   - 移动中跳: 依次轮换 FirstJumpHeight / SecondJumpHeight / ThirdJumpHeight
//   - 站立跳:   同样轮换这三个
//   - 贴墙下滑跳 / 蹲跳 / 转身后空翻: 另用第一高度 / CrouchJumpHeight / BackflipJumpHeight
//
// 只放大 FirstJumpHeight 会导致"有的跳得高、有的照旧"（实测每两三次一次高）。
//
// 地址在角色重生后会变，因此按字段序号记录"上次写入的地址与原值"，
// 地址变化时重新捕获。
type multiFloatMul struct {
	f    *Feature
	mult float32
	gets []func(r *Runtime) (uint32, bool)
	addr []uint32
	orig []float32
}

func (g *multiFloatMul) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	if g.addr == nil {
		g.addr = make([]uint32, len(g.gets))
		g.orig = make([]float32, len(g.gets))
	}
	done, total := 0, 0
	for i, get := range g.gets {
		a, ok := get(r)
		if !ok {
			continue
		}
		total++
		if g.addr[i] != a {
			v, ok2 := r.Proc.ReadF32(a)
			if !ok2 {
				g.addr[i] = 0
				continue
			}
			g.addr[i], g.orig[i] = a, v
		}
		if r.Proc.WriteF32(a, g.orig[i]*g.mult) {
			done++
		}
	}
	switch {
	case total == 0:
		g.f.setStatus("地址解析失败")
	case done == 0:
		g.f.setStatus("写入失败")
	default:
		g.f.setStatus(fmt.Sprintf("已放大 %.1fx（%d 项跳跃高度）", g.mult, done))
	}
}

func (g *multiFloatMul) OnDeactivate(r *Runtime) {
	for i := range g.addr {
		if g.addr[i] != 0 {
			r.Proc.WriteF32(g.addr[i], g.orig[i])
			g.addr[i] = 0
		}
	}
}

// ---------- 无限能力点数（含等级门槛修正）----------

// skillPointsRefill 无限能力点数。
//
// 除了把 SkillPoints 补到目标值，还会把 SeinLevel.Current（等级）从 0 修正为 1：
//
//	源码: SeinSoulFlame.AllowedToAccessSkillTree
//	      => m_sein.Level.Current > 0 && IsSafeToCastSoulFlame == Safe
//
// 而 Current 只在 LevelUp() 里 ++，**全新存档（还没升过级）它就是 0**，
// 于是技能树一直打不开、能力点花不出去（实测踩到：玩家看到 "进不去灵魂链接"）。
// 只在它 == 0 时改写，不会影响正常存档的等级。
type skillPointsRefill struct {
	f       *Feature
	target  int32
	sawFix  bool // 本次激活期间修正过等级（状态提示用）
	sawFull bool // 本次激活期间补过点数
}

func (g *skillPointsRefill) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	_, level, _, _, _, _ := r.Addrs()
	if level == 0 {
		g.f.setStatus("地址解析失败")
		return
	}
	lv, ok := r.Proc.ReadI32(level + OffLevelCurrent)
	if !ok {
		g.f.setStatus("读取等级失败")
		return
	}
	if lv <= 0 && r.Proc.WriteI32(level+OffLevelCurrent, 1) {
		g.sawFix = true
	}
	sp, ok := r.Proc.ReadI32(level + OffLevelSkillPoints)
	if !ok {
		g.f.setStatus("读取能力点失败")
		return
	}
	if sp < g.target && r.Proc.WriteI32(level+OffLevelSkillPoints, g.target) {
		g.sawFull = true
		sp = g.target
	}
	switch {
	case g.sawFix && g.sawFull:
		g.f.setStatus(fmt.Sprintf("已补满 = %d；等级 0→1（技能树需等级>0）", g.target))
	case g.sawFix:
		g.f.setStatus(fmt.Sprintf("就绪 = %d；等级已修正 0→1（技能树需等级>0）", sp))
	case g.sawFull:
		g.f.setStatus(fmt.Sprintf("已补满到 %d", g.target))
	default:
		g.f.setStatus(fmt.Sprintf("已就绪 = %d", sp))
	}
}

// ---------- 重置游戏时间 ----------

// FormatGameTime 把秒格式化为 H:MM:SS（与游戏内 GameTimer 的调试串一致）。
func FormatGameTime(sec float32) string {
	if sec < 0 {
		sec = 0
	}
	t := int(sec)
	return fmt.Sprintf("%d:%02d:%02d", t/3600, (t/60)%60, t%60)
}

// resetTime 把 GameTimer.CurrentTime 持续写 0（游玩时间归零）。
//
// 语义:
//   - 开启期间每周期写 0 → 游戏时间恒为 0:00:00（等效"冻结在 0"）。
//   - 关闭后计时从 0 继续累计（CurrentTime += deltaTime 仍在跑）。
//
// 暂停界面的"花费时间"由 TimeCounterDisplay.Update() 每 1 秒读一次
// GameController.Instance.Timer.DisplayTimeAsString（由 CurrentTime 派生），
// 所以归零后最多 1 秒界面就同步显示 0。
//
// 对象通过 GameTimer.Instance 静态槽解析（TimerAddr），换场景/读档后会自动
// 重读新对象；对象地址可能低于常规托管堆下限，解析侧已特殊处理。
type resetTime struct {
	f    *Feature
	orig float32 // 首次归零前的原值（状态提示用）
	got  bool
}

func (g *resetTime) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	obj := r.TimerAddr()
	if obj == 0 {
		g.f.setStatus("计时器定位中…")
		return
	}
	cur, ok := r.Proc.ReadF32(obj + OffTimerCurrentTime)
	if !ok {
		g.f.setStatus("读取计时失败")
		return
	}
	if !g.got && cur > 1 {
		g.orig, g.got = cur, true
	}
	if !r.Proc.WriteF32(obj+OffTimerCurrentTime, 0) {
		g.f.setStatus("写入计时失败")
		return
	}
	if g.got {
		g.f.setStatus(fmt.Sprintf("已归零（原 %s）", FormatGameTime(g.orig)))
	} else {
		g.f.setStatus("已归零 = 0:00:00")
	}
}

// ---------- 获得三把钥匙 ----------

// grantKeys 把三把钥匙的静态标记置 1（Keys.GinsoTree / ForlornRuins / MountHoru）。
//
// 机制: 钥匙只是"库存布尔"，开门逻辑（SeinWorldStateCondition → WorldState.*Key）
// 读的就是这三个静态字段。置 1 等价于走过去捡起钥匙，**不跳过副本内容**，
// 因此风险远低于直接置元素恢复标记（后者会驱动 WorldProgression，可能卡关）。
//
// 字段在 mono 类静态数据块里（对象链取不到），由 Runtime.KeysAddr() 定位，
// 地址每会话变一次、定位一次即可缓存。
// 开启期间维持（幂等写入）；关闭不撤销（钥匙已获得，且会随存档持久化）。
type grantKeys struct {
	f *Feature
}

func (g *grantKeys) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	sd := r.KeysAddr()
	if sd == 0 {
		g.f.setStatus("钥匙地址定位中…")
		return
	}
	offs := [3]uint32{OffKeysGinsoTree, OffKeysForlornRuins, OffKeysMountHoru}
	var v [3]byte
	for i, off := range offs {
		cur, ok := r.Proc.ReadU8(sd + off)
		if !ok {
			g.f.setStatus("读取钥匙标记失败")
			return
		}
		if cur == 0 {
			if r.Proc.WriteU8(sd+off, 1) {
				cur = 1
			}
		}
		v[i] = cur
	}
	if v[0] == 1 && v[1] == 1 && v[2] == 1 {
		g.f.setStatus("三把钥匙已获得 ✓（Ginso / Forlorn / Horu）")
	} else {
		g.f.setStatus(fmt.Sprintf("写入中… Ginso=%d Forlorn=%d Horu=%d", v[0], v[1], v[2]))
	}
}

// ---------- 100% 探索 ----------

// explore100 把所有已加载区域的完成度置为 1（即 100%）。
//
// GameWorld.RuntimeAreas 是 List<RuntimeGameWorldArea>，每个区域的
// m_completionAmount（0..1）参与平均，得到 GameWorld.CompletionAmount，
// 界面百分比 = round(x*100)。同时清掉 m_dirtyCompletionAmount，
// 避免游戏立即重算把值覆盖回去；开启期间每周期维持。
type explore100 struct {
	f *Feature
}

func (g *explore100) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	gw := r.GameWorldAddr()
	if gw == 0 {
		g.f.setStatus("等待定位 GameWorld…")
		return
	}
	list, ok := r.Proc.ReadU32(gw + OffGameWorldRuntimeAreas)
	if !ok || !isHeapPtr(list) {
		g.f.setStatus("地址解析失败")
		return
	}
	items, ok := r.Proc.ReadU32(list + OffListItems)
	size, ok2 := r.Proc.ReadI32(list + OffListSize)
	if !ok || !ok2 || !isHeapPtr(items) || size <= 0 || size > 4096 {
		g.f.setStatus("区域列表读取失败")
		return
	}
	done := 0
	for i := int32(0); i < size; i++ {
		// ⚠ 元素从 items+0x10 开始（items 指向的是"数组对象"，前面有
		// vtable/monitor/bounds/max_length 四个 dword）。
		area, ok := r.Proc.ReadU32(items + OffArrayData + uint32(i)*4)
		if !ok || !isHeapPtr(area) {
			continue
		}
		// 类名校验: 防止元素基址算错时把值写进别的结构（曾经因此写进数组类
		// 的元数据区）。多花 2~3 次读，换"绝不写错对象"，值得。
		if nm, _, ok2 := r.classOf(r.Proc, area); !ok2 || nm != "RuntimeGameWorldArea" {
			continue
		}
		if r.Proc.WriteF32(area+OffAreaCompletion, 1.0) {
			r.Proc.WriteU8(area+OffAreaCompletionDirty, 0)
			done++
		}
	}
	g.f.setStatus(fmt.Sprintf("已置 100%% 探索（%d/%d 区域）", done, size))
}

// ---------- 解锁全部基础技能 ----------

// grantAllAbilities 解锁"暂停界面显示的基础能力"（11 项，见 BaseAbilityOffsets）。
//
// 只给基础能力，**不给灵魂链接技能树里的被动**（那些让玩家用"无限能力点数"
// 自己去买）。原因: 游戏把两者建模成同一类型 CharacterAbility，只能按字段区分。
//
// 已获得的跳过（幂等）；关闭时不做任何事，再次开启仍会把缺的补上。
// 写入的只是 1 字节的 HasAbility 标志位，不触碰其它字段。
//
// 注意: 游戏是在「能力被正规授予」时才创建对应组件
// （PlayerAbilities.SetAbility → Prefabs.EnsureRightPrefabsAreThereForAbilities）。
// 这里只写标志位，因此少数能力（滑翔/冲刺/猛击等非默认实例化的）其组件
// 可能要等存档重载或场景切换后才会实体化生效。
type grantAllAbilities struct {
	f        *Feature
	sawWrite bool // 本次激活期间是否补授过
}

func (g *grantAllAbilities) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	sein, _, _, _, _, _ := r.Addrs()
	if sein == 0 {
		g.f.setStatus("地址解析失败")
		return
	}
	pa, ok := r.Proc.ReadU32(sein + OffSeinPlayerAbil)
	if !ok || !isHeapPtr(pa) {
		g.f.setStatus("地址解析失败")
		return
	}
	granted, total := 0, 0
	for _, off := range BaseAbilityOffsets {
		obj, ok := r.Proc.ReadU32(pa + off)
		if !ok || !isHeapPtr(obj) {
			continue
		}
		total++
		a := obj + OffAbilityHasAbility
		v, ok := r.Proc.ReadU8(a)
		if !ok {
			continue
		}
		if v == 0 && r.Proc.WriteU8(a, 1) {
			granted++
		}
	}
	switch {
	case total == 0:
		g.f.setStatus("地址解析失败")
	case granted > 0:
		g.sawWrite = true
		g.f.setStatus(fmt.Sprintf("已补授 %d 项基础能力（共 %d 项）", granted, total))
	case g.sawWrite:
		g.f.setStatus(fmt.Sprintf("本轮已补授完毕（共 %d 项基础能力）", total))
	default:
		g.f.setStatus(fmt.Sprintf("全部 %d 项基础能力均已拥有", total))
	}
}

// ---------- 无限二段跳 ----------

// infiniteDoubleJump 无限二段跳。
//
// 只写 m_numberOfJumpsAvailable 是不够的: 游戏每帧执行
// DoubleJump.SetStateActive(AllowDoubleJump)，而 AllowDoubleJump 要求
// PlayerAbilities.DoubleJump.HasAbility 为真。没有该能力时状态永远不激活，
// SeinController.PerformJump 里根本不会走到二段跳分支（实测确认:
// 玩家初始 HasAbility=0，因此该功能无效）。
//
// 因此需要: ① 把能力开关置 1（关闭时还原原值）；
//
//	② 持续维持 m_numberOfJumpsAvailable，实现"无限"。
type infiniteDoubleJump struct {
	f      *Feature
	abilit uint32 // CharacterAbility.HasAbility 字段地址
	orig   byte
	have   bool
}

func (g *infiniteDoubleJump) Tick(r *Runtime) {
	if !g.f.Active() {
		return
	}
	sein, _, _, _, dbl, _ := r.Addrs()
	if sein == 0 || dbl == 0 {
		g.f.setStatus("地址解析失败")
		return
	}

	// ① 能力开关（1 字节，不能按 4 字节写，否则会覆盖相邻字段）
	granted := false
	if pa, ok := r.Proc.ReadU32(sein + OffSeinPlayerAbil); ok && isHeapPtr(pa) {
		if obj, ok2 := r.Proc.ReadU32(pa + OffPlayerAbilitiesDoubleJump); ok2 && isHeapPtr(obj) {
			a := obj + OffAbilityHasAbility
			if !g.have {
				if v, ok3 := r.Proc.ReadU8(a); ok3 {
					g.abilit, g.orig, g.have = a, v, true
				}
			}
			if r.Proc.WriteU8(a, 1) {
				granted = true
			}
		}
	}

	// ② 跳跃次数
	counted := r.Proc.WriteI32(dbl+OffDoubleJumpCount, 999)
	switch {
	case !granted && !counted:
		g.f.setStatus("地址解析失败")
	case !granted:
		g.f.setStatus("已维持跳跃次数（能力开关未就绪）")
	case !counted:
		g.f.setStatus("已启用二段跳（次数写入失败）")
	default:
		g.f.setStatus("无限二段跳已启用")
	}
}

func (g *infiniteDoubleJump) OnDeactivate(r *Runtime) {
	// 刻意不还原 HasAbility。
	//
	// 游戏的 SeinNestedPrefab.IsInstantiated setter 在置 false 时会直接
	// Destroy() 掉对应的能力组件（见源码 EnsureRightPrefabsAreThereForAbilities）。
	// 若关闭时把能力还原为 0，二段跳组件会被销毁；再次开启只写标志位无法
	// 将其重建，于是"关闭后再打开就失效"。
	//
	// 因此首次开启后能力保持授予（副作用: 关闭功能后仍保留普通二段跳，
	// 即一次空中跳）。关闭功能只是停止维持无限次数。
	g.have, g.abilit = false, 0
}

// ---------- 功能表 ----------

// jumpField 读取 SeinJump 上某个跳跃高度字段的地址。
func jumpField(r *Runtime, off uint32) (uint32, bool) {
	_, _, _, jump, _, _ := r.Addrs()
	if jump == 0 {
		return 0, false
	}
	return jump + off, true
}

// BuildFeatures 构建功能表（键位对齐风灵月影 DE v1.0 Plus 13）。
//
// 全程持有 tickersMu 写锁: 重建 allTickers / featureActivators 期间，
// 上一会话的功能引擎协程可能仍在 TickAll（见 tickersMu 注释）。
func BuildFeatures() []*Feature {
	tickersMu.Lock()
	defer tickersMu.Unlock()
	allTickers = nil
	featureActivators = map[*Feature]func(on bool){}

	// ---- 字段地址闭包 ----
	// 注意: 所有闭包一律通过 r.Addrs() 取地址（内部加锁）。
	// 后台 Refresh 会并发改写 Runtime 的地址字段，直接读会构成数据竞争。
	// healthObj 解析 SeinCharacter -> Mortality -> HealthController 链。
	healthObj := func(r *Runtime) (uint32, bool) {
		sein, _, _, _, _, _ := r.Addrs()
		if sein == 0 {
			return 0, false
		}
		mor, ok := r.Proc.ReadU32(sein + OffSeinMortality)
		if !ok || mor == 0 {
			return 0, false
		}
		h, ok2 := r.Proc.ReadU32(mor + OffMortalityHealth)
		if !ok2 || h == 0 {
			return 0, false
		}
		return h, true
	}
	energyObj := func(r *Runtime) (uint32, bool) {
		sein, _, _, _, _, _ := r.Addrs()
		if sein == 0 {
			return 0, false
		}
		e, ok := r.Proc.ReadU32(sein + OffSeinEnergy)
		if !ok || e == 0 {
			return 0, false
		}
		return e, true
	}
	hp := func(r *Runtime) (uint32, bool) {
		h, ok := healthObj(r)
		if !ok {
			return 0, false
		}
		return h + OffHealthAmount, true
	}
	hpMax := func(r *Runtime) (uint32, bool) {
		h, ok := healthObj(r)
		if !ok {
			return 0, false
		}
		return h + OffHealthMaxHealth, true
	}
	en := func(r *Runtime) (uint32, bool) {
		e, ok := energyObj(r)
		if !ok {
			return 0, false
		}
		return e + OffEnergyCurrent, true
	}
	enMax := func(r *Runtime) (uint32, bool) {
		e, ok := energyObj(r)
		if !ok {
			return 0, false
		}
		return e + OffEnergyMax, true
	}
	deaths := func(r *Runtime) (uint32, bool) {
		_, _, _, _, _, death := r.Addrs()
		if death == 0 {
			return 0, false
		}
		return death + OffDeathCounterValue, true
	}
	soulCd := func(r *Runtime) (uint32, bool) {
		_, _, soul, _, _, _ := r.Addrs()
		if soul == 0 {
			return 0, false
		}
		return soul + OffSoulFlameCooldownRemaining, true
	}

	jumpHeightFields := []func(*Runtime) (uint32, bool){
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpFirstHeight) },
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpSecondHeight) },
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpThirdHeight) },
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpCrouchHeight) },
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpBackflipHeight) },
		func(r *Runtime) (uint32, bool) { return jumpField(r, OffJumpIdleHeight) },
	}

	// ---- 构造器 ----
	// 无限生命/能量: 持续补满到上限（而非冻结激活瞬间值）
	newRefill := func(digit int, name string, get, getMax func(*Runtime) (uint32, bool), isInt bool, scale float32, unit string) *Feature {
		f := NewFeature(digit, name)
		allTickers = append(allTickers, &freezeMaxFloat{f: f, get: get, getMax: getMax, isInt: isInt, scale: scale, unit: unit})
		return f
	}
	newZeroFloat := func(digit int, name string, get func(*Runtime) (uint32, bool)) *Feature {
		f := NewFeature(digit, name)
		allTickers = append(allTickers, &zeroFloat{f: f, get: get})
		return f
	}
	// 超级跳: 同时放大所有跳跃高度字段（见 multiFloatMul 说明）
	newSuperJump := func(digit int, mult float32) *Feature {
		f := NewFeature(digit, "超级跳")
		allTickers = append(allTickers, &multiFloatMul{f: f, mult: mult, gets: jumpHeightFields})
		return f
	}
	// 无限二段跳: 需要同时开启能力开关（见 infiniteDoubleJump 说明）
	newInfiniteDoubleJump := func(digit int) *Feature {
		f := NewFeature(digit, "无限二段跳")
		allTickers = append(allTickers, &infiniteDoubleJump{f: f})
		return f
	}
	// 能力点数: 不足才补满（见 setIntMin 说明）。
	// 无限能力点数（含"等级 0 → 1"修正，见 skillPointsRefill 说明）
	newSkillPoints := func(digit int, target int32) *Feature {
		f := NewFeature(digit, "无限能力点数")
		allTickers = append(allTickers, &skillPointsRefill{f: f, target: target})
		return f
	}
	newGrantAll := func(digit int) *Feature {
		f := NewCtrlFeature(digit, "解锁全部基础技能")
		allTickers = append(allTickers, &grantAllAbilities{f: f})
		return f
	}
	newExplore100 := func(digit int) *Feature {
		f := NewCtrlFeature(digit, "100% 探索")
		allTickers = append(allTickers, &explore100{f: f})
		return f
	}
	// 不安全区域也可建立灵魂链接
	newSoulFlameAnywhere := func(digit int) *Feature {
		f := NewFeature(digit, "可在不安全区域建立灵魂链接")
		allTickers = append(allTickers, &soulFlameAnywhere{f: f})
		return f
	}
	newCtrlCounterLock := func(digit int, name string, get func(*Runtime) (uint32, bool), target int32) *Feature {
		f := NewCtrlFeature(digit, name)
		t := target
		f.FixedTarget = &t
		allTickers = append(allTickers, &freezeInt{f: f, get: get, fixed: &t})
		return f
	}
	newResetTime := func(digit int) *Feature {
		f := NewCtrlFeature(digit, "重置时间")
		allTickers = append(allTickers, &resetTime{f: f})
		return f
	}
	newGrantKeys := func(digit int) *Feature {
		f := NewCtrlFeature(digit, "获得三把钥匙")
		allTickers = append(allTickers, &grantKeys{f: f})
		return f
	}
	// 键位分配原则: 每个功能只有一个快捷键，两档从简到繁——
	// 小键盘 1-9/0（普通功能）→ Ctrl+小键盘（特殊功能，一命保护排最后）。

	return []*Feature{
		// ===== 基础能力: 小键盘 1-9/0（顺序编号）=====
		newRefill(1, "无限生命", hp, hpMax, true, HealthPointsPerCell, " 球"),
		newRefill(2, "无限能量", en, enMax, false, 1, ""),
		newZeroFloat(3, "灵魂链接无需冷却", soulCd),
		newSoulFlameAnywhere(4),
		newSuperJump(5, 2.5),
		newInfiniteDoubleJump(6),
		newSkillPoints(7, 99),
		// ===== 特殊功能: Ctrl+小键盘（顺序接续）=====
		newCtrlCounterLock(1, "死亡数归零", deaths, 0),
		newExplore100(2),
		newGrantAll(3),
		newResetTime(4),
		newGrantKeys(5),
		// ===== 特殊功能: Ctrl+小键盘 6（一命保护，见 oneLife）=====
	}
}

// CtrlOneLifeDigit 一命保护的键位（Ctrl+小键盘 6，追加在 Ctrl 组末尾）。
//
// ⚠ 历史：曾用 Ctrl+Shift+小键盘 1/0，都不行——Windows Terminal 默认把
// Ctrl+Shift+1..9 绑成"新建标签页"（OpenNewTabProfile0..8），按键会被终端
// 吃掉、根本送不到本程序（表现为"按一下就冒出个新标签页/像修改器退出了"）。
// 现在统一收敛为两档：小键盘（普通）+ Ctrl+小键盘（特殊），不再用 Ctrl+Shift。
const CtrlOneLifeDigit = 6

// OneLifeHotkeyLabel 一命保护的键位显示文本。
func OneLifeHotkeyLabel() string {
	return fmt.Sprintf("Ctrl+小键盘 %d", CtrlOneLifeDigit)
}
