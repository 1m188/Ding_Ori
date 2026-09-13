# OriTrainer (Go) — 奥日与迷失森林 双版本修改器

风灵月影风格的 TUI 修改器，一个 exe 支持两个版本：

- **原版**《Ori and the Blind Forest》（`ori.exe`）
- **终极版**《Ori and the Blind Forest: Definitive Edition》（`oriDE.exe`）

**纯 Go 标准库实现（零外部依赖），单 exe 发布。**
本 README 分两部分：前面是**使用文档**，后面是**维护手册 / 逆向知识库**——
后者记录了本项目所有逆向结论、踩过的坑与维护流程，目的就是让**完全不了解
上下文的新会话**能在最短时间内接手。

> 当前状态：**终极版与原版功能均已实测可用**。原版核验于 2026-09（Unity 5.0 /
> mono 地址空间与终极版不同，见 §2、§8-27）；两版字段偏移差异只有 `SeinJump`
> 与 `PlayerAbilities` 的基础能力清单，其余同表通用（§5）。

---

# 一、使用文档

## 使用

1. 以管理员权限启动 `oritrainer.exe`（`go build .` 的默认产物名），在版本选择界面用 ↑↓ 选择，回车进入。
2. 启动对应游戏并进入存档。修改器自动附加，约 **0.2–1 秒**内定位活体玩家对象
   （横向对比：早期版本是全堆扫描 9–15 秒，且经常定位失败）。
   死亡数 / 探索度 / 计时器这类**单例**是后台异步定位的：终极版几秒内完成，
   **原版低区约 160MB，需要 15–25 秒**——这期间对应功能显示"定位中"，
   扫描完成后自动生效，不需要重开修改器（见 §2、§8-27）。
3. ESC 可随时返回版本选择（仅在修改器窗口有焦点时生效）。

## 热键（两档）

键位分两档，每档内按顺序编号，**无空位、无重复**。两档互斥由"修饰键状态精确
匹配"保证——按 Ctrl+小键盘 N 时小键盘 N 不会触发；按住 Shift 时 Ctrl 档也不触发。

> 为什么没有 Ctrl+Shift 档：Windows Terminal 默认把 `Ctrl+Shift+1..9` 绑成
> "新建标签页"，会把按键在送到程序之前吃掉。详见维护手册 §8-23。

**普通功能 —— 小键盘 1-9（终极版）**

| 热键 | 功能 | 状态 |
|---|---|---|
| 小键盘 1 | 无限生命 | ✓ 实测（按"球"显示，补满到上限） |
| 小键盘 2 | 无限能量 | ✓ 实测（上限为 0 时提示暂无可补） |
| 小键盘 3 | 灵魂链接无需冷却 | ✓ 实测（冷却归零） |
| 小键盘 4 | 可在不安全区域建立灵魂链接 | ✓ 实测 |
| 小键盘 5 | 超级跳 | ✓ 实测（同时放大 6 个跳跃高度字段） |
| 小键盘 6 | 无限二段跳 | ✓ 实测（同时开启二段跳能力） |
| 小键盘 7 | 无限冲刺 | ✓ 只授予基础冲刺能力 + 解除冲刺次数限制（**不碰技能树/AirDash**）；自购空中冲刺后空中同样无限 |
| 小键盘 8 | 无限能力点数 | ✓ 实测（不足才补满，目标 99） |
| 小键盘 9 | 显示地图 | ✓ 实测（地形+图标+无迷雾；**仅本次游戏有效，重启还原**；不影响成就） |

**特殊功能 —— Ctrl+小键盘 1..6（终极版）**

> **原版（ori.exe）键位**：原版没有冲刺，也没有难度/一命机制，因此
> **去掉"无限冲刺"与"一命保护"，其余按键按顺序顺延**：
>
> | | 小键盘 | Ctrl+小键盘 |
> |---|---|---|
> | 终极版 | 1-6 同上，7 无限冲刺，8 无限能力点数，9 显示地图 | 1-5 同上，6 一命保护 |
> | 原版 | 1-6 同上，**7 无限能力点数，8 显示地图** | **1-5 同上**（无 6） |
>
> 原版的"解锁全部基础技能"补授 **9 项**（无 Grenade/Dash）。
> 修改器会按版本自动裁剪功能表，见 `BuildFeatures(prof)`。

| 热键 | 功能 | 状态 |
|---|---|---|
| Ctrl+小键盘 1 | 死亡数归零 | ✓ 实测 |
| Ctrl+小键盘 2 | 100% 探索 | ✓ 实测（区域完成度置 1） |
| Ctrl+小键盘 3 | 解锁全部基础技能 | ✓ 实测（11 项，只给基础能力） |
| Ctrl+小键盘 4 | 重置时间 | ✓ 字段/写入已实测（`GameTimer.CurrentTime=0`）；暂停界面同步经源码确认 |
| Ctrl+小键盘 5 | 获得三把钥匙 | ✓ 实测（`Keys` 三个静态标记置 1；等价于捡起钥匙，不跳过副本） |
| Ctrl+小键盘 6 | 一命保护（死亡不清档） | ✓ 实测（锁 `Difficulty=Easy`，死亡正常复活；`LowestDifficulty` 只读保留） |

**界面按键（仅修改器窗口有焦点时生效）**

| 键 | 作用 |
|---|---|
| ↑↓ | 选择功能项 |
| 回车 / 空格 | 激活或取消选中功能 |
| HOME / F12 / F1 / ESC | 全开/全关 切换 / 重扫 / 帮助 / 返回版本选择 |

> 没有"按键直接退出程序"：退出用窗口关闭按钮即可（END 曾绑定退出，已移除；
> 原因见 §8-23）。版本选择界面也只有 ↑↓ + 回车，不含退出键。

**离开运行状态时的收尾（统一规则）**：任何离开路径——**ESC 返回版本选择**、
**关闭窗口/注销/关机**——都会执行同一次收尾（`teardownSession` → `DeactivateAll`）：

- **能复原的复原**：如超级跳的 6 个跳跃高度、灵魂链接的 `HoldDownDuration`；
- **一次性修改型不动**：解锁技能、三把钥匙、重置时间、100% 探索、能力点数等
  （一命保护难度也在其中，但仅终极版；设计如此，且会被游戏存档带进存档）；
- **HOME = 全开/全关切换**（不离开会话）：只要还有功能没开，就全部打开（已开的跳过）；
  已经全开时则关闭全部（"关"= 同样的还原/关闭逻辑）。

唯一抓不到的是**任务管理器"结束任务"/强杀**（收不到控制台关闭事件），那种情况
可能残留可还原型的值（最典型是超级跳），重启游戏即恢复。

## 功能语义

- **无限生命 / 无限能量 = 持续补满到上限**：每周期读上限并写满当前值。
  残血/空能量时激活也会立即补满。
- **超级跳 / 无限二段跳 = 捕获原值后放大 / 维持，关闭时还原**。
- **无限冲刺（仅终极版）= 只解锁基础冲刺 + 解除冲刺次数限制**：授予 `PlayerAbilities.Dash`
  （基础能力，关闭不还原，§8-15），再持续清 `m_hasDashed` 使冲刺不再"空中限一次"
  ——**只要游戏允许在此处冲刺就无限**（地面任何时候；空中需**自己在技能树购买**
  AirDash，本功能绝不代授/代买，只读它做状态提示）。详见 §7-15。
