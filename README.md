# OriTrainer (Go) — 奥日与迷失森林 双版本修改器

风灵月影风格的 TUI 修改器，一个 exe 支持两个版本：

- **原版**《Ori and the Blind Forest》（`ori.exe`）
- **终极版**《Ori and the Blind Forest: Definitive Edition》（`oriDE.exe`）

**纯 Go 标准库实现（零外部依赖），单 exe 发布。**
本 README 分两部分：前面是**使用文档**，后面是**维护手册 / 逆向知识库**——
后者记录了本项目所有逆向结论、踩过的坑与维护流程，目的就是让**完全不了解
上下文的新会话**能在最短时间内接手。

> 当前状态：**终极版功能已实测可用；原版未做验证**（我们的逆向工作全部在终极版
> 完成，原版字段偏移未核验，见 `ctables/ori_vanilla.ct` 的说明）。

---

# 一、使用文档

## 使用

1. 以管理员权限启动 `GoTrainer.exe`，在版本选择界面用 ↑↓/WS 选择，回车进入。
2. 启动对应游戏并进入存档。修改器自动附加，约 **0.2–1 秒**内定位活体玩家对象
   （横向对比：早期版本是全堆扫描 9–15 秒，且经常定位失败）。
3. ESC 可随时返回版本选择（仅在修改器窗口有焦点时生效）。

## 热键（三档）

键位分三档，每档内按顺序编号，**无空位、无重复**。三档互斥由"两个修饰键状态
都要精确匹配"保证——按 Ctrl+Shift+N 时，小键盘 N 与 Ctrl+N 都不会被误触发。

**基础能力 —— 小键盘 1-9/0**

| 热键 | 功能 | 状态 |
|---|---|---|
| 小键盘 1 | 无限生命 | ✓ 实测（按"球"显示，补满到上限） |
| 小键盘 2 | 无限能量 | ✓ 实测（上限为 0 时提示暂无可补） |
| 小键盘 3 | 灵魂链接无需冷却 | ✓ 实测（冷却归零） |
| 小键盘 4 | 可在不安全区域建立灵魂链接 | ✓ 实测 |
| 小键盘 5 | 超级跳 | ✓ 实测（同时放大 6 个跳跃高度字段） |
| 小键盘 6 | 无限二段跳 | ✓ 实测（同时开启二段跳能力） |
| 小键盘 7 | 无限能力点数 | ✓ 实测（不足才补满，目标 99） |

**进阶能力 —— Ctrl+小键盘 1..N**

| 热键 | 功能 | 状态 |
|---|---|---|
| Ctrl+小键盘 1 | 死亡数归零 | ✓ 实测 |
| Ctrl+小键盘 2 | 100% 探索 | ✓ 实测（区域完成度置 1） |
| Ctrl+小键盘 3 | 解锁全部基础技能 | ✓ 实测（11 项，只给基础能力） |

**特殊能力 —— Ctrl+Shift+小键盘 1..N**

| 热键 | 功能 | 状态 |
|---|---|---|
| Ctrl+Shift+小键盘 1 | 一命保护 | ⚠ **未生效**（目标对象定位不到，见维护手册 §11） |

**界面按键（仅修改器窗口有焦点时生效）**

| 键 | 作用 |
|---|---|
| ↑↓ / WS | 选择功能项 |
| 回车 / 空格 | 激活或取消选中功能 |
| HOME / F12 / F1 / ESC / END | 全关 / 重扫 / 帮助 / 返回版本选择 / 退出 |

## 功能语义

- **无限生命 / 无限能量 = 持续补满到上限**：每周期读上限并写满当前值。
  残血/空能量时激活也会立即补满。
- **超级跳 / 无限二段跳 = 捕获原值后放大 / 维持，关闭时还原**。
- **无限能力点数 = 不足才补满**：低于 99 才写，达到或超过不动（原因见 §8-13）。
- **100% 探索 / 解锁全部基础技能 = 一次性动作型**：开启期间维持，关闭不做任何事，
  再次开启仍会把缺的补上。
- **死亡数归零 = 固定目标值**：无条件写 0，适合"无死亡通关"成就。

---

# 二、维护手册 / 逆向知识库

## §0 新会话从这里开始

**这个项目是什么**：一个 Go 写的进程内存读写工具（TUI），通过定位游戏
（Unity + mono，32 位进程）里的托管对象、按字段偏移读写来生效。

**30 秒理解核心机制**：

1. 游戏是 32 位 mono 进程，所有关心的对象都在**托管堆**上，**每次启动地址都会变**。
2. 游戏自己的静态字段 `Characters.Sein` / `Characters.Current` 总是指向"活体玩家"，
   且两者在内存里相隔 12 字节、值相同（形如 `[P, 0, 0, P, …]`）。
3. 我们在**低地址区**扫这个模式 → 拿到槽位地址 → **每周期重读该槽**（而不是缓存
   对象地址），于是角色重生也能立刻跟上。
