// Package ori —— 冻结型功能: 激活时捕获当前值，之后每 tick 写回。
package ori

import (
	"fmt"
	"sync/atomic"
)

// Feature 一个可开关的冻结功能。
type Feature struct {
	Num    int    // 热键数字 1..6 / 0
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

// freezeInt 冻结整型字段。
type freezeInt struct {
	f     *Feature
	get   func(r *Runtime) (uint32, bool) // 返回字段地址
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

// BuildFeatures 构建默认功能表（顺序即热键 1..5）。
func BuildFeatures() []*Feature {
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
		t := &freezeInt{f: f, get: get}
		fTickers = append(fTickers, t)
		return f
	}
	ff := func(num int, name string, get func(*Runtime) (uint32, bool)) *Feature {
		f := NewFeature(num, name)
		t := &freezeFloat{f: f, get: get}
		fTickers = append(fTickers, t)
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

// fTickers 全部功能的 tick 实现（BuildFeatures 内注册）。
var fTickers []interface{ Tick(*Runtime) }

// TickAll 驱动所有激活功能。
func TickAll(fs []*Feature, r *Runtime) {
	for _, t := range fTickers {
		t.Tick(r)
	}
}

// DeactivateAll 全部关闭。
func DeactivateAll(fs []*Feature) {
	for _, f := range fs {
		f.SetActive(false)
	}
}
