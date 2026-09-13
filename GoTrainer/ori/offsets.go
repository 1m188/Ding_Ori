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
	OffLevelCurrent     = 0x28 // 等级：**技能树开启条件是 Current > 0**
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

// PlayerAbilities 能力开关偏移。
//
// 取值路径: SeinCharacter.PlayerAbilities(+0x4C) -> PlayerAbilities 对象
//
//	-> 能力字段（CharacterAbility 引用）-> CharacterAbility.HasAbility(+0x08)。
//
// 注意: 这两个偏移经 mono 元数据直读核验，**与源码字段声明顺序不同**
// （mono 会重排字段；按声明顺序推算得到 0x18/0x48，实测为 0x24/0x54）。
//
// 用途: 二段跳能否触发取决于 PlayerAbilities.DoubleJump.HasAbility ——
// 游戏每帧执行 DoubleJump.SetStateActive(AllowDoubleJump)，而
// AllowDoubleJump 要求该能力为真；否则 PerformJump 永远不会进入二段跳分支。
const (
	OffPlayerAbilitiesDoubleJump        = 0x24
	OffPlayerAbilitiesDoubleJumpUpgrade = 0x54
	OffAbilityHasAbility                = 0x08 // CharacterAbility.HasAbility (bool, 1 字节)
)

// BaseAbilityOffsets "暂停界面显示的基础能力"在 PlayerAbilities 中的字段偏移。
//
// 重要区分: 游戏把"基础能力"和"灵魂链接技能树里用能力点买的被动"建模成
// 同一个类型（CharacterAbility，只有一个 HasAbility 布尔），因此**无法靠类型
// 区分**，只能按字段清单区分。本表只列基础能力，技能树被动（RapidFire /
// UltraDefense / 各种 *Efficiency / Upgrade / MapMarkers 等）**一律不在内**，
// 那些应由玩家自己用"无限能力点数"去技能树购买。
//
// 偏移经 mono 元数据核验（PlayerAbilities 首个字段 +0x14）。
var BaseAbilityOffsets = []uint32{
	0x3C, // SpiritFlame    精灵之火
	0x1C, // WallJump       飞檐走壁
	0x18, // ChargeFlame    充能烈焰
	0x24, // DoubleJump     二段跳
	0x14, // Bash           猛击
	0x20, // Stomp          践踏攻击
	0x38, // Glide          黑子之羽
	0x34, // Climb          攀爬
	0x28, // ChargeJump     充能跳跃
	0xA8, // Grenade        光芒爆裂
	0xAC, // Dash           冲刺
}

// GameWorld 字段偏移（探索度所在；GameWorld.Instance 可定位）。
//
//	GameWorld.RuntimeAreas(+0x18) -> List<RuntimeGameWorldArea>
//	每个 RuntimeGameWorldArea 的 m_completionAmount 即该区域完成度（0..1），
//	GameWorld.CompletionAmount 是各区域的平均值，CompletionPercentage = round(x*100)。
const (
	OffGameWorldRuntimeAreas = 0x18
	OffAreaCompletion        = 0x14 // RuntimeGameWorldArea.m_completionAmount (float, 0..1)
	OffAreaCompletionDirty   = 0x18 // RuntimeGameWorldArea.m_dirtyCompletionAmount (bool)
	// List<T> / 数组布局（mono，32 位）:
	//   List:  +0x08 _items（指向数组对象）, +0x0C _size
	//   数组:  +0x00 vtable, +0x04 monitor, +0x08 bounds, +0x0C max_length,
	//          **+0x10 起才是元素**
	// ⚠ 曾经把 _items 当元素起点（+i*4）使用，导致元素整体错位 4 个：
	//   既漏写了后半部分区域，又把值写进了数组类的元数据区。见 README §8。
	OffListItems = 0x08
	OffListSize  = 0x0C
	OffArrayData = 0x10 // 数组元素起始偏移
)

// GameTimer 字段偏移（游玩计时器；"重置时间"功能用）。
//
// 载体: GameController.Timer(+0x14) 指向 GameTimer；该类另有静态单例
// GameTimer.Instance（可用它直接定位活体实例）。
//
//	CurrentTime          +0x1C  float 秒 —— 累计游玩时间
//	                     FixedUpdate() 里 `CurrentTime += Time.deltaTime`
//	                     （主菜单/扩展标题界面/加载中会提前 return，暂停时
//	                     Time.timeScale=0 → deltaTime=0，因此不增长）。
//	m_waitTillSave       +0x20  float ∈[0,1]，内部每秒刷新节流（校验用）
//	m_sendTelemetryTimer +0x24  float ∈[0,60]，遥测发送计时（校验用）
//
// 暂停界面显示: TimeCounterDisplay.Update() 每 1 秒读
// GameController.Instance.Timer.DisplayTimeAsString → 由 CurrentTime 派生，
// 所以把 CurrentTime 写 0 后最多 1 秒界面同步显示 0。
//
// ⚠ CurrentTime 是"值语义"（累加器），不是指针派生值：直接写 0 即可，
//
//	不存在被游戏重算覆盖的问题；开启期间持续写 0 就等于冻结在 0。
const (
	OffTimerCurrentTime  = 0x1C
	OffTimerWaitTillSave = 0x20
	OffTimerTelemetry    = 0x24
)