4. 拿到玩家对象后，其余对象都是**沿对象链直读**（见 §3 的链）。
5. 字段偏移**不能靠源码声明的顺序推算**（mono 会重排），必须用
   `probe offs` 从运行中的 mono 元数据里读（见 §4）。

**改一个功能的标准流程**（详见 §10）。

**代码地图**：

| 文件 | 职责 |
|---|---|
| `GoTrainer/main.go` | TUI 渲染、按键（控制台输入 + 全局热键）、会话协程 |
| `GoTrainer/core/memory.go` | 进程附加、RPM/WPM、区域枚举、控制台输入、存档回滚工具 |
| `GoTrainer/ori/resolver.go` | 对象定位（活体玩家 / 各单例）、快照读取 |
| `GoTrainer/ori/features.go` | 功能定义与执行器（冻结/补满/放大/置位/遍历） |
| `GoTrainer/ori/offsets.go` | **唯一地址维护点**：所有字段偏移常量 |
| `GoTrainer/cmd/probe/main.go` | 诊断工具（等价 CE 的核心能力），见 §9 |
| `ctables/ori_de.ct` / `ori_vanilla.ct` | 与修改器同步的 CE 表，见 §10 |

## §1 目标程序与环境

| 项 | 终极版 | 原版 |
|---|---|---|
| 进程名 | `oriDE.exe` | `ori.exe` |
| Unity | 5.3.2 | 5.0 |
| 运行时 | mono（32 位 WoW64） | mono（32 位 WoW64） |
| 托管程序集 | `<游戏目录>/OriDE_Data/Managed/Assembly-CSharp.dll` | 同结构 |
| 混淆 | **无**（可反编译；目录里存在 3DM 汉化补丁） | 无 |

- 修改器本体是 **64 位** Go 程序，用 `ReadProcessMemory` 读 32 位目标。
- 反编译：用 ILSpy 打开 `Assembly-CSharp.dll` 即可得到完整 C# 源码。本项目所有
  "源码依据"均指该反编译结果。

## §2 进程内存模型（关键前提）

**地址空间是 32 位 + `/LARGEADDRESSAWARE`**：托管堆横跨 `0x50xxxxxx`–`0x7Bxxxxxx`。
实测观察到的地址带：

| 地址带 | 内容 |
|---|---|
| `< 0x10000000` | mono 低区：静态数据 / 分配区 / JIT 代码（实测有 `MEM_PRIVATE + PAGE_EXECUTE_READWRITE` 段） |
| `0x2A000000`–`0x2BFFFFFF` | mono 元数据：klass、类型名字符串、部分 vtable |
| `0x50xxxxxx`–`0x53xxxxxx` | vtable / mono 运行时结构 |
| `0x50xxxxxx`–`0x7Bxxxxxx` | **托管堆对象**（我们的目标） |

要点：

- **RPM 地址用零扩展**（`uint32 → uintptr`）。WoW64 目标的地址空间就是宿主 64 位
  空间的低 4GB，零扩展即正确；符号扩展会指向未映射区。
- 区域枚举（`ReadableRegions`）用 64 位视图的 `VirtualQueryEx`，再把
  `BaseAddress` 掩码到低 32 位。
- **任何 `0x?0000000` 之类的地址上限假设都极其危险**——见 §8-2。

## §3 对象定位原理

### 3.1 活体玩家（最重要）

游戏里 `Characters` 是静态类，字段顺序：

```csharp
public class Characters {
    public static SeinCharacter Sein;   // ← 活体玩家
    public static BabySein BabySein;
    public static Naru Naru;
    public static Character Current;    // ← 与 Sein 同一个对象
    public static Ori Ori;
}
```

在内存里表现为 `[P, x, y, P, …]`：**同一个堆指针 P 出现在相隔 12 字节的两个槽位**
（`Sein` 在 +0，`Current` 在 +0xC；中间两项在某些游戏状态下为 null）。

定位算法（`resolver.go`）：

1. 缓存槽位地址 `seinAnchor`，**每周期直接重读该槽**——角色重生时游戏会改写静态
   字段，槽内容自动指向新对象。
2. 槽失效（读不到 / 校验不过）时，扫低区（`< 0x10000000`）找 `[P, …, P]` 模式，
   逐个用 `validateSein` 校验 + 类名回读确认。
3. 仍失败则全地址空间兜底。

`validateSein` 的判据（每一条都对应一次踩坑，别随意加严）：

- 对象在堆带、`[0]` 是合法 vtable；
- `Level(+0x38)` / `Energy(+0x3C)` / `Mortality(+0x40)` 都是堆指针；
- `u32(Level+0x20) == 对象`（`SeinLevel.m_sein` 回指本体）；
- 能量/生命的数值在合理范围——**只校验范围，不校验 `Current <= Max`**
  （拾取瞬间或外部改写会出现 `Current > Max` 的暂态，加严会让解析器整体失效，见 §8-11）。

### 3.2 玩家对象链（其余对象都从这里直读）

