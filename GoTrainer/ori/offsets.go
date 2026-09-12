// Package ori —— 地址常量与运行时结构（原版 + 终极版通用）。
//
// 两版游戏同为 Unity Mono 32 位，类结构经 CE mono dissect 考古：
// 字段偏移两版完全一致，仅类静态槽/堆区域位置不同（因此运行期
// 全部依赖堆扫描定位，不依赖静态槽——CE 的静态槽地址位于
// MonoDataCollector 注入层，外部读取为 MEM_FREE，不可用）。
//
// 原版:  ori.exe   (appid 261570, buildid 814852, Unity 5.0.0)
// 终极版: oriDE.exe (appid 387290, buildid 1096284, Unity 5.3.2)
//
// 指针结构（两版相同，4 字节指针）:
//
//	SeinCharacter: +0x38 Level*(→SeinLevel) +0x3C Energy*(→SeinEnergy)
//	               +0x40 Mortality*(→SeinMortality)
//	SeinLevel:     +0x20 m_sein*(回指本体,克隆/副本为0)
//	               +0x24 SkillPoints(int) +0x2C Experience(int)
//	SeinEnergy:    +0x20 Current(float) +0x24 Max(float)
//	SeinMortality: +0x0C Health*(→SeinHealthController)
//	SeinHealthController: +0x1C Amount(float) +0x20 MaxHealth(int)
//	SeinDeathCounter: +0x14 m_deathCounter(int)
//
// 活体判别（堆扫描签名）:
//   - SeinLevel 回指: u32(X+0x20)=P 且 u32(P+0x38)==X
//   - 加固: Energy.Max∈[1,50] 且 Health.MaxHealth∈[12,400]
//     （排除传送门克隆/UI 副本/教学对象）
//   - DE 的 mono 大堆位于 64 位地址空间高位（0x80000000+，WoW64 影子区），
//     区域枚举须使用 64 位视图（见 core.ReadableRegions）。
//
// 数值验证记录:
//
//	原版: 死亡333 / GameTime 10939s≈存档05:11 / SP=1 / Exp=1187 / 能量0.5/5.0
//	DE:   SP=13 / Exp=405 / 能量1.0/2.0 / 血16/16 / 死亡98
package ori

// 版本定义。
type Version int

const (
	Vanilla Version = iota
	Definitive
)

// Profile 每个版本的目标信息。
type Profile struct {
	Version     Version
	ProcessName string
	DisplayName string
}

// Profiles 版本 -> 目标信息。
var Profiles = map[Version]Profile{
	Vanilla:    {Vanilla, "ori.exe", "原版 (ori.exe)"},
	Definitive: {Definitive, "oriDE.exe", "终极版 (oriDE.exe)"},
}

// 字段偏移（两版一致，字节）。
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

	// SeinDeathCounter
	OffDeathCounterValue = 0x14

	// DifficultyController（一命保护用）
	// 注意: OffDiffLowest 只用于只读校验，任何情况下都不得写入 ——
	// 它是成就资格判定的唯一依据（见 AchievementsLogic.OnAct3End）。
	OffDiffDifficulty = 0x18
	OffDiffLowest     = 0x1C
	OffDiffDelegate   = 0x20

	// ---- SeinCharacter 子对象引用（用于定位新功能的载体对象）----
	OffSeinSoulFlame   = 0x28 // SeinCharacter +0x28 -> SeinSoulFlame
	OffSeinPlatformBeh = 0x48 // SeinCharacter +0x48 -> PlatformBehaviour
	OffSeinInput       = 0x34 // SeinCharacter +0x34 -> SeinInput
	OffSeinPlayerAbil  = 0x4C // SeinCharacter +0x4C -> PlayerAbilities
)