- **无限能力点数 = 不足才补满**：低于 99 才写，达到或超过不动（原因见 §8-13）。
- **显示地图 = 打开游戏的"未探索地图可见"显示位**：地图绘制把所有区域/面按已发现
  处理（地形+图标+无迷雾），观感等同于"插了所有地图石"，世界地图与区域地图
  （含元素门内部）都适用。**仅本次游戏进程有效，重启游戏后还原**；纯显示、
  不写存档、不影响完成度与成就（§7-16）。
- **100% 探索 / 解锁全部基础技能 = 一次性动作型**：开启期间维持，关闭不做任何事，
  再次开启仍会把缺的补上。
- **死亡数归零 = 固定目标值**：无条件写 0，适合"无死亡通关"成就。
- **重置时间 = 冻结在 0**：开启期间每周期把 `GameTimer.CurrentTime` 写 0，
  游戏时间恒为 `0:00:00`；关闭后从 0 重新累计。暂停界面 "花费时间" 由
  `TimeCounterDisplay` 每 1 秒读一次，故归零后最多 1 秒同步（详见 §7-13）。
- **一命保护（仅终极版）= 只在"一命存档"上把 `Difficulty` 顶成 Easy**：死亡判定读的就是
  这个字段，变成 Easy 后"清档链"整条不成立，死亡退化为普通复活；顺带享受 Easy
  的伤害减半/敌人血量降低。**`LowestDifficulty`（成就判定字段）只读保留**。
  关闭功能不还原难度（保护是粘性的，避免误关就恢复一命）。详见 §7-7。
- **获得三把钥匙 = 把三个静态 bool 置 1**：`Keys.GinsoTree` / `ForlornRuins` /
  `MountHoru`（在 mono 类静态数据块里，非对象链）。等价于走过去捡起钥匙，
  **不跳过副本内容**，风险低；关闭不撤销，且会随存档持久化。详见 §7-14。
  ⚠ 与之相邻的"三个元素恢复"标记（`Events.WaterPurified/WindRestored/WarmthReturned`）
  会驱动 `WorldProgression`，**不提供该功能**（跳过副本易卡关），原因见 §7-14。

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
5. 字段偏移**不能靠"源码声明顺序 + 想当然的对象头大小"推算**（mono 按字段
   大小分组重排，首个字段偏移也不是 0x08），必须用 `probe offs` /
   `probe fieldoff` / `probe snap` 从运行中的 mono 元数据里读（见 §4）。

**改一个功能的标准流程**（详见 §10）。

**代码地图**：

| 文件 | 职责 |
|---|---|
| `GoTrainer/main.go` | TUI 渲染、按键（控制台输入 + 全局热键）、会话协程 |
| `GoTrainer/core/memory.go` | 进程附加、RPM/WPM、区域枚举、控制台输入、存档回滚工具 |
| `GoTrainer/ori/resolver.go` | 对象定位（活体玩家 / 各单例）、快照读取 |
| `GoTrainer/ori/features.go` | 功能定义与执行器（冻结/补满/放大/置位/遍历） |
| `GoTrainer/ori/offsets.go` | **唯一地址维护点**：所有字段偏移常量（含按版本取值的那几个） |
| `GoTrainer/ori/diag.go` | `probe snap` / `probe fieldoff` 的只读核验实现 |
| `GoTrainer/ori/resolver_profile_test.go` | 回归测试：版本 ↔ 对象下界/vtable 带 |
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

**地址空间是 32 位 + `/LARGEADDRESSAWARE`**。**两个版本的地址带完全不同**，
因此对象下界与 vtable 扫描带都按版本设置（`ApplyProfile`）：

| 地址带 | 终极版（Unity 5.3） | 原版（Unity 5.0） |
|---|---|---|
| 托管堆对象 | `0x50xxxxxx`–`0x7Bxxxxxx` | **`0x01xxxxxx` 起（低地址！）** |
| mono 元数据 klass / 字段描述符 | `0x2Axxxxxx`–`0x2Bxxxxxx` | 描述符 `0x27Axxxxx`、klass `0x35B7xxxx` |
| vtable / mono 运行时结构 | `0x2Axxxxxx`、`0x50xxxxxx`–`0x53xxxxxx` | `0x35B7xxxx` 段 |
| 低区静态数据 / JIT | `< 0x10000000` | `< 0x10000000`（原版低区约 160MB） |

要点：

- **原版托管堆在低地址**（实测玩家对象 `0x01A8E6C0`）：沿用终极版的
  `0x40000000` 对象下界会**一个对象都认不出来**（见 §8-27）。
- **原版 vtable 在 `0x35B7xxxx`**：`findStaticData` 的扫描带若只允许终极版的
  `0x2A/0x50` 段，`Keys` 静态块永远定位不到。

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

`SeinDeathCounter` / `GameWorld` / `GameTimer` 等单例的做法：扫低区（`< 0x10000000`）中
"值能解析成目标类名"的槽位，缓存槽地址并在每周期重读；槽失效则清空重扫（带指数退避）。

**注意**：
- 终极版能定位 `GameWorld` / `SeinDeathCounter` / `GameTimer` / `DifficultyController`；
  其中 `GameTimer` / `GameController` / `DifficultyController` 的对象在 `0x2A/0x2F`
  段，低于终极版下界 `minObjAddr(0x40000000)`，必须用 `minObjLowAddr` 才能扫到
  （见 §8-22）。**原版没有 DifficultyController**，且原版下界本身就只有 `0x10000`，
  这些对象都在低区堆内，不存在"低于下界"的问题（见 §8-27）。
- `GameController` 的静态槽也能扫到（终极版实测 `0x063D3D80 -> 0x2AC03C00`），
  只是修改器目前不需要它；`GameController.Timer` 指向的 `GameTimer` 可直接用
  上面的方式拿到。

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

`probe offs <类名> <字段名...>` 就是这个逻辑 + 按声明类过滤；
`probe fieldoff <类名> <字段名>` 是它的单字段快查版（对 `Max` 这类常见名更快）。

⚠⚠ **不要按"源码声明顺序 + 对象头大小"推算偏移**：
- mono **按字段大小分组**布局（4 字节一组、更小的排后面），所以混类型的类
  光看声明顺序就会算错；同尺寸字段在实测的两个 build 里恰好保持了声明顺序，
  但这是观察结果、不是保证。
- **首个字段偏移不是 0x08**：`PlayerAbilities` 首字段实测就是 `+0x14`。
  于是 `DoubleJump`（声明第 5 个）若按"0x08 + 4×4"推会得 `+0x18`，
  **实际是 `+0x24`**（`DoubleJumpUpgrade` 同理：推算 `+0x48`、实际 `+0x54`）。
- 结论不变：**必须查元数据**（`probe offs` / `probe fieldoff` / `probe snap`）。

## §5 字段偏移总表（**终极版 / 原版通用**，均已由元数据+活体内存双重核验）

> 原版核验（2026-09）：除下表标注外，**所有偏移与终极版一致**（两个版本这些类的
> 字段声明顺序相同，mono 重排结果也就相同）。仅 `SeinJump` 的全套高度偏移、
> 以及 `PlayerAbilities` 的 Grenade/Dash 两项不同（原版没有这两个字段）。

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
| SeinLevel | Current | 0x28 | int | **等级；技能树开启要求 > 0**（新存档为 0） |
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
| List\<T\> | _items | 0x08 | ptr | **指向数组对象**（不是元素首地址！） |
| List\<T\> | _size | 0x0C | int | 元素个数 |

