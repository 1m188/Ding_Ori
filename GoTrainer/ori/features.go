// Package ori —— 功能实现: 冻结 / 归零 / 倍率 / 计数锁定 四种模式。
//
// 键位布局对齐风灵月影《奥日与黑暗森林:终极版》v1.0 Plus 13 修改器。
// 所有字段偏移经 CE mono dissect 实测（DE v1.0, Assembly-CSharp.dll）。
package ori

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Feature 一个可开关的功能。
//
// 键位约定（只用小键盘，避免与笔记本键盘的 F 键/主键盘区冲突）:
//   - NeedCtrl=false: 小键盘数字键 Digit（1..9, 0）
//   - NeedCtrl=true : Ctrl + 小键盘数字键 Digit
//
// 另支持"前台导航"：修改器窗口在前台时，用 ↑↓ 选择、回车/空格切换
// （见 UI 层的 navCursor），供没有小键盘的键盘使用。
type Feature struct {
	Digit    int  // 1..9 或 0（小键盘数字键 0）
	NeedCtrl bool // 是否需按住 Ctrl
	Name     string

	// FixedTarget 非 nil 表示"固定目标值"模式（UI 显示用）
	FixedTarget *int32

	active atomic.Bool
	capI   atomic.Int32
	capF   atomic.Value // float32
	have   atomic.Bool
	status atomic.Value // string
}

// NewFeature 小键盘数字键功能。
func NewFeature(digit int, name string) *Feature {
	f := &Feature{Digit: digit, Name: name}
	f.status.Store("未激活")
	return f
}

// NewCtrlFeature Ctrl + 小键盘数字键功能。
func NewCtrlFeature(digit int, name string) *Feature {
	f := &Feature{Digit: digit, NeedCtrl: true, Name: name}
	f.status.Store("未激活")
	return f
}

