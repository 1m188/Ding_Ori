# OriTrainer (Go) — 奥日与迷失森林 双版本修改器

风灵月影风格的 Go TUI 修改器，一个程序支持两个版本（启动时选择入口）：

- **原版**《Ori and the Blind Forest》（appid 261570，buildid 814852）
- **终极版**《Ori and the Blind Forest: Definitive Edition》（appid 387290，buildid 1096284）

**纯标准库实现（零外部依赖），单 exe 发布。**

## 使用

1. 启动 `GoTrainer.exe`（管理员权限），在版本选择界面用 ↑↓/WS 选择、回车进入。
2. 启动对应游戏并进入存档；修改器自动附加并全堆扫描（约 15 秒），
   TUI 显示 `● 已附加` / `Sein 已定位` / 实时数值后即可使用。
3. ESC 可随时返回版本选择切换另一版。

### 热键（全局，免切窗）

每个功能只有**一个**快捷键。分配原则：优先填满小键盘 1-9/0（10 个），
超出部分用 Ctrl+小键盘（1 起）。原版与终极版各自独立（两者不会同时运行）。

| 键 | 作用 |
|---|---|
| 小键盘 1-9/0 | 前 10 项功能 |
| Ctrl+小键盘 1-6 | 后 6 项功能（含一命保护、经验倍率）|
| HOME | 关闭全部 |
| F12 | 重新附加/重扫 |
| F1 | 帮助 |
| ESC | 返回版本选择（修改器界面）/ 退出（选择界面） |
| END | 退出 |

> 没有小键盘的键盘：切到修改器窗口后用 **↑↓ 选择、回车/空格切换**（详见下文"前台导航"）。

## 功能（键位对齐风灵月影《终极版》v1.0 Plus 13）

**全局热键只用小键盘**（避免与笔记本键盘功能键/主键盘冲突）；没有小键盘的键盘
可切到修改器窗口用 ↑↓ 选择、回车/空格切换（见下方"前台导航"）。

| 热键 | 功能 | 状态 |
|---|---|---|
| 小键盘 1 | 无限生命 | ✓ 实测 |
| 小键盘 2 | 无限能量 | ✓ 实测 |
| 小键盘 3 | 灵魂链接无需冷却 | ✓ 实测（冷却 5.00→0.00） |
| **小键盘 4** | **可在不安全区域建立灵魂链接** | ✓ 实测（见下节） |
| 小键盘 5 | 超级跳 | ✓ 实测（3.00→7.50，可还原） |
| 小键盘 6 | 超级跳冲量 | ✓ 实测 |
| 小键盘 7 | 无限二段跳 | ✓ 实测（2→99） |
| 小键盘 8 | 二段跳强化 | ✓ 实测 |
| 小键盘 9 | 无限经验 | ✓ 实测（0→999999） |
| 小键盘 0 | 无限能力点数 | ✓ 实测 |
| Ctrl+小键盘 1 | 一命保护（死亡如普通模式） | ✓ 见下节 |
| Ctrl+小键盘 2 | 死亡数归零 | ✓ 实测 |
| Ctrl+小键盘 3 / 4 / 5 / 6 | 经验倍率 2x / 4x / 8x / 16x | ✓ 实测（单选，自动关闭其它倍率） |
| HOME | 关闭全部 | ✓ |
| F12 | 重新附加/重扫 | ✓ |
| F1 | 帮助 | ✓ |
| ESC / END | 返回版本选择 / 退出 | ✓ |

### 前台导航（无小键盘时使用）

把焦点切到修改器窗口后：

- **↑ / ↓**（或 W / S）：移动光标选择功能项
- **回车 / 空格**：激活或取消当前选中的功能
- 光标以 `▶` 高亮显示，底部提示会切换为"前台模式"

两条触发路径互不干扰：窗口在前台时导航生效，切回游戏后全局小键盘热键依然可用。
（前台判定见 `core/memory.go` 的 `WindowIsForeground`，覆盖 conhost、进程父链、
Windows Terminal 宿主三种场景。）

冻结语义：
- 功能 1-2、超级跳系列 = 捕获激活瞬间的值并锁定/放大（关闭时还原原值）；
- **死亡数归零 = 固定目标值模式**：激活后无条件写 0，适合"无死亡通关"成就
  （游戏判定 `SeinDeathCounter.Count == 0`，见 `AchievementsLogic.OnAct3End`）。

### 小键盘 4：可在不安全区域建立灵魂链接

原版游戏只允许在"安全区域"建立灵魂链接（存档点），不安全时按链接键会被拒绝并显示提示。

**实现原理**（DE v1.0 反编译源码，`SeinSoulFlame`）：

```csharp
// HandleCharging(): 蓄力只在"安全区域"判定通过时累加
if (m_isCasting && ... && IsSafeToCastSoulFlame == Safe && ...)
    m_holdDownTime += Time.deltaTime / HoldDownDuration;
else
    m_holdDownTime -= ...;                    // 不安全时回退

// UpdateCharacterState(): 施放判定【不含任何安全检查】
if (m_holdDownTime == 1f && IsOnGround && m_delayOnGround == 0f)
    CastSoulFlame();
```

关键发现：`IsSafeToCastSoulFlame` 有 7 项判定（禁制区域／黑暗/存档台/附近敌人/
重生点/无敌状态/地面射线检测），但它**只用于控制蓄力是否累加**；真正的施放入口
`CastSoulFlame()` 不检查任何安全性。