> ⚠ **数组元素从 `_items + 0x10` 开始**：数组对象头部是
> `vtable(0) / monitor(4) / bounds(8) / max_length(0xC)`，元素区在 `+0x10`。
> 把 `_items` 当元素首地址（`+i*4`）会让所有元素**错位 4 个**——既漏写后半段、
> 又会把值写进数组类的元数据区（见 §8-20，实际踩过）。

### GameTimer（游玩计时器）

由 `GameTimer.Instance`（静态单例）或 `GameController.Timer(+0x14)` 定位；
对象实测可能落在 `0x2Fxxxxxx` 段（低于常规托管堆下限 `0x40000000`，见 §8-22）。

| 字段 | 偏移 | 说明 |
|---|---|---|
| CurrentTime | 0x1C | float 秒，累计游玩时间（`FixedUpdate += deltaTime`） |
| m_waitTillSave | 0x20 | float ∈[0,1]，内部每秒刷新节流（**只读校验用**） |
| m_sendTelemetryTimer | 0x24 | float ∈[0,60]，遥测发送计时（**只读校验用**） |

> ⚠ 原版该类字段顺序不同：`+0x24` 落在 `m_builder` 指针上，`m_sendTelemetryTimer`
> 不在该位置。`scanLowBandAux` 的 GameTimer 预筛在原版只用 `CurrentTime` +
> `m_waitTillSave` 两个字段（见 §8-27）。

> 暂停界面显示的"花费时间" = `GameTimer.DisplayTimeAsString`（由 `CurrentTime`
> 派生，只显示分钟数）；`TimeCounterDisplay.Update()` 每 1 秒把它写进 GUIText。
> 另有 `m_builder`（`GameTimer+0x14` 的 StringBuilder）保存 `H:MM:SS` 调试串，
> 实测字符串与 `CurrentTime` 一致，是识别该字段的旁证。

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

**两版偏移不同**（终极版多出 `SpinJumpSoundProvider` / `m_currentJumpingMaterial`
两个字段，使后续字段整体后移）——代码里用 `JumpHeightOffsets(version)` 取，
不要直接写死：

| 字段 | 终极版 | 原版 | 用在哪 |
|---|---|---|---|
| BackflipJumpHeight | 0x54 | **0x50** | 转身后空翻 |
| CrouchJumpHeight | 0x58 | **0x54** | 蹲跳 |
| FirstJumpHeight | 0x60 | **0x5C** | 移动跳/站立跳 轮换第 1 段 |
| JumpIdleHeight | 0x64 | **0x60** | 备用高度 |
| JumpImpulse | 0x68 | **0x64** | 起跳冲量（**当前未被任何功能修改**） |
| SecondJumpHeight | 0x70 | **0x68** | 轮换第 2 段 |
| ThirdJumpHeight | 0x74 | **0x6C** | 轮换第 3 段 / 贴墙跳 |

> 超级跳在**两版都放大 6 项**（不含 JumpImpulse）；原版偏移由
> `probe findfield SeinJump ... -vanilla` 采集。

### SeinDoubleJump

| 字段 | 偏移 | 说明 |
|---|---|---|
| JumpStrength | 0x38 | 二段跳强度 |
| m_doubleJumpTime | 0x3C | |
| m_numberOfJumpsAvailable | 0x40 | 剩余跳跃次数（int） |
| m_remainingLockTime | 0x44 | |

### SeinDashAttack（冲刺，`SeinAbilities+0x80`）⚠仅终极版

组件只有**获得冲刺能力后**才实例化（未解锁时 `Abilities+0x80 == 0`）。

| 字段 | 偏移 | 说明 |
|---|---|---|
| m_lastDashTime | 0xAC | float；`DashHasCooledDown = Time.time - 此值 > 0.4`（冲刺间隔） |
| m_hasDashed | 0xB1 | bool；**本次未落地前是否已冲刺**（只在 `IsOnGround` 时清零） |

> `SeinAbilities` 里还有 `Dash` 之外的一批组件（`+0x08` SeinDoubleJump、`+0x0C` SeinJump …）。
> 空中冲刺的许可还额外要求 `PlayerAbilities.AirDash`（`playerab+0xB8`，CharacterAbility，
> `HasAbility` 在 +0x08）。

### PlayerAbilities

- 能力字段全部是 `CharacterAbility` 引用，从 `+0x14` 起连续排列：
  **终极版 43 项（0x14..0xBC）/ 原版 37 项（0x14..0xA4）**；
  `CharacterAbility` 内 `HasAbility` 在 **+0x08，1 字节 bool**。
- **基础能力** ← 修改器"解锁全部基础技能"只给这些（**终极版 11 项 / 原版 9 项**，
  原版没有 Grenade/Dash 两个字段）：

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
| 0xA8 | Grenade | 光芒爆裂（仅终极版） |
| 0xAC | Dash | 冲刺（仅终极版） |

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
| 不安全区域建链接 | 按住期间 `soulflame+98（HoldDownDuration）= +Inf`，再 `soulflame+94 = 1.0`、`+C0 = 0` | 必须先检查 `m_tapRemainingTime(+B8) <= 0`，否则轻点开技能树失效（§8-16）；只写 1.0 会被同帧蓄力回退扣掉，故用 +Inf 抑制回退（§8-25）；**每次按键只施放一次**（施放判定不含冷却，连写会刷存档） |
| 超级跳 | 同时放大 6 个高度字段（终极版 0x54/0x58/0x60/0x64/0x70/0x74；**原版 0x50/0x54/0x5C/0x60/0x68/0x6C**，见 `JumpHeightOffsets`），关闭还原 | 只放大一个会出现"有的跳得高有的照旧"（§8-17） |
| 无限二段跳 | `playerab+24 → +8 = 1`（能力开关）+ `doublejump+40 = 999` | 只写次数不够（§8-14）；**关闭时不还原能力开关**（§8-15） |
| 无限冲刺 | `[playerab+AC]+8 = 1`（只授基础 Dash）+ `dash+0xB1 = 0` | **仅终极版**（原版无 Dash 字段/组件，功能会被裁掉）；**不碰技能树**（AirDash 只读作提示）；自购 AirDash 后空中同样无限（§7-15）；组件未实例化时需触发一次（读档/买技能） |
| 无限能力点数 | `level+24`，`< 99` 才写；并把 `level+28`（等级）从 0 修正为 1 | 持续写会与升级结算打架（§8-13）；等级为 0 时技能树打不开（§8-21） |
| 显示地图 | `[AreaMapUI+50]（AreaMapDebugNavigation）+0x20 = 1` | 纯显示位；**不写存档、重启还原**；不影响成就/完成度（§7-16） |
| 死亡数归零 | `death+14 = 0` | |
| 100% 探索 | 遍历 `gw+18` 列表，元素从 `[_items]+0x10` 起，每个区域 `+14 = 1.0`、`+18 = 0` | 元素基址易错（§8-20）；只能解锁"完成地图"成就，不是"找齐秘密"（§7-6） |
| 解锁全部基础技能 | `BaseAbilityOffsets(version)`: 终极版 11 个 / **原版 9 个** `[playerab+偏移]+8 = 1` | 只写 1 字节；**部分能力需读档或进技能树买一个技能后才生效**（状态栏已提示） |
| 重置时间 | `timer+1C = 0.0` | 每周期写 = 冻结在 0；对象每周期从静态槽重读（`TimerAddr()`，换场景会重建）；原版辅助扫描较慢，附加后前 ~25 秒显示"定位中" |
| 获得三把钥匙 | `Keys` 静态块 `+0/+1/+2 = 1` | 纯静态类字段（非对象链），由 `KeysAddr()` 定位并缓存；三个元素标记刻意不做（§7-14） |
| 一命保护 | `diffc+18 = 0`（Easy），仅当 `diffc+1C（LowestDifficulty）== OneLife` 时 | **仅终极版**（原版无 DifficultyController/OneLife 类，功能会被裁掉）；死亡判定读 `Difficulty`，改它即可断掉清档链；**永不写 `+0x1C`**（成就判定字段）。见 §7-7 |

