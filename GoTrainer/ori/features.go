// Package ori —— 冻结型功能: 激活时捕获当前值，之后每 tick 写回。
// 功能表两版通用（DE 的字段偏移与原版一致，经堆扫描定位）。
package ori

import (
	"fmt"
	"sync/atomic"
)

// Feature 一个可开关的冻结功能。
type Feature struct {
	Num    int    // 热键数字 1..5
	Name   string
	active atomic.Bool
	capI   atomic.Int32
	capF   atomic.Value // float32
	have   atomic.Bool
	status atomic.Value // string
}

// NewFeature 创建功能。
func NewFeature(num int, name string) *Feature {
	f := &Feature{Num: num, Name: name}
	f.status.Store("未激活")
	return f
}

// Active 是否激活。
func (f *Feature) Active() bool { return f.active.Load() }

// SetActive 开关（关闭时清捕获）。
func (f *Feature) SetActive(on bool) {
	f.active.Store(on)
	if !on {
		f.have.Store(false)
		f.status.Store("未激活")
	}
}

// Status 当前状态文本。
func (f *Feature) Status() string { return f.status.Load().(string) }

func (f *Feature) setStatus(s string) { f.status.Store(s) }

type ticker interface{ Tick(r *Runtime) }

// OneLifeProtect 一命保护: 把 DifficultyController.Difficulty 锁定为 Normal，
// 使一命存档的死亡行为与普通模式一致（在上个检查点复活），
// 同时严格保持 LowestDifficulty == OneLife 以保留"一命通关"成就资格。
//
// 依据（DE v1.0 反编译源码，Assembly-CSharp.dll）:
//   - SeinDamageReciever.OnKill:        if (Difficulty == OneLife) { 标记存档WasKilled + 删光备份 }
//   - SeinDamageReciever.OnKillRoutine: if (Difficulty == OneLife) 弹 GameOver else 淡出+RestoreCheckpoint
//   - AchievementsLogic.OnAct3End:      switch (LowestDifficulty) { case OneLife: 授予成就 }
//   - DifficultyController.ChangeDifficulty: LowestDifficulty = Min(新难度, 原值) —— 只降不升（防作弊）
//
// 因此"只改 Difficulty、不动 LowestDifficulty"即可两全:
// 死亡不会清档/弹 GameOver，而通关时 LowestDifficulty 仍是 OneLife 可拿成就。
//
// 实测验证记录（2026-09）:
//   ✓ 锁定生效: Difficulty 3→1，持续观察 10s+ 游戏未回写
//   ✓ 不变量保持: LowestDifficulty 始终为 3 (OneLife)
//   ✓ 纠正能力: 手动写回 OneLife 后，保护在 300ms 内自动纠正为 Normal
//   ⚠ 未完成的端到端验证（需正常游玩到有伤害区域）:
//     - 真实死亡后的画面表现（预期：淡出后检查点复活，无 GameOver）
//     - 存档写盘后重载，确认 .sav 中 LowestDifficulty 仍为 OneLife
//     建议首次实战使用前，先用新建的一命存档跑一遍死亡流程确认。
//
// 副作用说明: 存档槽元数据 (SaveSlotInfo.Difficulty) 会记为 Normal，
//   因此存档列表可能显示"普通"难度标签；但成就判定读的是 LowestDifficulty，不受影响。
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

// freezeInt 冻结整型字段。
type freezeInt struct {
	f   *Feature
	get func(r *Runtime) (uint32, bool)
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
	if !g.f.have.Load() {
		if v, ok := r.Proc.ReadI32(addr); ok {
			g.f.capI.Store(v)
			g.f.have.Store(true)
		} else {
			g.f.setStatus("读取失败")
			return
		}
	}
	v := g.f.capI.Load()
	if r.Proc.WriteI32(addr, v) {
		g.f.setStatus(fmt.Sprintf("已冻结 = %d", v))
	} else {
		g.f.setStatus("写入失败")
	}
}

// freezeFloat 冻结浮点字段。
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
		if v, ok := r.Proc.ReadF32(addr); ok {
			g.f.capF.Store(v)
			g.f.have.Store(true)
		} else {
			g.f.setStatus("读取失败")
			return
		}
	}
	v := g.f.capF.Load().(float32)
	if r.Proc.WriteF32(addr, v) {
		g.f.setStatus(fmt.Sprintf("已冻结 = %.2f", v))
	} else {
		g.f.setStatus("写入失败")
	}
}

var (
	allTickers []ticker
	oneLife    = NewOneLifeProtect() // 一命保护单例（数字键 6）
)

// OneLife 返回一命保护实例。
func OneLife() *OneLifeProtect { return oneLife }

// BuildFeatures 构建功能表（热键 1..5，两版同名同键位）。
func BuildFeatures() []*Feature {
	allTickers = nil

	lvlSP := func(r *Runtime) (uint32, bool) {
		if r.SeinLevel == 0 {
			return 0, false
		}
		return r.SeinLevel + OffLevelSkillPoints, true
	}
	lvlEXP := func(r *Runtime) (uint32, bool) {
		if r.SeinLevel == 0 {
			return 0, false
		}
		return r.SeinLevel + OffLevelExperience, true
	}
	hp := func(r *Runtime) (uint32, bool) {
		if r.SeinCharacter == 0 {
			return 0, false
		}
		mor, ok := r.Proc.ReadU32(r.SeinCharacter + OffSeinMortality)
		if !ok || mor == 0 {
			return 0, false
		}
		h, ok2 := r.Proc.ReadU32(mor + OffMortalityHealth)
		if !ok2 || h == 0 {
			return 0, false
		}
		return h + OffHealthAmount, true
	}
	en := func(r *Runtime) (uint32, bool) {
		if r.SeinCharacter == 0 {
			return 0, false
		}
		e, ok := r.Proc.ReadU32(r.SeinCharacter + OffSeinEnergy)
		if !ok || e == 0 {
			return 0, false
		}
		return e + OffEnergyCurrent, true
	}
	deaths := func(r *Runtime) (uint32, bool) {
		if r.DeathCounter == 0 {
			return 0, false
		}
		return r.DeathCounter + OffDeathCounterValue, true
	}

	fi := func(num int, name string, get func(*Runtime) (uint32, bool)) *Feature {
		f := NewFeature(num, name)
		allTickers = append(allTickers, &freezeInt{f: f, get: get})
		return f
	}
	ff := func(num int, name string, get func(*Runtime) (uint32, bool)) *Feature {
		f := NewFeature(num, name)
		allTickers = append(allTickers, &freezeFloat{f: f, get: get})
		return f
	}

	return []*Feature{
		ff(1, "无限生命", hp),
		ff(2, "无限能量", en),
		fi(3, "技能点冻结", lvlSP),
		fi(4, "经验冻结", lvlEXP),
		fi(5, "死亡数冻结", deaths),
	}
}

// TickAll 驱动所有激活功能。
func TickAll(r *Runtime) {
	for _, t := range allTickers {
		t.Tick(r)
	}
	oneLife.Tick(r)
}

// DeactivateAll 全部关闭。
func DeactivateAll(fs []*Feature) {
	for _, f := range fs {
		f.SetActive(false)
	}
	oneLife.SetActive(false)
}