因此本功能的做法：**当玩家按住链接键（且在地面、无落地延迟）时，直接把蓄力
`m_holdDownTime` 写满 1.0f** —— 游戏下一帧便执行 `CastSoulFlame()`，
在不安全区域成功建立链接。纯数据写入，不需要代码补丁，也不修改安全判定本身
（关闭功能后游戏行为完全恢复原样）。

**对照实验验证**：
- 功能关闭 + 模拟按住键 → 蓄力保持 **0.000**（游戏因安全判定回退，符合源码）；
- 功能开启 + 模拟按住键 → 蓄力被写满 **1.000**，游戏执行施放。

**使用提示**：先把链接技能（Rekindle）学会，然后在任意位置按住链接键即可；
若同时开启小键盘 3（无需冷却），可连续建立链接。

### Ctrl+小键盘 1：一命保护（终极版专用）

让"一命通关"难度下的死亡行为与普通模式一致——**在上个检查点复活，存档不被销毁**，
同时**保留一命通关成就（Unhinged / BeatOneLife）的获取资格**。

原理（基于 DE v1.0 反编译源码）：

| 源码位置 | 逻辑 |
|---|---|
| `SeinDamageReciever.OnKill` | `if (Difficulty == OneLife) { WasKilled=true; 存盘; 删光所有备份 }` |
| `SeinDamageReciever.OnKillRoutine` | `if (Difficulty == OneLife) 弹 GameOver` else `淡出 → RestoreCheckpoint()` 复活 |
| `AchievementsLogic.OnAct3End` | `switch (LowestDifficulty) { case OneLife: 授予成就 }` |

一命清档与 GameOver 都只认 `Difficulty`，而成就只认 `LowestDifficulty`。
本功能因此**每秒 20 次把 `Difficulty` 锁为 Normal(1)，绝不触碰 `LowestDifficulty`(保持 OneLife=3)**。

**验证状态**：
- ✓ 锁定生效（3→1，持续稳定，游戏未回写）；✓ `LowestDifficulty` 始终保持 3；
- ✓ 纠正能力：手动把 `Difficulty` 写回 OneLife 后，**300ms 内被自动纠正回 Normal**；
- ⚠ 未做端到端实测：真实死亡后的画面表现、存档写盘重载后 `LowestDifficulty` 的值
  （需正常游玩到有伤害区域/检查点才能验证）。首次实战前建议先用新建的一命存档试跑。

**已知副作用**：存档列表的难度标签可能显示为"普通"（因 `SaveSlotInfo.Difficulty` 同步的是被锁值），
但成就判定读 `LowestDifficulty`，不受影响。

**使用建议**：一命通关全程开启即可；如担心意外，可配合游戏的"备份"功能手动存档。

## 地址方案（重要：为什么不用 CE 表 / FLiNG 提取）

按优先级实测过三条路线：

1. **FLiNG DE 修改器提取**（✗ 不可行）：exe 内无明文 `oriDE.exe+XXXX`
   符号引用（0 处），AOB 串均为二进制噪声，无法提取。
2. **网上现成 CE 表**（△ 价值低）：fearlessrevolution 的 DE 表
   （`ctables/de_frf_62130.ct`）仅含 1 个 AOB 石头数量脚本，无指针结构。
3. **CE mono dissect 按类名考古**（✓ 采用）：终极版与原版的类结构
   **字段偏移完全一致**（SeinLevel.m_sein/SkillPoints/Experience = 0x20/0x24/0x2C 等），
   运行期用**全堆两阶段扫描**定位活体对象，两版共用一套扫描代码：
   - `SeinLevel` 回指签名 `u32(X+0x20)=P && u32(P+0x38)==X`
     + Energy.Max∈[1,50] + Health.MaxHealth∈[12,400] 加固（排除克隆/副本）；
   - `SeinDeathCounter` 稳定魔数 `u32(X+8)==0xFFFF18A6`。

CE 的类静态槽地址（mono.dll+XXXX）位于 MonoDataCollector 注入层，
外部读取为 MEM_FREE，不可固化——这是 CE 表指针写法对外部训练器的死路。

**WoW64 关键坑**（两版通用）：DE 的 mono 大堆位于 64 位地址空间高位
（如 SeinLevel@0x800265C0），32 位视角的 VirtualQueryEx 看不到；
本程序为 64 位进程，用 64 位 VQEx 视图枚举（见 `core/memory.go` ReadableRegions）。

字段偏移与扫描签名集中在 `ori/offsets.go`（唯一维护点）。
两版数值验证记录：原版 死亡333/GameTime≈05:11/SP=1/Exp=1187；
终极版 SP=13/Exp=405/能量1.0/2.0/血16/16/死亡98。

## 构建

```bash
cd GoTrainer && go build -o GoTrainer.exe .
```

## 目录

```
GoTrainer/
  main.go            版本选择 + TUI + 热键轮询 + 会话协程
  core/memory.go     进程读写 / 64位区域枚举 / 指针链（32 位指针）
  ori/offsets.go     ★ 地址常量（唯一维护点，两版通用）
  ori/resolver.go    堆扫描定位活体对象（两版共用）
  ori/features.go    冻结型功能
ctables/             CE 表与 mono 探针存档（研究记录，含 DE 考古）
```

## 已知边界

- 游戏失焦/暂停时堆扫描仍可用；场景切换可能重建对象，2 秒校验失败自动重扫。
- 终极版打过 3DM 汉化补丁的构建已验证兼容（本机实测）。
- 原版修改器的 FLiNG DE exe 已从本项目移除（原计划捆绑启动，被原生实现取代）；
  C#/WinForms 旧版已删除（git 历史可查）。