```
SeinCharacter
  +0x10 -> SeinAbilities
             +0x08 -> SeinDoubleJump
             +0x0C -> SeinJump
  +0x28 -> SeinSoulFlame
  +0x38 -> SeinLevel        (+0x20 m_sein 回指)
  +0x3C -> SeinEnergy
  +0x40 -> SeinMortality
             +0x0C -> SeinHealthController
  +0x48 -> PlatformBehaviour
  +0x4C -> PlayerAbilities  (每个能力: +偏移 -> CharacterAbility -> +0x08 HasAbility)
```

子对象会随场景/重生重建，因此**每次刷新都重读指针**。

### 3.3 独立单例（低区槽扫描）

`SeinDeathCounter` / `GameWorld` 等单例的做法：扫低区（`< 0x10000000`）中"值能
解析成目标类名"的槽位，缓存槽地址并在每周期重读；槽失效则清空重扫（带指数退避）。

**注意**：`GameWorld` 能定位；`DifficultyController` / `GameTimer` / `GameController`
**定位不到**（见 §11）。

## §4 mono 元数据读取（怎么查字段偏移）

这是本项目最关键的"武器"：**不靠猜、不靠源码顺序，直接从运行中的进程读 mono 元数据**。

### 4.1 对象与类的结构

```
对象:  [0] = vtable 指针        [4] = monitor
vtable: [0] = klass 指针
klass:  [0] = 自身（自指，可靠性判据）   [+0x30] = 类型名字符串指针
```

判"这个指针是不是合法 klass"的三条：`klass` 在合法指针范围、`u32(klass)==klass`
（自指）、`klass+0x30` 指向可读的标识符字符串。

⚠ **名字符串不保证 4 字节对齐**（实测 `SeinDeathCounter` 的名字在 `0x2AFB8F4D`）。
对字符串指针做对齐校验会导致**所有类名解析静默失败**——见 §8-3。

### 4.2 字段描述符

在元数据区搜索"值等于类型名字符串地址"的 dword（即字段描述符的 name 指针），
该描述符：

```
+0x00 = 字段名字符串指针
+0x04 = 声明该字段的 klass
+0x08 = 字段在对象内的偏移     ← 我们要的
```

`probe offs <类名> <字段名...>` 就是这个逻辑 + 按声明类过滤。

⚠⚠ **元数据偏移 ≠ 源码字段声明顺序**：mono 会重排。实测例子：
`PlayerAbilities.DoubleJump` 按声明顺序推算是 `+0x18`，**实际是 `+0x24`**；
`DoubleJumpUpgrade` 推算 `+0x48`，实际 `+0x54`。**必须查元数据，不要推算。**

## §5 字段偏移总表（终极版，均已由元数据+活体内存双重核验）

### SeinCharacter 及其直接子对象

| 类 | 字段 | 偏移 | 说明 |
|---|---|---|---|
| SeinCharacter | Abilities | 0x10 | → SeinAbilities |
| SeinCharacter | Controller | 0x18 | |
| SeinCharacter | Input | 0x34 | |
| SeinCharacter | SoulFlame | 0x28 | → SeinSoulFlame |
| SeinCharacter | Level | 0x38 | → SeinLevel |
| SeinCharacter | Energy | 0x3C | → SeinEnergy |
| SeinCharacter | Mortality | 0x40 | → SeinMortality |
| SeinCharacter | PlatformBehaviour | 0x48 | |
| SeinCharacter | PlayerAbilities | 0x4C | → PlayerAbilities |
| SeinAbilities | DoubleJump | 0x08 | → SeinDoubleJump |
| SeinAbilities | Jump | 0x0C | → SeinJump |

### 数值字段

| 类 | 字段 | 偏移 | 类型 | 说明 |
|---|---|---|---|---|
| SeinLevel | m_sein | 0x20 | ptr | 回指本体（校验用） |
| SeinLevel | SkillPoints | 0x24 | int | 可用能力点 |
| SeinLevel | Experience | 0x2C | int | 经验 |
| SeinEnergy | MinVisual | 0x18 | float | |
| SeinEnergy | MaxVisual | 0x1C | float | |
| SeinEnergy | Current | 0x20 | float | 当前能量 |
| SeinEnergy | Max | 0x24 | float | **能量上限 = 能量球数** |
| SeinMortality | Health | 0x0C | ptr | → SeinHealthController |
| SeinHealthController | Amount | 0x1C | float | 当前生命（**点数，1 球 = 4 点**） |
| SeinHealthController | MaxHealth | 0x20 | int | 生命上限（点数） |
| SeinHealthController | VisualMinAmount | 0x24 | float | 界面插值 |
| SeinHealthController | VisualMaxAmount | 0x28 | float | 界面插值 |
| SeinDeathCounter | m_deathCounter | 0x14 | int | 死亡次数 |
| DifficultyController | Difficulty | 0x18 | int | 0=Easy 1=Normal 2=Hard 3=OneLife（**可写**） |
| DifficultyController | LowestDifficulty | 0x1C | int | **⛔ 只读！成就资格唯一依据** |
| DifficultyController | OnDifficultyChanged | 0x20 | ptr | |
| GameWorld | RuntimeAreas | 0x18 | ptr | → List\<RuntimeGameWorldArea\> |
| RuntimeGameWorldArea | m_completionAmount | 0x14 | float | 区域完成度 0..1 |
| RuntimeGameWorldArea | m_dirtyCompletionAmount | 0x18 | bool | 置 0 防重算 |
| List\<T\> | _items | 0x08 | ptr | 元素数组 |
| List\<T\> | _size | 0x0C | int | 元素个数 |