// HotkeyLabel 键位显示文本。
func (f *Feature) HotkeyLabel() string {
	if f.NeedCtrl {
		return fmt.Sprintf("Ctrl+小键盘 %d", f.Digit)
	}
	return fmt.Sprintf("小键盘 %d", f.Digit)
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
	if r.Proc.WriteF32(addr, target) {
		g.f.setStatus(fmt.Sprintf("已补满 = %.0f", target))
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
//	    m_holdDownTime -= ...;                       // 不安全时回退
//
//	// UpdateCharacterState(): 施放条件【不含任何安全检查】
//	if (m_holdDownTime == 1f && m_sein.IsOnGround && m_delayOnGround == 0f)
//	    CastSoulFlame();
//
// 即 IsSafeToCastSoulFlame（7 项判定：禁区/黑暗/存档台/敌人/重生点/无敌/地面射线）
// 只用于控制蓄力是否累加，真正的施放不检查安全性。
//
// 因此本功能的做法: 当玩家按住链接键（m_isCasting=true）且满足施放前置
// （在地面、无落地延迟）时，直接把蓄力 m_holdDownTime 写满 1.0f ——
// 游戏下一帧就会执行 CastSoulFlame()，从而在不安全区域成功建立链接。
// 这是纯数据写入方案，不需要代码补丁，也不修改任何安全判定本身。
type soulFlameAnywhere struct {
	f *Feature
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

	// 前置: 玩家正在按链接键（m_isCasting）
	castFlag := make([]byte, 1)
	if !p.ReadBytes(sf+OffSoulFlameCastFlag, castFlag) || castFlag[0] == 0 {
		g.f.setStatus("待命（按住链接键时生效）")
		return
	}
	// 前置: 在地面且无落地延迟（源码施放条件的一部分）
	if delay, ok := p.ReadF32(sf + OffSoulFlameDelayOnGround); ok && delay > 0 {
		g.f.setStatus("落地延迟中…")
		return
	}
	// 核心: 把蓄力写满，跳过安全判定的累加过程
	if p.WriteF32(sf+OffSoulFlameHoldDown, 1.0) {
		g.f.setStatus("已强制蓄力 ✓ 可在此区域建链接")
	} else {
		g.f.setStatus("写入失败")
	}
}

type OneLifeProtect struct {
	active  atomic.Bool
	locked  atomic.Bool   // 是否已成功锁定
	status  atomic.Value  // string
	lastLo  atomic.Int32  // 最近读到的 LowestDifficulty（校验用）
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
func (o *OneLifeProtect) Name() string { return "一命保护（死亡如普通模式）" }

// Tick 每周期执行: 若检测到一命难度，则把 Difficulty 锁为 Normal。
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

	// 仅在一命难度下介入（其他难度无需保护，也避免多余写入）
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
	if cur == DiffOneLife {
		if r.Proc.WriteI32(addr, DiffNormal) {
			o.locked.Store(true)
			o.status.Store("已锁定 ✓ 死亡将正常复活")
		} else {
			o.status.Store("写入失败")
		}
		return
	}
	// 已被锁定（或游戏自身写回）
	if o.locked.Load() {
		o.status.Store(fmt.Sprintf("保护中 ✓ (Difficulty=%d, Lowest=%d 已保留)", cur, low))
	} else {
		o.status.Store(fmt.Sprintf("Difficulty=%d", cur))
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
	allTickers  []ticker
	oneLife     = NewOneLifeProtect() // 一命保护（Ctrl+小键盘 3）
	xpBoostMult atomic.Value          // float32 经验倍率，0/未设置=关闭
	xpBoostBase atomic.Int32          // 倍率生效时的经验基数

	// featureActivators 记录需要自定义激活/关闭副作用的执行器
	// （如经验倍率的单选逻辑），由 UI 层通过 ActivateFeature/DeactivateFeature 调用。
	featureActivators = map[*Feature]func(on bool){}
)

// OneLife 返回一命保护实例。
func OneLife() *OneLifeProtect { return oneLife }

// ActivateFeature 激活功能（先设置状态，再执行器特定的激活副作用）。
func ActivateFeature(f *Feature) {
	f.SetActive(true)
	if act, ok := featureActivators[f]; ok {
		act(true)
	}
}

// DeactivateFeature 关闭功能。
// 顺序要求: 先还原原值（倍率型），再清状态，最后回滚激活副作用。
func DeactivateFeature(f *Feature, r *Runtime) {
	if !f.Active() {
		return
	}
	for _, t := range allTickers {
		if sf, ok := t.(*setFloat); ok && sf.f == f {
			if r != nil {
				sf.OnDeactivate(r) // 内部会在还原后清 have
			} else {
				sf.f.have.Store(false)
			}
			break
		}
	}
	f.SetActive(false)
	if act, ok := featureActivators[f]; ok {
		act(false)
	}
}

// TickAll 驱动所有激活中的功能。
func TickAll(r *Runtime) {
	for _, t := range allTickers {
		t.Tick(r)
	}
	tickXPBoost(r)
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
	xpBoostMult.Store(float32(0))
	xpBoostBase.Store(0)
}

// ---------- 经验倍率（Ctrl+小键盘 5/6/7/8）----------

// xpBoostOption 经验倍率选项（单选语义: 激活一个会自动关闭其它倍率）。
type xpBoostOption struct {
	f    *Feature
	mult float32
}

// Tick 空实现: 倍率的实际写入由 tickXPBoost 统一处理。
func (g *xpBoostOption) Tick(r *Runtime) {}

// tickXPBoost 维持 Experience = 基数 × 倍率。
func tickXPBoost(r *Runtime) {
	m, _ := xpBoostMult.Load().(float32)
	if m <= 0 {
		return
	}
	_, level, _, _, _, _ := r.Addrs()
	if level == 0 {
		return
	}
	addr := level + OffLevelExperience
	base := xpBoostBase.Load()
	if base <= 0 {
		cur, ok := r.Proc.ReadI32(addr)
		if !ok || cur <= 0 {
			return
		}
		xpBoostBase.Store(cur)
		base = cur
	}
	r.Proc.WriteI32(addr, int32(float32(base)*m))
}

// SetXPBoost 设置经验倍率（0 = 关闭）。
func SetXPBoost(mult float32) {
	xpBoostBase.Store(0) // 重新捕获基数
	if mult <= 0 {
		xpBoostMult.Store(float32(0))
		return
	}
	xpBoostMult.Store(mult)
}

// XPBoostMult 当前经验倍率（UI 显示）。
func XPBoostMult() float32 {
	m, _ := xpBoostMult.Load().(float32)
	return m
}

// ---------- 功能表 ----------

// BuildFeatures 构建功能表（键位对齐风灵月影 DE v1.0 Plus 13）。
func BuildFeatures() []*Feature {
	allTickers = nil
	xpBoostMult.Store(float32(0))
	xpBoostBase.Store(0)

	// ---- 字段地址闭包 ----
	// 注意: 所有闭包一律通过 r.Addrs() 取地址（内部加锁）。
	// 后台 Refresh 会并发改写 Runtime 的地址字段，直接读会构成数据竞争。
	lvlSP := func(r *Runtime) (uint32, bool) {
		_, level, _, _, _, _ := r.Addrs()
		if level == 0 {
			return 0, false
		}
		return level + OffLevelSkillPoints, true
	}
	lvlEXP := func(r *Runtime) (uint32, bool) {
		_, level, _, _, _, _ := r.Addrs()
		if level == 0 {
			return 0, false
		}
		return level + OffLevelExperience, true
	}
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
	jumpH := func(r *Runtime) (uint32, bool) {
		_, _, _, jump, _, _ := r.Addrs()
		if jump == 0 {
			return 0, false
		}
		return jump + OffJumpFirstHeight, true
	}
	jumpImp := func(r *Runtime) (uint32, bool) {
		_, _, _, jump, _, _ := r.Addrs()
		if jump == 0 {
			return 0, false
		}
		return jump + OffJumpImpulse, true
	}
	dblCount := func(r *Runtime) (uint32, bool) {
		_, _, _, _, dbl, _ := r.Addrs()
		if dbl == 0 {
			return 0, false
		}
		return dbl + OffDoubleJumpCount, true
	}
	dblStrength := func(r *Runtime) (uint32, bool) {
		_, _, _, _, dbl, _ := r.Addrs()
		if dbl == 0 {
			return 0, false
		}
		return dbl + OffDoubleJumpStrength, true
	}

	// ---- 构造器 ----
	// 无限生命/能量: 持续补满到上限（而非冻结激活瞬间值）
	newRefill := func(digit int, name string, get, getMax func(*Runtime) (uint32, bool), isInt bool) *Feature {
		f := NewFeature(digit, name)
		allTickers = append(allTickers, &freezeMaxFloat{f: f, get: get, getMax: getMax, isInt: isInt})
		return f
	}
	newZeroFloat := func(digit int, name string, get func(*Runtime) (uint32, bool)) *Feature {
		f := NewFeature(digit, name)
		allTickers = append(allTickers, &zeroFloat{f: f, get: get})
		return f
	}
	newMultiplier := func(digit int, name string, get func(*Runtime) (uint32, bool), mult float32) *Feature {
		f := NewFeature(digit, name)
		t := &setFloat{f: f, get: get, Multiplier: mult}
		allTickers = append(allTickers, t)
		return f
	}
	newCounterLock := func(digit int, name string, get func(*Runtime) (uint32, bool), target int32) *Feature {
		f := NewFeature(digit, name)
		t := target
		f.FixedTarget = &t
		allTickers = append(allTickers, &setInt{f: f, get: get, Target: t})
		return f
	}
	// 不安全区域也可建立灵魂链接（小键盘 4，对齐 FLiNG 键位）
	newSoulFlameAnywhere := func() *Feature {
		f := NewFeature(4, "可在不安全区域建立灵魂链接")
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
	// 经验倍率选项（单选语义: 同时只有一个倍率生效）
	newXPBoost := func(digit int, mult float32) *Feature {
		f := NewCtrlFeature(digit, fmt.Sprintf("经验倍率 %.0fx", mult))
		allTickers = append(allTickers, &xpBoostOption{f: f, mult: mult})
		// 单选语义: 激活某倍率时关闭其它倍率，并设置全局倍率值
		featureActivators[f] = func(on bool) {
			if on {
				for other, act := range featureActivators {
					if other != f && other.Active() && strings.HasPrefix(other.Name, "经验倍率") {
						act(false) // 关闭其它倍率（会调用 SetXPBoost(0)）
						other.SetActive(false)
					}
				}
				SetXPBoost(mult)
			} else {
				SetXPBoost(0)
			}
		}
		return f
	}

	// 键位分配原则: 每个功能只有一个快捷键；优先填满小键盘 1-9/0（10 个），
	// 溢出部分再用 Ctrl+小键盘。一命保护占用 Ctrl+小键盘 1（见 oneLife 单例）。
	newFixedInt := func(digit int, name string, get func(*Runtime) (uint32, bool), target int32) *Feature {
		f := NewFeature(digit, name)
		t := target
		f.FixedTarget = &t
		allTickers = append(allTickers, &freezeInt{f: f, get: get, fixed: &t})
		return f
	}

	return []*Feature{
		// ===== 小键盘 1-9/0（前 10 项）=====
		newRefill(1, "无限生命", hp, hpMax, true),
		newRefill(2, "无限能量", en, enMax, false),
		newZeroFloat(3, "灵魂链接无需冷却", soulCd),
		newSoulFlameAnywhere(), // 小键盘 4
		newMultiplier(5, "超级跳", jumpH, 2.5),
		newMultiplier(6, "超级跳冲量", jumpImp, 2.0),
		newCounterLock(7, "无限二段跳", dblCount, 99),
		newMultiplier(8, "二段跳强化", dblStrength, 2.0),
		newFixedInt(9, "无限经验", lvlEXP, 999999),
		newFixedInt(0, "无限能力点数", lvlSP, 999),
		// ===== Ctrl+小键盘（第 11 项起；Ctrl+1 为一命保护，见 oneLife）=====
		newCtrlCounterLock(2, "死亡数归零", deaths, 0),
		newXPBoost(3, 2),
		newXPBoost(4, 4),
		newXPBoost(5, 8),
		newXPBoost(6, 16),
	}
}

// CtrlOneLifeDigit 一命保护的键位（Ctrl+小键盘 1）。
const CtrlOneLifeDigit = 1
