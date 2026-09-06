# OriTrainer — 奥日与迷失森林修改器

风灵月影风格的 C# WinForms 单文件修改器，同时支持两个版本：

- **原版**《Ori and the Blind Forest》（appid 261570）—— 自研内存修改
- **终极版**《Ori and the Blind Forest: Definitive Edition》（appid 387290）—— 内嵌 FLiNG 官方修改器，窗口嵌入程序内

## 使用方法

1. 双击 `OriTrainer/publish/OriTrainer.exe`（需要管理员权限）。
2. **原版页**：先启动游戏，程序自动附加 `ori.exe`（底部状态栏变绿）。
   用数字键或勾选框切换功能；功能在进入存档后才实际生效。
3. **终极版页**：点击"启动并嵌入终极版修改器"，FLiNG 修改器会释放到
   `%TEMP%\OriTrainer\` 并嵌入本程序窗口内；关闭本程序时自动结束它并清理。

### 原版热键表

| 热键 | 功能 | 实现方式 | 状态 |
|------|------|----------|------|
| 1 | 无限生命 | AOB 代码补丁 ×2（跳过伤害结算 + 跳过死亡分支） | 待验证 |
| 2 | 无限能量 | AOB 代码补丁 ×2（两个扣能点，匹配哪个补哪个） | 待验证 |
| 3 | 无限技能点 | AOB 代码补丁（跳过扣减存储） | 待验证 |
| 4 | 死亡数冻结 | 指针链（激活瞬间捕获当前值） | 待验证 |
| 5 | 技能点数冻结 | 指针链 | 待验证 |
| 6 | 精神点(经验)冻结 | 指针链 | 待验证 |
| HOME | 全部关闭 | — | — |

大键盘和小键盘的数字键均可；热键是全局的（风灵月影同款），不吞键。
全部功能来自 fearlessrevolution.com 社区 CE 表转写（本地副本见 `ctables/`），
**尚未经过实机验证**，实测结果见下文"实测排错"。

## 地址维护（改条目 → 重编译）

所有原版地址集中在 **`OriTrainer/Ori/OriOffsets.cs`**（指针链）和
**`OriTrainer/Ori/OriFeatures.cs`**（AOB 特征码），这是全程序唯一的地址维护点，
没有任何外部配置文件。

```bash
dotnet publish -c Release -o OriTrainer/publish OriTrainer/OriTrainer.csproj
```

产物 `publish/OriTrainer.exe`（约 1.2MB，FLiNG exe 已内嵌），拷走即可用。

### 指针链不通时

先把 `OriOffsets.cs` 里的 `ReverseCeOffsets` 常量改为 `false`（CE 表偏移顺序的两种
解读互换，一条链都通了就不用动）。仍不通则需用 CE 重新验证该条链。

### AOB 补丁找不到时

状态栏会显示"特征扫描中…"。Mono 的 JIT 代码只有对应方法被执行过才存在——
进入游戏触发相关动作（受击、耗能、用技能点）后再激活。若始终找不到，说明游戏
构建与 CE 表版本有差异，需要用 CE 重新抓特征码并更新 `OriFeatures.cs`。

## 已知边界 / v2 计划

- 终极版修改器（FLiNG）目标 v1.0，本机终极版打过 3DM 汉化补丁，兼容性由使用者确认。
- 大表（kemenner mono 表）的 `pSeinCharacter` 字段族（跳跃高度/能力/憋气/伤害等）
  依赖 JIT 代码钩子捕获实例指针，外部改法无法直接转写，v2 可做：
  代码钩子注入捕获 / mono 静态结构解析 / VirtualAllocEx 落补丁码（灵魂+10）。
- 公开发布需注意：内嵌的 FLiNG 修改器版权归其作者所有，仅限自用。

## 工程

```
OriTrainer/
  OriTrainer.csproj      net48 / WinForms / x86 / requireAdministrator
  Core/                  内存读写、指针链、AOB 扫描、热键轮询、功能引擎
  Ori/                   ★ 地址常量与功能定义（唯一维护点）
  UI/                    主窗体 + 双 tab
  De/DeTrainerHost.cs    FLiNG exe 释放/启动/嵌入/清理
ctables/                 三张来源 CE 表（仅解析用，不参与发布）
```
