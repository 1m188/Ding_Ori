// Package ori 地址常量 —— 全程序唯一地址维护点。
//
// 目标: 原版《Ori and the Blind Forest》Steam 最终构建
//       (buildid 814852 / Build Number 5474 / Unity 5.0.0 32位 Mono)
//
// 地址考古方法: Cheat Engine 7.6 mono dissect (mono_image_enumClasses
// + mono_class_enumFields + mono_class_getStaticFieldAddress)，
// 并已实测验证: 游戏重启后(不同 PID)类静态槽绝对地址不变
// (mono 运行时对 class static storage 采用确定性分配)。
//
// 指针结构(全部 4 字节指针, 32 位进程):
//
//	GameController 静态槽  [0x06553E20] -> Instance* -> +0x64 GameTime(float)
//	SeinDeathCounter 静态槽 [0x0655B5B0] -> Instance* -> +0x14 m_deathCounter(int)
//
//	SeinCharacter (堆中两实例: 本体 + 传送门克隆):
//	  +0x10 Abilities*   +0x18 Controller*   +0x38 Level*(→SeinLevel)
//	  +0x3C Energy*(→SeinEnergy)  +0x40 Mortality*(→SeinMortality)
//	SeinEnergy:   +0x20 Current(float)  +0x24 Max(float)
//	SeinMortality:+0x0C Health*(→SeinHealthController)
//	SeinHealthController: +0x1C Amount(float)  +0x20 MaxHealth(int)
//	SeinLevel:    +0x20 m_sein*(指回本体 SeinCharacter, 克隆为 0)
//	              +0x24 SkillPoints(int) +0x2C Experience(int)
//
// 本体/克隆判别: SeinLevel.m_sein != 0 (克隆的 m_sein==0)。
// 数值验证记录: 死亡数 333 与界面一致; GameTime 10939.6s ≈ 存档 05:11;
// SkillPoints=1 与能力树紫点一致; Energy 0.5/5.0 与 HUD 一致。
package ori

// 类静态槽绝对地址（mono 确定性分配，已实测跨重启稳定）。
const (
	StaticGameController   = 0x06553E20 // -> GameController.Instance
	StaticSeinDeathCounter = 0x0655B5B0 // -> SeinDeathCounter.Instance
)

// 目标进程与模块名。
const (
	ProcessName = "ori.exe"
	ModuleMono  = "mono.dll"
)

// 字段偏移（字节）。
const (
	// SeinCharacter
	OffSeinAbilities  = 0x10
	OffSeinController = 0x18
	OffSeinLevel      = 0x38
	OffSeinEnergy     = 0x3C
	OffSeinMortality  = 0x40

	// SeinEnergy
	OffEnergyCurrent = 0x20
	OffEnergyMax     = 0x24

	// SeinMortality
	OffMortalityHealth = 0x0C

	// SeinHealthController
	OffHealthAmount    = 0x1C
	OffHealthMaxHealth = 0x20

	// SeinLevel
	OffLevelMSein       = 0x20
	OffLevelSkillPoints = 0x24
	OffLevelExperience  = 0x2C

	// GameController
	OffGameTime = 0x64

	// SeinDeathCounter
	OffDeathCounterValue = 0x14
)