// SeinSoulFlame 字段偏移（灵魂链接: 冷却/安全区域）。
// 来源: CE mono dissect dump（DE v1.0）。
const (
	OffSoulFlameCooldownRemaining = 0xB0 // m_cooldownRemaining (float) —— 归零=无冷却
	OffSoulFlameCooldownDuration  = 0xA8 // CooldownDuration (float)
	OffSoulFlameLock              = 0xA4 // LockSoulFlame (bool) —— 置0解除锁定
	OffSoulFlameCastCount         = 0x90 // m_numberOfSoulFlamesCast
	OffSoulFlameHoldDown          = 0x94 // m_holdDownTime
)

// SeinJump 字段偏移（跳跃强化）。
const (
	OffJumpBackflipHeight = 0x54 // BackflipJumpHeight (float)
	OffJumpCrouchHeight   = 0x58 // CrouchJumpHeight (float)
	OffJumpFirstHeight    = 0x60 // FirstJumpHeight (float) —— 普通跳跃高度
	OffJumpIdleHeight     = 0x64 // JumpIdleHeight (float)
	OffJumpImpulse        = 0x68 // JumpImpulse (float) —— 起跳冲量
	OffJumpSecondHeight   = 0x70 // SecondJumpHeight (float)
	OffJumpThirdHeight    = 0x74 // ThirdJumpHeight (float)
)

// SeinAbilities 字段偏移（技能子对象容器）。
//
// 验证: DE v1.0 活体内存实测 —— Abilities 指向的对象中，
// +0x08 指向 SeinDoubleJump、+0x0C 指向 SeinJump（vtable->klass->name 校验），
// 与 IL 字段声明顺序一致。
const (
	OffAbilitiesDoubleJump = 0x08 // SeinDoubleJump
	OffAbilitiesJump       = 0x0C // SeinJump
)

// SeinDoubleJump 字段偏移（多段跳）。
const (
	OffDoubleJumpStrength   = 0x38 // JumpStrength (float)
	OffDoubleJumpCount      = 0x40 // m_numberOfJumpsAvailable (int) —— 剩余跳跃次数
	OffDoubleJumpTime       = 0x3C // m_doubleJumpTime (float)
	OffDoubleJumpRemainLock = 0x44 // m_remainingLockTime (float)
)

// PlatformMovement 字段偏移（移动速度）。
const (
	OffPlatformLocalSpeed = 0xBC // m_localSpeed (Vector2, float x) —— 当前移动速度
)

// 一命保护相关常量。
const (
	// DifficultyMode 枚举值
	DiffEasy    = 0
	DiffNormal  = 1
	DiffHard    = 2
	DiffOneLife = 3

	// StaticDiffController = DifficultyController.Instance 静态字段的真实存储地址。
	// 实测该地址可被外部进程直接读取，返回权威实例指针（与 CE mono 视图一致）。
	// 获取方式: CE AOB 反查"指向活体实例的引用"，命中项之一即此静态槽。
	// 若游戏版本变更导致失效，resolver 会自动回退到堆扫描路线。
	StaticDiffController = 0x061F3D20
)

// 活体判别阈值（堆扫描加固验证）。
const (
	EnergyMaxMin   = 1.0
	EnergyMaxMax   = 50.0
	MaxHealthMin   = 12
	MaxHealthMax   = 400
	MaxSkillPoints = 99
	MaxExperience  = 999999
)

// SeinSoulFlame 施放控制字段（"不安全区域建链接"功能用）。
// 源码依据（SeinSoulFlame.UpdateCharacterState / HandleCharging / CastSoulFlame）:
//   - m_holdDownTime 只在"安全区域"判定通过时才累加（HandleCharging）
//   - 但施放判定 if (m_holdDownTime == 1f && IsOnGround && m_delayOnGround == 0) CastSoulFlame()
//     不含任何安全检查 —— 因此直接写满蓄力即可绕过全部 7 项安全判定
const (
	OffSoulFlameCastFlag      = 0xBC // m_isCasting (bool) —— 玩家按住链接键
	OffSoulFlameDelayOnGround = 0xC0 // m_delayOnGround (float) —— 落地延迟
)