### SeinSoulFlame

| 字段 | 偏移 | 说明 |
|---|---|---|
| m_numberOfSoulFlamesCast | 0x90 | |
| m_holdDownTime | 0x94 | 蓄力；写满 1.0 = 可施放 |
| m_tapRemainingTime | 0xB8 | **>0 表示仍在"轻点"窗口**（松开 → 打开技能树） |
| m_isCasting | 0xBC | 玩家按住链接键 |
| m_delayOnGround | 0xC0 | 落地延迟，>0 不可施放 |
| LockSoulFlame | 0xA4 | |
| CooldownDuration | 0xA8 | |
| m_cooldownRemaining | 0xB0 | 归零 = 无冷却 |

### SeinJump（跳跃高度全在 SeinJump 上，**不是一个变量**）

| 字段 | 偏移 | 用在哪 |
|---|---|---|
| BackflipJumpHeight | 0x54 | 转身后空翻 |
| CrouchJumpHeight | 0x58 | 蹲跳 |
| FirstJumpHeight | 0x60 | 移动跳/站立跳 轮换第 1 段 |
| JumpIdleHeight | 0x64 | 备用高度 |
| JumpImpulse | 0x68 | 起跳冲量（**当前未被任何功能修改**） |
| SecondJumpHeight | 0x70 | 轮换第 2 段 |
| ThirdJumpHeight | 0x74 | 轮换第 3 段 / 贴墙跳 |

### SeinDoubleJump

| 字段 | 偏移 | 说明 |
|---|---|---|
| JumpStrength | 0x38 | 二段跳强度 |
| m_doubleJumpTime | 0x3C | |
| m_numberOfJumpsAvailable | 0x40 | 剩余跳跃次数（int） |
| m_remainingLockTime | 0x44 | |

### PlayerAbilities

- 能力字段全部是 `CharacterAbility` 引用（连续 `0x14`..`0xB0`，共 40 项）；
  `CharacterAbility` 内 `HasAbility` 在 **+0x08，1 字节 bool**。
- **基础能力（暂停界面 11 项）** ← 修改器"解锁全部基础技能"只给这些：

| 偏移 | 字段 | 中文 |
|---|---|---|
| 0x14 | Bash | 猛击 |
| 0x18 | ChargeFlame | 充能烈焰 |
| 0x1C | WallJump | 飞檐走壁 |
| 0x20 | Stomp | 践踏攻击 |
| 0x24 | DoubleJump | 二段跳 |
| 0x28 | ChargeJump | 充能跳跃 |
| 0x34 | Climb | 攀爬 |
| 0x38 | Glide | 黑子之羽 |
| 0x3C | SpiritFlame | 精灵之火 |
| 0xA8 | Grenade | 光芒爆裂 |
| 0xAC | Dash | 冲刺 |

- 其余为**技能树被动 / 升级**（RapidFire、WaterBreath、Sense、StompUpgrade、
  DoubleJumpUpgrade、BashBuff、UltraDefense、各 \*Efficiency、Rekindle、Regroup、
  MapMarkers、HealthMarkers、EnergyMarkers、AbilityMarkers 等），
  **修改器一律不给**，由玩家用"无限能力点数"自己去技能树买。
- **独立第三方交叉验证**：删掉的旧表 `attach_3943.ct` 里的 AA 脚本按
  `[esi+3C]/+1C/+18/+24/+14/+20/+38/+34/+28` 读取能力，与我们核验的偏移
  **完全一致**（Bash+14 … SpiritFlame+3C）。

## §6 逐功能实现（写什么、为什么）

| 功能 | 写入字段 | 关键点 |
|---|---|---|
| 无限生命 | `health+1C = health+20`（Amount ← MaxHealth） | 每周期写；上限为 0 时提示 |
| 无限能量 | `energy+20 = energy+24` | 同上 |
| 灵魂链接无需冷却 | `soulflame+B0 = 0` | |
| 不安全区域建链接 | 长按确认后 `soulflame+94 = 1.0` | **必须先检查 `m_tapRemainingTime(+B8) <= 0`**，否则轻点开技能树失效（§8-16） |
| 超级跳 | 同时放大 6 个高度字段（0x54/0x58/0x60/0x64/0x70/0x74），关闭还原 | 只放大一个会出现"有的跳得高有的照旧"（§8-17） |
| 无限二段跳 | `playerab+24 → +8 = 1`（能力开关）+ `doublejump+40 = 999` | 只写次数不够（§8-14）；**关闭时不还原能力开关**（§8-15） |
| 无限能力点数 | `level+24`，`< 99` 才写 | 持续写会与升级结算打架（§8-13） |
| 死亡数归零 | `death+14 = 0` | |
| 100% 探索 | 遍历 `gw+18` 列表，每个区域 `+14 = 1.0`、`+18 = 0` | 只能解锁"完成地图"成就，不是"找齐秘密"（§7-6） |
| 解锁全部基础技能 | 上表 11 个 `[playerab+偏移]+8 = 1` | 只写 1 字节；组件实例化见 §7-4 |
| 一命保护 | `diffc+18 = 1` | ⚠ 目标对象定位不到，未生效（§11） |