// 三把钥匙的静态字段偏移（"获得三把钥匙"功能用）。
//
// 游戏用两个**纯静态类**保存世界状态（没有实例，字段全是 static bool）：
//
//	public static class Keys { GinsoTree; ForlornRuins; MountHoru; }
//	namespace Sein.World { static class Events { WaterPurified; WindRestored; WarmthReturned; ... } }
//
// 这些字段不在任何对象里，而在 mono 的"类静态数据块"中：
//
//	字段地址 = u32(MonoVTable + 0x0C) + 字段偏移
//
// 偏移由 mono 字段描述符实测（描述符布局 {name*, klass*, offset}）。
// 暂停界面的"三钥匙/三元素"图标由 WorldState 条件读这些标记，
// 详见 README §7-14。
const (
	OffKeysGinsoTree    = 0 // Keys.GinsoTree：Ginso Tree 门钥匙
	OffKeysForlornRuins = 1 // Keys.ForlornRuins：Forlorn Ruins 门钥匙
	OffKeysMountHoru    = 2 // Keys.MountHoru：Mount Horu 门钥匙
)

// 生命值单位换算。
//
// SeinHealthController.HealthUpgradesCollected => MaxHealth/4 - 3，
// 即游戏内部**一个生命球 = 4 点**。游戏界面显示的是球数，而内存里是点数:
// 初始 3 球 => MaxHealth = 12。UI 显示需除以该系数才与游戏一致。
const HealthPointsPerCell = 4

// SeinSoulFlame 字段偏移（灵魂链接: 冷却/安全区域）。
// 来源: CE mono dissect dump（DE v1.0）。
const (
	OffSoulFlameCooldownRemaining = 0xB0 // m_cooldownRemaining (float) —— 归零=无冷却
	OffSoulFlameCooldownDuration  = 0xA8 // CooldownDuration (float)
	OffSoulFlameLock              = 0xA4 // LockSoulFlame (bool) —— 置0解除锁定
	OffSoulFlameCastCount         = 0x90 // m_numberOfSoulFlamesCast
	OffSoulFlameHoldDown          = 0x94 // m_holdDownTime
	OffSoulFlameTapRemaining      = 0xB8 // m_tapRemainingTime: >0 表示仍处于"轻点"窗口（松开即开技能树）
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
)

// ⚠ 已废弃（保留说明以免后人重犯）:
//
//	曾有一个硬编码常量 StaticDiffController = 0x061F3D20，声称是
//	"DifficultyController.Instance 静态字段的真实存储地址"。**它是错的** ——
//	实测读出来是 0x8240810B 这类非指针值（既不是该对象、也不是任何有效指针），
//	靠它取到的"实例"是假对象，导致一命保护静默失效且写出垃圾。
//
//	教训: mono 的静态字段位于**运行期分配的静态数据块**，不是固定地址；
//	任何"抄来的绝对地址"都必须经过 `probe` 实地核验，不能直接信。
//	正确的做法是定位"指向该类型实例的静态槽"（resolver 的 aux 扫描）或
//	从 SaveSceneManager.SaveData 列表取（见 README §11）。

// 活体判别阈值（仅供文档/历史参考；实际校验见 resolver.validateSein）。
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
//   - HandleCharging() 在"安全区域"判定通过时才累加 m_holdDownTime；
//     否则走回退分支 m_holdDownTime -= deltaTime / HoldDownDuration
//   - 施放判定 if (m_holdDownTime == 1f && IsOnGround && m_delayOnGround == 0) CastSoulFlame()
//     不含任何安全检查 —— 直接写满蓄力即可绕过全部 7 项安全判定
//
// ⚠ 关键: 施放判定在同帧的 HandleCharging 之后，若只写 1.0 会被回退分支扣掉
// （实测因此"偶尔能建、大多数时候不行"）。做法是按住期间把 HoldDownDuration
// 置为 +Inf，使回退量 delta/Inf = 0（float32 下 1.0f - 0 仍是 1.0f），
// 1.0 得以保持到施放判定；松开后还原原值。
const (
	OffSoulFlameCastFlag      = 0xBC // m_isCasting (bool) —— 玩家按住链接键
	OffSoulFlameDelayOnGround = 0xC0 // m_delayOnGround (float) —— 落地延迟
	OffSoulFlameHoldDownDur   = 0x98 // HoldDownDuration (float) —— 蓄力时长；临时置 +Inf 抑制回退
)
