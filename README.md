# OriTrainer (Go) — 奥日与迷失森林（原版）修改器

风灵月影风格的 Go TUI 修改器，目标**原版**《Ori and the Blind Forest》
（Steam appid 261570，最终构建 buildid 814852 / Build Number 5474）。

**纯标准库实现（零外部依赖），单 exe 发布。**

## 使用

1. 启动游戏，进入存档。
2. 运行 `GoTrainer.exe`（管理员权限），等待 TUI 显示
   `● 已附加` / `Sein 已定位` / 实时数值（约 15 秒全堆扫描）。
3. 数字键 1-5 开关功能（**全局热键**，无需切换窗口），HOME 关闭全部，
   F12 重新附加/重扫，F1 帮助，END 退出。

## 功能（全部已实机验证）

| 热键 | 功能 | 验证记录 |
|---|---|---|
| 1 | 无限生命 | 外部写 6.0 → 50ms 内写回 24，15s 稳定 |
| 2 | 无限能量 | 游戏内耗能后能量保持满值 |
| 3 | 技能点冻结 | 外部写 42 → 拉回 1 |
| 4 | 经验冻结 | 写 999999 → 游戏升级结算写入 989999 时被持续拉回 |
| 5 | 死亡数冻结 | 写 777 → 500ms 内写回真实值 |

冻结语义 = 激活瞬间捕获当前值并每 50ms 写回（风灵月影同款）。

## 地址方案（重要：为什么不用 CE 表）

2015 年社区 CE 表（ubiByte/Dix Dark）的 AOB 特征码与指针链
**均不适配本构建**（实机验证失败）。本项目的地址通过 CE 7.6 mono dissect
按类名考古获得，并验证了跨重启稳定性：

- 类静态槽在 mono 确定性分配下跨重启稳定，但该地址位于 MonoDataCollector
  注入层的地址空间，**外部读取时为 MEM_FREE，不可直接固化**——CE 表的
  `mono.dll+XXXX` 写法对外部训练器是死路。
- 可靠方案是**全堆扫描 + 结构签名**（见 `ori/resolver.go`）：
  - `SeinLevel`：回指签名 `u32(X+0x20)=P && u32(P+0x38)==X`，
    加 Energy.Max ∈ [1,50]、Health.MaxHealth ∈ [12,400] 双重验证挑出活体
    （排除传送门克隆与 UI 副本）；
  - `SeinDeathCounter`：稳定魔数 `u32(X+8)==0xFFFF18A6`；
  - 两阶段批处理扫描（本地快筛 + 候选 RPM 验证），约 14 秒遍历全堆。

字段偏移（SeinCharacter/SeinLevel/SeinEnergy/SeinMortality/SeinHealthController）
集中在 `ori/offsets.go`，数值已与游戏画面逐一核对
（死亡 333、GameTime 10939s≈存档 05:11、SP=1、Exp=1187、能量 0.5/5.0）。

## 已知边界

- 游戏失焦/暂停时 GameTime 停走，堆扫描仍可用（扫描不依赖游戏激活）。
- Mono 老版 Boehm GC 不移动对象，实例地址在存续期内稳定；场景切换可能重建对象，
  训练器每 2 秒校验失败自动重扫。
- 64 位 Go 进程读写 32 位目标正常，但**区域枚举必须接受 MEM_MAPPED 段**
  （mono 堆是 mapped section 而非 private，保护属性含执行位），见 `core/memory.go`。

## 构建

```bash
cd GoTrainer && go build -o GoTrainer.exe .
```

## 目录

```
GoTrainer/
  main.go            TUI + 热键轮询 + 附加看护 + 功能引擎
  core/memory.go     进程读写 / 区域枚举 / 指针链（32 位指针）
  ori/offsets.go     ★ 地址常量（唯一维护点）
  ori/resolver.go    堆扫描定位活体对象
  ori/features.go    冻结型功能
ctables/             CE 表与 mono 探针存档（研究记录）
OriTrainer/          旧 C#/WinForms 版本（已弃用，留作参考）
```