所有执行器一律通过 `Runtime.Addrs()` / `SubAddr()` 取地址（内部加锁）——
**不要直接读 Runtime 字段**，后台刷新会并发改写（§8-6）。

## §7 游戏内部机制事实（改功能前必读）

> 机制依据是**终极版**反编译源码（`Assembly-CSharp.dll`）。标 **⚠仅终极版**
> 的条目在原版不存在（原版没有冲刺、难度/一命机制）。两版共有的机制（生命单位、
> 能力两类同型、二段跳门槛、灵魂链接轻点/长按、钥匙静态类等）已在 ori.exe 上
> 用 `probe snap -vanilla` 复核过偏移与取值。

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
   少数能力（滑翔/猛击等非默认实例化的）可能要等存档重载或场景切换才实体化**
   （原版基础能力只有 9 项，没有冲刺/Grenade）。
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
7. **一命难度的清档链路**（"一命保护"实现依据）⚠仅终极版：
   - 死亡时 `SeinDamageReciever.OnKill`：`if (DifficultyController.Instance.Difficulty
     == OneLife) { CurrentSaveSlot.WasKilled = true; SaveGameController.PerformSave();
     SaveSlotBackupsManager.DeleteAllBackups(); }`，随后 `OnKillRoutine` 再按同一个
     条件决定弹 GameOver 还是普通复活。
   - 真正清档发生在**下次读档**：`SaveWasOneLifeAndKilled => currentSaveSlot.Difficulty
     == OneLife && currentSaveSlot.WasKilled` → `ClearSaveSlotForOneLife`。
   - 所以**唯一闸门**是死亡那一刻的 `DifficultyController.Instance.Difficulty`。
     把它顶成 `Easy(0)`，上述分支全不成立：不清档、不删备份、不弹 GameOver，
     死亡退化为普通复活。
   - **成就只看 `LowestDifficulty`**（`BeatOneLifeAchievementAsset`，`+0x1C`），
     方案是锁 `Difficulty(+0x18)`、**绝不碰 `LowestDifficulty`**。
   - 难度选择/伤害分支里只有 `Easy`/`Hard` 有特殊逻辑，`OneLife` 与 `Normal`
     走默认；因此锁 Easy 顺带得到 Easy 的伤害减半/敌人血量 ×0.65，且
     **一命与普通在伤害上本来就没有区别**。
   - 副作用（已知并接受）：存档时 `SaveSlotInfo.FillData()` 会把控制器 Difficulty
     抄进存档槽 → 槽内难度标签变成 Easy、并且 `SaveWasOneLifeAndKilled` 从此为假
     （保护变"粘性"，关掉也仍不清档，直到从菜单"重开一命"）。
     一命存档本来不允许复制（复制会删源槽），这一限制也会随之失效。
8. **灵魂链接的两种操作**（同一按键）：轻点（0.3 秒内松开，
   `m_tapRemainingTime > 0`）→ 打开技能树；长按 → 就地建立链接。
   任何在轻点窗口内的蓄力写入都会破坏"轻点"语义。
9. **能力点与升级**：`SeinLevel.LevelUp()` 会 `SkillPoints++` 并实例化
   `OnLevelUpGameObject`（升级特效）。持续写回固定点数会与它循环打架。
10. **游玩时间**：`GameTimer.CurrentTime`（float 秒，`GameTimer+0x1C`），
    `GameController.Timer(+0x14)` 指向该对象；`GameTimer` 自身有静态单例
    `GameTimer.Instance`，所以即使 `GameController` 定位不到也能直接定位计时器
    （见 §3.3、§7-13）。
