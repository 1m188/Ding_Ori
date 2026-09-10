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

| 键 | 作用 |
|---|---|
| 数字键 1-5 | 开关功能 |
| HOME | 关闭全部 |
| F12 | 重新附加/重扫 |
| F1 | 帮助 |
| ESC | 返回版本选择（修改器界面）/ 退出（选择界面） |
| END | 退出 |

## 功能（两版同名同键位，均已实机验证）

| 热键 | 功能 | 原版验证记录 | 终极版验证记录 |
|---|---|---|---|
| 1 | 无限生命 | 写 6.0 → 拉回 24 | 同签名路径（CE 权威值 HP=16/16 吻合） |
| 2 | 无限能量 | 游戏内耗能保持满值 | CE 权威值 1.0/2.0 吻合 |
| 3 | 技能点冻结 | 写 42 → 拉回 1 | 写 42 → 1 秒内拉回 13 ✓ |
| 4 | 经验冻结 | 升级结算写入被持续拉回 | 同结构（Exp=405 吻合） |
| 5 | 死亡数冻结 | 写 777 → 500ms 拉回 | 同签名路径（deaths=98 吻合） |

冻结语义 = 激活瞬间捕获当前值并每 50ms 写回（风灵月影同款）。

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