所有执行器一律通过 `Runtime.Addrs()` / `SubAddr()` 取地址（内部加锁）——
**不要直接读 Runtime 字段**，后台刷新会并发改写（§8-6）。

## §7 游戏内部机制事实（改功能前必读）

1. **生命单位是"点"，不是"球"**：`HealthUpgradesCollected => MaxHealth/4 - 3`，
   即 **1 个生命球 = 4 点**。初始 3 球在内存里是 `12`。UI 显示要除以 4。
2. **能力分两类但同一类型**：基础能力与技能树被动都是 `CharacterAbility`，
   无法靠类型区分，**只能按字段清单区分**。
3. **二段跳的门槛**：游戏每帧执行 `DoubleJump.SetStateActive(AllowDoubleJump)`，
   而 `AllowDoubleJump` 要求 `PlayerAbilities.DoubleJump.HasAbility`；
   否则 `SeinController.PerformJump` 根本不进二段跳分支。
4. **能力组件的实例化**：`SeinNestedPrefab.IsInstantiated` 的 setter 在置 false 时
   会 `Destroy()` 组件；游戏只在"正规授予能力"时（`PlayerAbilities.SetAbility` →
   `Prefabs.EnsureRightPrefabsAreThereForAbilities`）实例化。因此**直接写标志位后，
   少数能力（滑翔/冲刺/猛击等非默认实例化的）可能要等存档重载或场景切换才实体化**。
   默认实例化的有：Carry/Crouch/Fall/Jump/PushAgainstWall/Run/Idle/StandingOnEdge/
   Swimming/SoulFlame/GrabPushPull/SpiritFlame/PickupProcessor。
5. **跳跃高度不是单一变量**：`SeinJump.PerformJump()` 会分支——移动跳/站立跳
   **轮换** First→Second→Third；贴墙跳用 First；蹲跳用 Crouch；后空翻用 Backflip。
   另外长按跳跃键会持续上升（`JumpSustain`），轻按自然跳得低，这是游戏本身的设计。
6. **成就机制**（改功能前务必知道）：
   - 睡眠总闸：`AchievementsController.AwardAchievement` 被
     `!CheatsHandler.DebugWasEnabled` 拦着——**一旦游戏内调试菜单被启用过，
     之后所有成就都不会解锁**。
   - "完成地图"（`CompleteMapAchievementAsset`）：每 5 秒采样一次
     `GameWorld.CompletionAmount ≈ 1`。**我们的"100% 探索"改的就是这个值，
     所以能解锁它。**
   - "翻遍每一寸土地 / 找齐秘密"（`FindAllSecretsAchievementAsset`）：只在
     `AchievementsLogic.RevealTransparentWall()` 里发放，计数到 **45 面半透明隐藏墙**
     才给。**与地图百分比、与收集品都无关——100% 探索给不了它**，必须自己去穿墙。
   - 二段跳等"控制"作弊会影响成就资格的路子是 `CheatsHandler`，
     而技能树被动的 `HasAbility` 标志不属于该机制。
7. **一命难度的清档链路**：`SeinDamageReciever` 依据
   `DifficultyController.Instance.Difficulty == OneLife` 置 `WasKilled`；
   之后 `currentSaveSlot.Difficulty == OneLife && WasKilled` → 清档。
   成就只读 `LowestDifficulty`（所以保护方案是锁 `Difficulty`、**绝不碰
   `LowestDifficulty`**）。
8. **灵魂链接的两种操作**（同一按键）：轻点（0.3 秒内松开，
   `m_tapRemainingTime > 0`）→ 打开技能树；长按 → 就地建立链接。
   任何在轻点窗口内的蓄力写入都会破坏"轻点"语义。
9. **能力点与升级**：`SeinLevel.LevelUp()` 会 `SkillPoints++` 并实例化
   `OnLevelUpGameObject`（升级特效）。持续写回固定点数会与它循环打架。
10. **游玩时间**：`GameTimer.CurrentTime`（float 秒），挂在 `GameController.Timer`；
    当前两个对象都定位不到（§11）。