11. **探索度**：`GameWorld.CompletionAmount` 是**派生值**（各
    `RuntimeGameWorldArea.m_completionAmount` 的平均），底层数据是"已访问地图面
    + 已发现图标"（`UpdateCompletionAmount`：`(visitedFaces + found) /
    (faceCount + total)`）。直接写 `m_completionAmount` 是改派生值，
    所以要清 `m_dirtyCompletionAmount` 并持续维持。
    注意 `GameWorld.CompletionPercentage` 的实现：**只有 `CompletionAmount ≈ 1`
    才返回 100，否则钳到 [0,99]** —— 所以少写一个区域，界面就永远到不了 100。
12. **技能树的开启门槛**：`SeinSoulFlame.AllowedToAccessSkillTree =>
    m_sein.Level.Current > 0 && IsSafeToCastSoulFlame == Safe`。
    而 `SeinLevel.Current`（等级）只在 `LevelUp()` 里 `++`，**全新存档从未升级时
    它就是 0** → 技能树一直打不开、能力点花不出去。所以在存档点"轻点"想开技能树
    却没反应时，先查 `level+0x28` 是不是 0。
13. **游玩计时器 GameTimer**（"重置时间"依据）：
    - `FixedUpdate()` 里 `CurrentTime += Time.deltaTime`；主菜单 / 扩展标题界面 /
      加载中会提前 `return`，**暂停时 `Time.timeScale=0 → deltaTime=0` 也不增长**。
      所以"暂停界面看时间不动"是正常的，不代表字段找错。
    - `Reset()` 就是 `CurrentTime = 0`；`Serialize()` 也序列化该字段（死亡/存档点
      会把当前值写进存档）——直接写 0 与游戏自身语义一致。
    - 暂停界面："花费时间"文本由 `TimeCounterDisplay.Update()` 每 1 秒读
      `GameController.Instance.Timer.DisplayTimeAsString` 刷新，而
      `DisplayTimeAsString` 完全由 `CurrentTime` 派生 → **写 0 后最多 1 秒同步**。
    - 调试旁证：`m_builder`（`GameTimer+0x14` 的 StringBuilder）内容形如 `H:MM:SS`，
      用于遥测；实测它与 `CurrentTime` 对得上（本次定位时读到 `0:03:30` ↔
      `CurrentTime = 211.49s`）。

14. **世界状态：三把钥匙 / 三个元素（暂停界面右下六个图标）**
    这些是**纯静态类**的 `static bool`，不在任何对象链上：

    ```
    public static class Keys   { GinsoTree; ForlornRuins; MountHoru; }        // 三把钥匙
    namespace Sein.World { static class Events {
        GinsoTreeEntered; MistLifted; WaterPurified; WindRestored; GumoFree;
        SpiritTreeReached; WarmthReturned; DarknessLifted; m_gravityActivated; } } // 世界事件
    ```

    - 暂停界面图标由 `WorldState` 枚举 + `SeinWorldStateCondition` 条件读取它们
      （`WorldState.GinsoTreeKey → Keys.GinsoTree`、`WaterPurified → Events.WaterPurified` …）。
    - 由 `SeinWorldState.Serialize()` 写进存档；`OnGameReset()` 会把它们全部清零
      （回到标题/重开时）。游戏自己的 `SetSeinWorldStateAction` 就是触发器用的赋值入口。
    - **字段地址 = `u32(MonoVTable + 0x0C) + 字段偏移`**（mono 类静态数据块）。
      我们已实现 `Runtime.KeysAddr()`：按字段名反查描述符 → klass → 在 vtable 带找
      MonoVTable（`u32(V)==klass` 且 `+0x0C` 指向"字节全 ≤1"的小块）→ 静态块基址。
      实测 `Keys` 偏移: GinsoTree+0 / ForlornRuins+1 / MountHoru+2；
      `Events` 偏移: GinsoTreeEntered+0 / MistLifted+1 / WaterPurified+2 /
      WindRestored+3 / GumoFree+4 / SpiritTreeReached+5 / WarmthReturned+6 /
      DarknessLifted+7 / m_gravityActivated+8。
    - **"获得三把钥匙"只置前三个 bool**（低风险，等价捡钥匙）。
      三个元素标记**故意不做**：它们驱动 `WorldProgression`（由这些标记反推），
      会连带影响场景触发/过场/传送；强行置位却不做副本，**内容与能力不会获得、
      世界状态自相矛盾，容易卡关**（详见 §8-24 的风险记录）。

15. **冲刺为什么只能在空中冲一次（"无限冲刺"依据）⚠仅终极版**
    `SeinDashAttack.CanPerformNormalDash()`：

    ```
    if (!HasAirDashSkill() && !IsOnGround) return false;   // HasAirDashSkill = PlayerAbilities.AirDash.HasAbility
    return !AgainstWall() && DashHasCooledDown && !m_hasDashed;
    ```

    而 `UpdateNormal()` 里**只有** `if (m_sein.IsOnGround) m_hasDashed = false;`，
    即 `m_hasDashed` 只在落地时清零——这就是"空中一次、落地刷新"的来源。
    持续把 `m_hasDashed(SeinAbilities.Dash +0xB1)` 写 0 即可无限空中冲刺：
    - 触发仍需玩家按键（`Core.Input.RightShoulder.Pressed` + 0.15s 内），不会自己连发；
    - `DashTime(0.5s) > DashHasCooledDown(0.4s)`，冲刺结束冷却已过，不卡帧；
    - `AgainstWall()` / 能量消耗等其它条件照旧（贴墙走墙冲，冲刺仍耗能量）。

    **能力授予只做最小化**：只把 `PlayerAbilities.Dash(playerab+0xAC)` 的
    `HasAbility` 置 1（基础能力，关闭不还原，§8-15）；**`AirDash(playerab+0xB8)`
    只读、绝不写**——"空中冲刺"是技能树内容，由玩家自己购买。玩家买下 AirDash 后，
    本功能的无限次冲刺在空中同样生效（`CanPerformNormalDash` 的空中分支即通过）。
    组件 `SeinAbilities+0x80 → SeinDashAttack` 由 `HasAbility` 经
    `EnsureRightPrefabsAreThereForAbilities()` 实例化；该方法只在**读档
    （Serialize 读）/ SetAbility（技能树购买、捡能力道具）/ 角色跨场景生成**
    时调用，**不是每帧**。所以只写标志位不会立刻建组件——实测某存档
    `Dash.HasAbility=1` 而组件为 0，游戏照样正常跑（`SetStateActive` 是带判空的
    静态扩展 `if (state != null)`，其余读组件处也多有判空）。
    结论：**授予后要触发一次才会实体化**——最方便的是进技能树买任意一个技能
    （SetAbility 结尾无条件调 EnsureRightPrefabs），其次读档/跨场景生成。

16. **"显示地图"与成就闸门（为什么它不影响成就）**
    地图全开用的是游戏自带的显示位 `AreaMapDebugNavigation.UndiscoveredMapVisible`
    （`AreaMapUI.Instance +0x50` 指向它，位在 `+0x20`）。它为 true 时，地图绘制处
    普遍是 `FaceIsDiscoveredOrVisited(id) || UndiscoveredMapVisible`、图标隐藏条件
    里也有 `&& !UndiscoveredMapVisible`、迷雾 `Fog.SetActive(!IsDiscovered && !flag)`
    ——所以地形/图标/迷雾一并解除，观感等同"插了所有地图石"。
    - **不写存档**：该 bool 挂在 UI 单例上，只在本次游戏进程内有效，重启还原。
    - **不影响成就**：成就唯一闸门是 `CheatsHandler.DebugWasEnabled`
      （`AwardAchievement` 开头 `if (!DebugWasEnabled && ...)`），而它只由游戏自己的
      调试模式 `CheatsHandler.DebugEnabled` 经 `Update()` 置位（且一旦为真就粘住）。
      我们只写地图显示位，**从不碰 DebugEnabled/DebugControlsEnabled**，所以
      `DebugWasEnabled` 保持 false，成就照常。注意这两个调试开关是**会进存档**的：
      若你自己在游戏里开过调试菜单，成就早就被封，与本功能无关。
    - 也**不把图标标记为已发现**，不影响完成度与"找齐秘密"（那个要真穿 45 面隐藏墙）。

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
    → 功能表有 `NewFeature`（小键盘）/ `NewCtrlFeature`（Ctrl）两种构造器；把用错
    构造器的功能放到别的档位，就会与同数字的功能同时触发（曾出现"无限二段跳"与
    "无限生命"、"无限能力点数"与"无限能量"）。一命保护是 `oneLife` 单例、不走
    构造器，它的档位判断在 `main.go` 的 `handleDigit` 里单独写。
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

20. **"100% 探索"只写到 64%，且把值写进了元数据区**
    → 把 `List._items` 当成元素首地址用（`items+i*4`）。实际 `_items` 指向**数组
    对象**，元素从 **`+0x10`** 开始 → 元素整体错位 4 个：①漏写后半段区域
    （7/11 = 64%，界面永远到不了 100）；②前 4 次循环把 `1.0` 写进了数组类
    vtable 区（`+0x14`）。
    → 修：元素基址用 `_items + 0x10`；并**加类名校验**（只写
    `RuntimeGameWorldArea`），这样即使基址再算错也只会跳过、不会写坏东西。
    → 注：被误写的是运行期元数据（mono vtable），**重启游戏即重建**，无持久影响。

21. **技能树打不开、能力点花不出去**
    → `AllowedToAccessSkillTree => Level.Current > 0 && IsSafeToCastSoulFlame == Safe`；
    新存档 `Current`（等级）为 0（只在 `LevelUp()` 里自增）→ 门槛不成立。
    → 修："无限能力点数"功能顺带把 `level+0x28` 从 0 修正为 1（只在它是 0 时动），
    并在状态里提示"等级 0→1（技能树需等级>0）"。
    → 另注：技能树只在**存档点范围内轻点**（0.3 秒内松开）才开；长按是建立链接。

22. **`GameTimer` / `GameController` / `DifficultyController` 死活定位不到
    （"重置时间""一命保护"卡住的原因）**
    → 这些对象在游戏**启动早期**就构造，实测落在 `0x2Axxxxxx / 0x2Fxxxxxx`
    段，**低于 `minObjAddr = 0x40000000`**；而早期版本的 `classOf` 与低区槽扫描
    都用 `isHeapPtr`（下界 `0x40000000`）做第一道过滤，于是候选对象在类名解析
    之前就被丢掉了（`probe singleton GameTimer` 返回 0 槽位，`probe bases` 里
    `diffc` 恒为 0）。
    → 修：新增更低下界 `minObjLowAddr = 0x10000000` 与 `isObjPtrLow`，
    `classOfMin(p, obj, min)` 允许下调下界；低区槽扫描对 `GameTimer` 用
    "`+0x1C/+0x20/+0x24` 三个 float 取值区间"做便宜预筛，`DifficultyController`
    用 "`+0x18/+0x1C` 两个 ∈[0,3] 的整数"做预筛，**且必须放在 `isHeapPtr`
    那道下界门之前**（曾经只挪了 GameTimer、把 diff 留在门后，于是 diff 依旧找不到）。
    → 定位手法（可复用）：`probe slotscan <类名> 0x063C0000 0x063E0000 0x10000`
    直接扫 mono 静态字段块。本次看到 `GameTimer.Instance` `0x063DBAB8 -> 0x2FCD36B8`、
    `DifficultyController.Instance` `0x063D3D20 -> 0x2FCD26A0`，以及同块的
    `GameController.Instance`（`0x063D3D80 -> 0x2AC03C00`，其 `+0x14` 正好指向
    同一个 GameTimer）。
    → 教训：**"找不到对象"优先怀疑地址范围/对齐过滤，而不是对象不存在**；
    一个类的静态块里往往同时住着好几个"定位不到"的单例；放宽过滤时要**成组检查**
    所有同带对象，别只改当前那个。
    → 版本注记: 以上是**终极版**的地址现象；原版没有 DifficultyController，
    GameTimer 也在低区堆内，对应问题与修法见 §8-27。

23. **按 Ctrl+Shift+小键盘 N，修改器"像退出了"（其实是被 Windows Terminal 吃掉）**
    → 新终端（Windows Terminal 1.24）`defaults.json` 默认把
    `ctrl+shift+1..9` 绑成 `Terminal.OpenNewTabProfile0..8`。**当修改器/终端窗口
    有焦点时**，这个组合键在到达本程序之前就被 WT 拿去做"新建标签页"了——
    屏幕切到新开的标签页，看起来就像修改器退出。实测：事件查看器无崩溃记录，
    且游戏里 `Difficulty` 仍是 OneLife（说明按键根本没送到修改器）。
    → 修（最终方案）：**放弃 Ctrl+Shift 档，全部收敛为两档**——普通功能用小键盘，
    特殊功能用 Ctrl+小键盘，一命保护排在 Ctrl 组最后（后因新增"获得三把钥匙"，
    一命保护顺延为 **Ctrl+小键盘 6**）。曾试过 Ctrl+Shift+小键盘 1 → 0 也不行
    （WT 对这两条都不放行），所以不再在 Ctrl+Shift 上纠缠。
    同理 `ctrl+alt+1..9` 也被 WT 绑成切换标签页，不可用。
    → 教训：**终端里的全局热键要避开终端宿主自身的默认键位**；换键后用
    "按一下看功能状态有没有变"来确认按键确实到达了程序。
    → 附带的坑：NumLock 关闭时小键盘1=END；而旧版本把 END 绑成"直接退出"
    （`os.Exit(0)`），会让人误以为程序崩了。现已**移除所有"按键直接退出"**：
    退出请用窗口关闭按钮。ESC 仅用于"返回版本选择"，不再是退出。

24. **纯静态类（`Keys` / `Sein.World.Events`）的字段怎么定位**
    → 现象：按类名找 `Keys` 直接失败（`probe findfield Keys ...` 报"未找到类"）——
    C# 的 `static class` 没有实例，且它是嵌套类，按"类名 + 实例 vtable"那套都不好使。
    → 修：改走**字段名描述符**：扫描元数据里的字段名字符串 → 找指向它的 4 字节槽
    `H`（描述符布局 `{name*, klass*, offset}`）→ 从 `H+4` 拿到 klass 并校验类名；
    再在 **vtable 带**找 `u32(V)==klass` 且 `u32(V+0x0C)` 指向"字节全 ≤1"小块的槽
    → `static_data = u32(V+0x0C)`。档带**随版本不同**: 终极版 `0x2A-0x2B / 0x50-0x53`，
    原版 `0x35B7` 段（代码里由 `ApplyProfile` 设置 `vtableBands`，见 §8-27）。
    → 旁证：先确认了 `Events` 当前读数为全 0，并与当前存档一致（`SaveSlotInfo`
    显示载入的是 `sunkenGlades`、进度 10 的初期档），才相信静态块找对了；
    另外 `SpiritTreeReached` 不在暂停界面六图标里，不能用它当"必然为 1"的对照。
    → 教训：**bool 静态字段没法靠"值"来定位**（不像对象指针能顺着引用找），
    必须走 klass→vtable→static_data 这条链；验证时要找一个"当前状态下确定的值"
    来交叉印证（或确认当前存档阶段与之自洽）。
    → 关于"三个元素恢复"标记：可以写，但**不做**——它们反推 `WorldProgression`，
    跳过副本会让世界状态与内容不一致（见 §7-14）；"三把钥匙"只是库存布尔，
    才适合做成一键功能。

25. **"不安全区域建链接"开着却建不了（大多数时候无效）**
    → 早期实现只在按住时把 `m_holdDownTime(+94)` 写 1.0。但 `HandleCharging()`
    与施放判定 `if (m_holdDownTime == 1f && IsOnGround && m_delayOnGround == 0)`
    在**同一帧内，且回退在前**：不安全区（含不稳定地面/禁区/敌人）走回退分支
    `m_holdDownTime -= deltaTime / HoldDownDuration`，把我们刚写的 1.0 每帧扣掉，
    判定 `== 1f` 命中不了 → 表现为"偶尔能建、多数不行"。
    → 修：按住期间把 `HoldDownDuration(+98)` 临时置 **+Inf**，回退量 delta/Inf = 0
    （float32 下 `1.0f - 0` 仍是 `1.0f`），1.0 得以保持到施放判定；松开还原原值。
    → **另一个坑**：施放判定不含冷却/安全性，若每帧都写 1.0，`CastSoulFlame()`
    会连续触发——它内部 `PerformSave()` + `m_numberOfSoulFlamesCast++`，
    等于**刷存档**（还会撞 50 次的成就）。故以 `m_isCasting` 的按下沿武装，
    观察到 `m_holdDownTime` 被游戏清 0 即认为本次已施放，松开后再武装，
    保证"一次按键一个链接"。
    → 仍需满足施放的硬条件：**站在地面上**（`IsOnGround`）。空中按住不会立刻建，
    落地后会自动施放。

26. **"离开运行状态"必须走统一收尾（维护铁律）**
    → 修改器是外部写内存的工具，**停掉程序不会自动撤销它写过的值**。曾经只有
    ESC 返回选择界面会 `DeactivateAll`，而"点窗口关闭按钮"什么都不做 →
    开着超级跳直接关窗，跳跃高度会一直保持放大。
    → 约定: 定义唯一收尾函数 `teardownSession(s)`（`main.go`）=
    `DeactivateAll（能复原的复原、一次性修改型不动）→ closeProc → SetProcess(nil)`；
    **新增任何"离开会话/退出"的代码路径都必须调用它**，不要各自写一半。
    当前覆盖:
    - ESC 返回版本选择 → `teardownSession`；
    - 关闭窗口/注销/关机 → `core.SetCloseHandler` 在 `CTRL_CLOSE_EVENT` 回调里
      `teardownSession`（Windows 给约 5 秒，够用）；
    - 主协程 panic 兜底 → `defer` 里 `teardownSession`；
    - `choose()` 建新会话前先收尾旧的（防御）；
    - HOME 不离开会话：未全开时 `ActivateAll`（补开未开启的，含一命保护），已全开时 `DeactivateAll`。
    → 不可捕获: 任务管理器"结束任务"/强杀，收不到控制台事件（只能靠重启游戏恢复）。
    → 配套加固: `DeactivateFeature` 在还原前用 `Runtime.HasProcess()` 确认进程仍
    绑定——F12 会临时 `SetProcess(nil)`，此时若去写内存会空指针 panic。

27. **原版（ori.exe）整套功能"定位全失败 / Keys 静态块找不到 / 计时器找不到"**
    → 原版是 Unity 5.0 的另一份 mono 运行时，**地址空间和终极版完全不同**：
    托管堆在**低地址**（实测玩家对象 `0x01A8E6C0`，而代码里的对象下界是
    终极版的 `0x40000000`）；vtable 在 `0x35B7xxxx`（代码只允许终极版的
    `0x2A/0x50` 段）；GameTimer 的遥测字段偏移也不同，导致预筛拒绝所有候选。
    → 修：这三处都改成按版本取值——`ApplyProfile` 设置
    `minObjAddr/minObjLowAddr/vtableBands`；`scanLowBandAux` 增加 `ver` 参数，
    原版跳过遥测预筛。
    → 教训: 凡是"地址范围 / 扫描带 / 预筛字段"这类**依赖目标运行时布局**的常量，
    都不能只按终极版写死；先跑 `probe diag -vanilla` + `probe keys -vanilla` +
    `probe snap -vanilla`（见 §9）确认链路，再逐功能 `probe feat N -vanilla`。
    → 附带事实: 原版低区约 160MB，辅助单例扫描需 15–25 秒，首次附加后
    "死亡数/探索度/计时器"类功能会先显示"定位中"，扫描完成后自动生效。

28. **原版修改器界面一直卡在"Sein 扫描中…（进入存档后可定位）"**
    → 主程序建会话时写成 `&ori.Runtime{}`，**`Prof` 是 nil**；而 `ApplyProfile`
    只在 `r.Prof != nil` 时被 `SetProcess` 调用 → 原版仍用终极版的对象下界
    `0x40000000` 扫描，原版玩家对象在 `0x01A8E6C0`，永远命中不了。
    （终极版会话正常，因为默认值恰好等于终极版下界——所以这个 bug 只在原版暴露。）
    → 修：新增 `ori.NewRuntime(prof)`，`main.go` 的 `newSession` 统一用它建
    Runtime，把"版本"绑进去；并用 `ori/resolver_profile_test.go` 锁住
    "版本 → 对象下界 / vtable 带"的绑定关系。
    → 诊断: `oritrainer.exe -selftest` 不进入 TUI，直接对每个在运行的目标版本
    跑一遍真实的"附加 + 定位"并打印结果（0=成功，退出码 1=有进程但定位失败，
    2=目标都没运行），排查"界面一直扫描中"最快。
    → 另注意: 改了 Go 源码后必须**重新 `go build -o oritrainer.exe .`**——
    `go build ./...` 只做编译检查、不会更新仓库根目录那个 exe。

## §9 诊断工具 `probe`（等价 CE 的核心能力）

全部命令都可加 `-vanilla` 切换到原版进程（`ori.exe`）。

另外修改器自身有非交互自检（不进入 TUI）：
`oritrainer.exe -selftest` → 对每个在运行的目标版本跑一遍真实的"附加 + 定位"并
打印结果（退出码 0=至少一个成功 / 1=有进程但定位失败 / 2=目标都没运行）。
排"界面一直扫描中"最快，见 §8-28。

常用命令：

| 命令 | 用途 |
|---|---|
| `diag [-wait]` | 跑真实解析器，打印定位结果与快照（**验证解析器最快的入口**） |
| `bases [-xml]` | 打印所有基址符号；`-xml` 直接输出 CE 表的 `<UserdefinedSymbols>` 块 |
| `feats` | 打印功能/键位表并**自动检测键位重复** |
| `feat <数字> [秒] [-ctrl] [-shift]` | 对活体进程运行**真实功能**并打印状态（`-ctrl`/`-shift` 对应档位） |
| `offs <类名> <字段名...>` | **权威字段偏移**（从 mono 元数据读，等价 CE mono dissect） |
| `snap` | **一次打印所有功能依赖字段**（生命/能量/等级/跳跃/能力/探索/钥匙/地图/计时器）——换版本核验偏移的首选 |
| `fieldoff <类名> <字段名>` | 按"字段名+声明类名"反查偏移（比 `offs` 更适合常见字段名，`offs` 对 `Max` 这类名会退化到很慢） |
| `heapfind <类名>` | 在堆区按类名枚举实例（确认某单例是否存在） |
| `singleton <类名> [minObj]` | 在低区找"指向该类实例的静态槽"（单例定位；可下调对象下界） |
| `slotscan <类名> <lo> <hi> [minObj]` | 在指定地址范围内找"指向该类实例的槽位"（**定位早期对象/静态块首选**，如 `slotscan GameTimer 0x063C0000 0x063E0000 0x10000`） |
| `timer` | 用修改器自身的解析器定位 `GameTimer` 并打印 `CurrentTime`/节流字段（验证"重置时间"） |
| `onelife [秒]` | 端到端验证"一命保护"：定位难度控制器 → 激活保护 → tick → 打印状态/字段 → 关闭并还原 `Difficulty`（`LowestDifficulty` 全程只读） |
| `keys` | 只读定位 `Keys` 类静态数据块并打印三把钥匙标记（验证"获得三把钥匙"） |
| `map` | 只读定位 `AreaMapDebugNavigation` 并打印 `UndiscoveredMapVisible`（验证"显示地图"） |
| `struct <地址> [字数]` | 转储对象字段并标注指针目标类名 |
| `read/write <地址> <类型> <值>` | 原始读写（类型支持 i8/i16/i32/u32/f32/f64） |
| `fields2 <类名>` | 遍历 klass 的字段数组直出（备用） |
| `findfield <类名> <字段名...>` | 另一条字段查找路径 |
| `live` / `findall <类名>` / `refs <地址>` / `allrefs <地址>` | 活体定位 / 枚举实例 / 查引用 |
| `corereg [地址...]` | 打印修改器实际枚举到的内存段并查询某地址是否被覆盖 |
| `racetest` | 复现"会话切换 + 引擎并发"时序，配合 `-race` 用 |
| `conin` | **确定性自测控制台输入通路**（注入合成按键再读回，无需人工按键） |
| `staticblock` / `diffobj` / `diffname` / `diffstrict` / `onescan` / `staticmap` / `staticref` | 各类定位探索工具，详见源码注释 |
| `instances <类名>` | 按类名枚举实例（终极版堆带判据） |

> ⚠ 版本适用性: 上面标注"堆区/静态块带"的**探索类**命令（`heapfind` /
> `singleton` / `slotscan` / `instances` / `staticblock` / `diff*` 等）的地址判据
> 是按**终极版**堆带写的，原版跑可能一无所获。原版的偏移/取值核查请用
> `snap` / `fieldoff` / `diag` / `keys` / `map` / `timer`（这些走 `Runtime`，
> 已版本自适应）。`onelife` 仅终极版有意义（原版没有 DifficultyController）。

自测样例（不需要游戏也能跑）：

```
probe conin     → 注入 VK 0x41 读回识别 + 空缓冲不阻塞
probe feats     → 键位表 + "无重复 ✓"
```

## §10 维护流程（改功能时的 checklist）

1. **先从源码确认机制**：ILSpy 打开 `Assembly-CSharp.dll`，找到对应类/方法，
   看清"游戏到底读/写哪个字段、有哪些前置条件"。
2. **用元数据确认偏移**：`probe offs <类> <字段>` 或 `probe fieldoff <类> <字段>`
   （**不要按源码顺序推算**，见 §4.2）；换版本/换 build 时先跑 `probe snap`。
   新偏移写进 `ori/offsets.go`（唯一维护点），并把来源/用途写进注释。
3. **实现执行器**：在 `features.go` 里按语义选执行器，注意
   - 取址一律走 `Runtime.Addrs()/SubAddr()`（加锁）；
   - 需要还原的用 `OnDeactivate` 并在 `DeactivateFeature` 的 switch 里登记；
   - 幂等/一次性动作用"开启期间维持、关闭不动"。
4. **构建**：`cd GoTrainer && go build ./... && go vet ./...`；要让修改器真正生效
   还要 `go build -o oritrainer.exe .`（`go build ./...` 不会更新仓库里的 exe）。
5. **键位/顺序自检**：`probe feats`（确认无重复、档位正确、界面顺序由简到繁）。
6. **实机验证**：`probe feat <数字> [秒] [-ctrl|-shift]`，看状态与目标内存值；
   **写入型功能要验证"改回去/还原"也确实生效**。定位类问题（一直扫描中）先用
   `oritrainer.exe -selftest` 确认"真实二进制 + 真实会话"能定位。
7. **同步文档**：
   - README：§5 偏移表、§6 功能表、§7 机制、§8 新踩的坑；
   - `ctables/ori_de.ct`：新增/修改对应记录（字段名与偏移与 `offsets.go` 一致）；
     换会话后表可用 `probe bases -xml` 一键更新基址符号；
   - 原版相关改动若未实测，**必须在 `ori_vanilla.ct` 里标注"未核验"**；
     实测后把表头状态改为"已核验"并写明核验日期与命令（2026-09 起原版已全量核验）。
8. **提交**：说明"现象→根因→修法"，便于回溯。

键位分配规则（**只有两档**）：普通功能用**小键盘**（终极版 1-9；原版 1-8，
因为没有"无限冲刺"）；特殊功能用 **Ctrl+小键盘**（终极版 1-6，一命保护排最后；
原版 1-5，没有一命保护）。每档内顺序编号、不留空位，按版本自动顺延
（`BuildFeatures(prof)`）。不再使用 Ctrl+Shift 档（原因见 §8-23）。

**界面显示顺序必须与键位复杂度一致**（从一般到特殊）：

```
小键盘（普通功能） → Ctrl+小键盘（特殊功能）
```

功能表（`features.go` 的 `BuildFeatures`）按这个顺序排列；一命保护这类"最特殊"的
功能由 `main.go` 的 `navList` **追加到列表末尾**——不要再把它插进中间
（曾经它被插在小键盘组和 Ctrl 组之间，与"由简到繁"的顺序不一致）。
`probe feats` 会自动检查这一点（输出"顺序检查: 档位由简到繁 ✓"）。

## §11 未解决 / 待办

**原版支持（2026-09 完成）**：地址空间/vtable 带按版本适配后，13 项功能在原版
实机逐项验证通过（`probe feat <数字> -vanilla`）。两版差异与核验结论见 §5、§8-27。

**已知限制**：
- 原版低区约 160MB，`scanLowBandAux` 一次扫描需 **15–25 秒**；附加后前 ~25 秒内
  "死亡数归零 / 100% 探索 / 重置时间"会先显示"定位中"，扫描完成后自动生效
  （功能 Tick 持续重试，无需重开）。
- `probe` 部分命令（`heapfind`/`singleton`）仍是围绕终极版写的，用于原版前先跑
  `probe diag -vanilla` 确认链路。

**已全部完成**（历史记录）：
- **重置时间**：`GameTimer.Instance` 静态槽定位，写 `+0x1C=0`，Ctrl+小键盘 4（§7-13）。
- **一命保护**：`DifficultyController.Instance` 静态槽定位（早期曾因下界过滤误判为
  "不存在"，见 §8-22），仅在 `LowestDifficulty==OneLife` 时把 `Difficulty` 顶成
  Easy，Ctrl+小键盘 5（§7-7）。

## §12 资料与来源

- **反编译**（用 ILSpy，无混淆）：本 README 的"源码依据"出自这两份
  `Assembly-CSharp.dll`——终极版 `<游戏目录>/OriDE_Data/Managed/`，原版
  `<游戏目录>/Ori_Data/Managed/`。机制描述以终极版为准，原版差异见 §8-27。
- **第三方 CE 表**（用于交叉验证，原始文件已按"只保留两张表"的要求删除，
  可在 git 历史 `2e91d8c`（初始提交）找回）：
  - `attach_3834.ct`：**原版**指针链（`"ori.exe"+00A36164` 死亡数、
    `"mono.dll"+001F42C4` 能力点/灵魂点）——已转录进 `ori_vanilla.ct`。
  - `attach_3943.ct`：能力标志读取脚本（偏移与我们核验的**两版共有的 9 项**基础
    能力完全一致）+ 生命/能量 AOB 脚本——要点已记录在 §5 / `ori_vanilla.ct`。
  - `attach_12228.ct`：CE v27 mono 表（`usemono()` + `define`），478 项，
    含 SeinCharacter/Abilities/Jump 全字段；本项目的原版核验已完成（§8-27），
    该表保留作交叉参考。
- **本项目的教训来源**：全部为本项目实测（见 §8）。

---

# 三、构建 / 目录 / 边界

## 构建

```bash
cd GoTrainer
go build .                       # 修改器本体（产物名取 go.mod 模块默认: oritrainer.exe）
go build -o probe.exe ./cmd/probe  # 诊断工具
go vet ./... && gofmt -l .        # 提交前检查
./oritrainer.exe -selftest       # 非交互定位自检（见 §8-28）
```

**发布**：在 GitHub 上 Publish release 会触发 `.github/workflows/release.yml`——
在 `windows-latest` 上跑 gofmt/vet/单元测试并构建 `oritrainer.exe`，然后把
**修改器本体**与**风灵月影修改器**（随仓库分发的第三方工具）一起作为该 Release
的资产上传（`gh release upload --clobber`，可重跑覆盖）。

## 目录

```
GoTrainer/
  main.go              TUI + 按键 + 会话协程
  core/memory.go       进程/内存/控制台输入
  ori/resolver.go      对象定位
  ori/features.go      功能与执行器
  ori/offsets.go       ★ 唯一地址维护点
  ori/diag.go          probe 的偏移核验/快照（snap、fieldoff）
  ori/resolver_profile_test.go  版本↔地址空间绑定的回归测试
  cmd/probe/main.go    诊断工具（见 §9）
ctables/
  ori_de.ct            终极版字段对照表（与修改器同步）
  ori_vanilla.ct       原版字段对照表（2026-09 已核验，含第三方原版指针链）
```

## 已知边界

- 仅支持 32 位 mono 目标；地址策略与 WoW64 零扩展绑定。
- 需要管理员权限（`OpenProcess` 读写目标内存）。
- 原版字段偏移与功能已于 2026-09 实机核验（差异见 §8-27/§11）。
- 成就相关：若游戏内调试菜单被启用过（`CheatsHandler.DebugWasEnabled`），
  **任何修改器操作都不会影响成就**（因为游戏自己就不再发放）。