11. **探索度**：`GameWorld.CompletionAmount` 是**派生值**（各
    `RuntimeGameWorldArea.m_completionAmount` 的平均），底层数据是"已访问地图面
    + 已发现图标"（`UpdateCompletionAmount`：`(visitedFaces + found) /
    (faceCount + total)`）。直接写 `m_completionAmount` 是改派生值，
    所以要清 `m_dirtyCompletionAmount` 并持续维持。

## §8 踩坑清单（现象 → 根因 → 修法）

> 这一节是本项目最贵的资产。改功能前扫一眼，能省下大量时间。

1. **所有功能写入后毫无效果**
   → 定位到了"传送门克隆体"（`CloneOfSeinForPortals` 的副本 Sein）：结构完整、
   内存合法，但游戏从不使用（**静态引用为零**）。
   → 修：改扫游戏静态字段形成的 `[P,0,0,P]` 模式，并用"低区有引用"作为活体判据。

2. **"一开始能用，死亡重生后彻底失效 / 永远扫描中"**
   → 32 位进程带 `/LARGEADDRESSAWARE`，托管堆到 `0x7Bxxxxxx`；旧代码把"堆指针"
   上界写成 `0x70000000`，**重生后分配到 `0x73xxxxxx` 的对象被一律过滤**。
   → 修：地址范围放宽到完整 32 位用户空间（`maxUserAddr = 0xFFFF0000`）。

3. **类名单例（死亡计数/难度）永远定位不到**
   → mono 类型名字符串是紧凑拼接的，**不保证 4 字节对齐**；旧代码对字符串指针做了
   `&3==0` 校验，导致所有类名解析静默失败。
   → 修：字符串指针用独立的 `isTextPtr`（不校验对齐）。

4. **"用着用着突然失效"**
   → 旧代码缓存**对象地址**，只在结构校验失败时才重扫；角色重生后旧对象在被 GC 前
   内存依旧"看起来合法"，于是继续写一个没用的对象。
   → 修：缓存**静态槽地址**，每周期重读槽内容。

5. **游戏里按 ESC 导致修改器退回选择界面、再按一次直接退出**
   → 界面按键曾是"全局响应"。
   → 修：界面按键改读**控制台输入缓冲**（`CONIN$` + `ReadConsoleInputW`）——
   系统只往拥有键盘焦点的控制台投递输入事件，所以"仅本窗口前台才响应"是机制上的
   天然保证，不再依赖窗口标题/父进程链等启发式。

6. **反复切换会话时修改器偶发静默退出**
   → `BuildFeatures` 重建全局执行器列表时，上一会话的引擎协程仍在遍历同一切片，
   并发读写切片头会 panic 并终止进程。
   → 修：`tickersMu` 保护 + 后台协程 panic 兜底。**执行器/取址也不要绕过锁。**

7. **界面卡死（无响应）**
   → `ReadConsoleInputW` 在输入缓冲为空时会**阻塞**；主循环每帧调用它就卡住了。
   → 修：先 `GetNumberOfConsoleInputEvents` 查事件数，为 0 直接返回。

8. **鼠标拖选窗口后界面冻结**
   → 保留 QuickEdit 时，拖选文本会让后续 `WriteConsole` 阻塞，而本程序持续重绘。
   → 修：输入模式里不启用 QuickEdit。

9. **界面残留 / 两块 UI 重叠**
   → 只重写"有内容的行"再补 `ESC[J`，内容变短时上一帧字符残留。
   → 修：每帧**重写整屏每一行**（不足补空行）+ 每行追加 `ESC[K`；配合备用屏幕缓冲区
   （`ESC[?1049h`）与关闭自动换行（`ESC[?7l`），既不滚动也不残留。

10. **键位撞车（发生过两次）**
    → 功能表有 `NewFeature`（小键盘）/ `NewCtrlFeature`（Ctrl）/ `NewCtrlShiftFeature`
    （Ctrl+Shift）三种构造器；把用错构造器的功能放到别的档位，就会与同数字的功能
    同时触发（曾出现"无限二段跳"与"无限生命"、"无限能力点数"与"无限能量"）。
    → 修：用对构造器；**每次改完键位跑 `probe feats`**，它会打印键位表并自动检测重复。

11. **解析器突然整体定位失败（连活体玩家都找不到）**
    → `validateSein` 曾要求 `Energy.Current <= Max`；外部改写或拾取暂态会让这个关系
    短暂不成立，于是**把真正的玩家对象也拒掉了**。
    → 修：只校验数值在合理范围（非 NaN、0..10000），不做字段间关系校验。

12. **修改器显示 12 血，游戏里只有 3 血**
    → 生命以"点"存储，1 球 = 4 点。
    → 修：UI 按球显示（`HealthPointsPerCell`）。

13. **能力点数一直涨、升级动画刷屏**
    → 游戏 `LevelUp()` 会 `SkillPoints++` 并播特效；每周期强行写回固定值会与它
    循环互相覆盖。
    → 修：改成"不足才补满"（低于目标才写）。

14. **无限二段跳完全无效**
    → 只写 `m_numberOfJumpsAvailable` 不够，`AllowDoubleJump` 还要求
    `PlayerAbilities.DoubleJump.HasAbility`（见 §7-3）。
    → 修：同时把能力开关置 1。

15. **二段跳"关掉再打开就失效"**
    → `SeinNestedPrefab.IsInstantiated` 置 false 会 `Destroy()` 组件；关闭功能时还原
    `HasAbility=0` 会把组件销毁，再开只写标志位无法重建。
    → 修：**关闭时不还原该能力开关**（首次开启即永久授予，副作用已记录）。

16. **建立灵魂链接后进不去技能界面**
    → 功能 4 靠写满 `m_holdDownTime` 实现，在"轻点"窗口内也写，游戏于是把轻点当成
    长按，技能树路径失效（§7-8）。
    → 修：先读 `m_tapRemainingTime`，`> 0` 时完全不干预。

17. **超级跳"每两三次才有一次跳得高"**
    → 跳跃高度分 6 个字段且会轮换，只放大 `FirstJumpHeight` 只能命中一部分（§7-5）。
    → 修：同时放大 6 个字段（0x54/0x58/0x60/0x64/0x70/0x74）。

18. **修改器进程偶发因内存耗尽退出**
    → 早期扫描按"整段区域"一次性 `make([]byte, 区域大小)`，堆变大后可能申请数百 MB。
    → 修：一律 4MB 分块读取。

19. **误以为锚点会"变成垃圾数据"**
    → 曾把指针字节 `20 34 6E 73`（= `0x736E3420`）读成文本 `" 4ns"`，据此
    错判"锚点失效"。实际锚点一直有效，只是高地址值被 §8-2 的上限过滤掉了。
    → 教训：**看到"可疑数据"先按 dword 解读，别按文本解读**；并把范围/对齐假设
    当作首要怀疑对象。

## §9 诊断工具 `probe`（等价 CE 的核心能力）

全部命令都可加 `-vanilla` 切换到原版进程（`ori.exe`）。常用：

| 命令 | 用途 |
|---|---|
| `diag [-wait]` | 跑真实解析器，打印定位结果与快照（**验证解析器最快的入口**） |
| `bases [-xml]` | 打印所有基址符号；`-xml` 直接输出 CE 表的 `<UserdefinedSymbols>` 块 |
| `feats` | 打印功能/键位表并**自动检测键位重复** |
| `feat <数字> [秒] [-ctrl] [-shift]` | 对活体进程运行**真实功能**并打印状态（`-ctrl`/`-shift` 对应档位） |
| `offs <类名> <字段名...>` | **权威字段偏移**（从 mono 元数据读，等价 CE mono dissect） |
| `heapfind <类名>` | 在堆区按类名枚举实例（确认某单例是否存在） |
| `singleton <类名>` | 在低区找"指向该类实例的静态槽"（单例定位） |
| `struct <地址> [字数]` | 转储对象字段并标注指针目标类名 |
| `read/write <地址> <类型> <值>` | 原始读写（类型支持 i8/i16/i32/u32/f32/f64） |
| `fields2 <类名>` | 遍历 klass 的字段数组直出（备用） |
| `findfield <类名> <字段名...>` | 另一条字段查找路径 |
| `live` / `findall <类名>` / `refs <地址>` / `allrefs <地址>` | 活体定位 / 枚举实例 / 查引用 |
| `corereg [地址...]` | 打印修改器实际枚举到的内存段并查询某地址是否被覆盖 |
| `racetest` | 复现"会话切换 + 引擎并发"时序，配合 `-race` 用 |
| `conin` | **确定性自测控制台输入通路**（注入合成按键再读回，无需人工按键） |
| `staticblock` / `diffobj` / `diffname` / `diffstrict` / `onescan` / `staticmap` / `staticref` | 各类定位探索工具，详见源码注释 |

自测样例（不需要游戏也能跑）：

```
probe conin     → 注入 VK 0x41 读回识别 + 空缓冲不阻塞
probe feats     → 键位表 + "无重复 ✓"
```

## §10 维护流程（改功能时的 checklist）

1. **先从源码确认机制**：ILSpy 打开 `Assembly-CSharp.dll`，找到对应类/方法，
   看清"游戏到底读/写哪个字段、有哪些前置条件"。
2. **用元数据确认偏移**：`probe offs <类> <字段>`（**不要按源码顺序推算**）。
   新偏移写进 `ori/offsets.go`（唯一维护点），并把来源/用途写进注释。
3. **实现执行器**：在 `features.go` 里按语义选执行器，注意
   - 取址一律走 `Runtime.Addrs()/SubAddr()`（加锁）；
   - 需要还原的用 `OnDeactivate` 并在 `DeactivateFeature` 的 switch 里登记；
   - 幂等/一次性动作用"开启期间维持、关闭不动"。
4. **构建**：`cd GoTrainer && go build ./... && go vet ./...`
5. **键位/顺序自检**：`probe feats`（确认无重复、档位正确、界面顺序由简到繁）。
6. **实机验证**：`probe feat <数字> [秒] [-ctrl|-shift]`，看状态与目标内存值；
   **写入型功能要验证"改回去/还原"也确实生效**。
7. **同步文档**：
   - README：§5 偏移表、§6 功能表、§7 机制、§8 新踩的坑；
   - `ctables/ori_de.ct`：新增/修改对应记录（字段名与偏移与 `offsets.go` 一致）；
     换会话后表可用 `probe bases -xml` 一键更新基址符号；
   - 原版相关改动若未实测，**必须在 `ori_vanilla.ct` 里标注"未核验"**。
8. **提交**：说明"现象→根因→修法"，便于回溯。

键位分配规则：**优先小键盘 1-9/0；放不下的接 Ctrl+小键盘 1..N；特殊能力用
Ctrl+Shift+小键盘**。每档内顺序编号、不留空位。

**界面显示顺序必须与键位复杂度一致**（从一般到特殊）：

```
小键盘（基础能力） → Ctrl+小键盘（进阶能力） → Ctrl+Shift+小键盘（特殊能力）
```

功能表（`features.go` 的 `BuildFeatures`）按这个顺序排列；一命保护这类"最特殊"的
功能由 `main.go` 的 `navList` **追加到列表末尾**——不要再把它插进中间档位之间
（曾经它被插在小键盘组和 Ctrl 组之间，与"由简到繁"的顺序不一致）。
`probe feats` 会自动检查这一点（输出"顺序检查: 档位由简到繁 ✓"）。

## §11 未解决 / 待办

**重置时间** 与 **一命保护** 都卡在同一个问题上：目标对象定位不到。

- 时间载体：`GameTimer.CurrentTime`（float 秒），挂在 `GameController.Timer` 上。
- 一命保护载体：`DifficultyController`。
- 现状：`GameController` / `GameTimer` / `DifficultyController` 在运行中的进程里
  **都找不到活体实例**——全堆无过滤扫描（`probe heapfind`）只找到过若干
  `DifficultyController` 名字的孤立/垃圾对象（`probe allrefs` 显示**全内存零引用**），
  说明 `Instance` 静态字段并没有指向一个可定位的实例。
- **已找到的可行线索**：
  `SaveSceneManager`（**可以定位**，低区静态槽）持有 `List<SaveId> SaveData`(+0x10)，
  每项的 `SaveObject` 就是**游戏全部可存档对象**——`GameTimer` /
  `DifficultyController` / `GameController` 应该都在其中。顺着这个列表枚举并回读
  类名，即可取出它们，从而一次解决两个功能。
  相关源码：`SaveSceneManager.SaveData`、`SaveId.Id/SaveObject`。

**原版（ori.exe）**：字段偏移全部未核验，功能未实测。核验路径见 `ori_vanilla.ct`
的注释（`probe offs ... -vanilla`）。

**探针/脚本的小限制**：`probe` 的部分命令（`singleton`/`diffobj` 等）是围绕终极版
观察写的，用于原版前应先跑 `diag -vanilla` 确认定位链路可用。

## §12 资料与来源

- **反编译**：`<游戏目录>/OriDE_Data/Managed/Assembly-CSharp.dll`（无混淆），
  用 ILSpy 反编译。本 README 中所有"源码依据"均出自它。
- **第三方 CE 表**（用于交叉验证，原始文件已按"只保留两张表"的要求删除，
  可在 git 历史 `2e91d8c`（初始提交）找回）：
  - `attach_3834.ct`：**原版**指针链（`"ori.exe"+00A36164` 死亡数、
    `"mono.dll"+001F42C4` 能力点/灵魂点）——已转录进 `ori_vanilla.ct`。
  - `attach_3943.ct`：能力标志读取脚本（偏移与我们核验的 11 项基础能力**完全一致**）
    + 生命/能量 AOB 脚本——要点已记录在 §5 / `ori_vanilla.ct`。
  - `attach_12228.ct`：CE v27 mono 表（`usemono()` + `define`），478 项，
    含 SeinCharacter/Abilities/Jump 全字段，可作将来核验原版/写 AOB 的参考。
- **本项目的教训来源**：全部为本项目实测（见 §8）。

---

# 三、构建 / 目录 / 边界

## 构建

```bash
cd GoTrainer
go build -o GoTrainer.exe .      # 修改器本体（Windows 控制台 TUI）
go build -o probe.exe ./cmd/probe  # 诊断工具
go vet ./... && gofmt -l .        # 提交前检查
```

## 目录

```
GoTrainer/
  main.go              TUI + 按键 + 会话协程
  core/memory.go       进程/内存/控制台输入
  ori/resolver.go      对象定位
  ori/features.go      功能与执行器
  ori/offsets.go       ★ 唯一地址维护点
  cmd/probe/main.go    诊断工具
ctables/
  ori_de.ct            终极版字段对照表（与修改器同步）
  ori_vanilla.ct       原版表（偏移未核验，含第三方原版指针链）
```

## 已知边界

- 仅支持 32 位 mono 目标；地址策略与 WoW64 零扩展绑定。
- 需要管理员权限（`OpenProcess` 读写目标内存）。
- 原版未验证（见 §11）。
- 成就相关：若游戏内调试菜单被启用过（`CheatsHandler.DebugWasEnabled`），
  **任何修改器操作都不会影响成就**（因为游戏自己就不再发放）。
