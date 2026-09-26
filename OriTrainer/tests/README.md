# tests —— 测试套件

## 怎么跑

```powershell
cd OriTrainer
dotnet build OriTrainer.sln -c Release

cd tests\OriTrainerTests
bin\Release\net48\win-x86\OriTrainerTests.exe          # 全量
bin\Release\net48\win-x86\OriTrainerTests.exe conn     # 只跑一套
bin\Release\net48\win-x86\OriTrainerTests.exe list     # 列出全部套件
```

退出码：`0` = 全过，`1` = 有失败，`2` = 套件名不认识（防止打错名字时假装全绿）。
当前全量共 **64 项断言**。

跑之前请先关掉真游戏：套件按进程名 `oriDE` 扫描（与修改器一致的判定方式），
残留的真游戏进程会被当成被测对端，断言会乱。

## 三个项目

| 项目 | 作用 |
|---|---|
| `OriTrainerTests` | 测试主体，按参数分派套件 |
| `FakeGame` | 假游戏，**产物名固定为 `oriDE.exe`** |
| `Grab` | 截图工具，读目标控制台的内容并以 UTF-8 落盘 |

### 为什么测试项目把产品源码「编进来」而不是引用 exe

`PipeClient` / `Status` / `Keyboard` / `UI` 全是 `internal`，
`ProjectReference` 到 `OriTrainerDE.exe` 拿不到它们（除非给产品加
`InternalsVisibleTo`，那是为测试改动产品代码）。
把源码文件直接 `Compile` 进来，`internal` 在同一程序集内天然可见，**产品侧零改动**。

代价是 `Program.cs` 不能一起编（它有自己的 `Main`）。这不是问题：
E2E 本来就该跑**真实构建出来的 `OriTrainerDE.exe`**，而不是把主循环编进来假装是它。

### 假游戏为什么要单独一个 exe、还必须叫 oriDE.exe

`PipeClient` 是按进程名 `oriDE` 找游戏的，并且由**进程主模块所在目录**推算日志路径。
所以构建后会把假游戏复制到两个位置：

| 位置 | 用途 |
|---|---|
| `<测试输出>\oriDE.exe` | 仅管道模式（测连接与命令流） |
| `<测试输出>\FakeGame\oriDE.exe` | 日志模式，日志落在同级 `FakeGame\OriDE_Data\output_log.txt`（测就绪门） |

## 套件

| 名称 | 断言 | 覆盖 |
|---|---:|---|
| `conn` | 20 | 连接状态迁移、命令流与顺序、自动重连、断开期间不落地 |
| `gate` | 6 | 就绪门：残留日志不被误判、门放行前一次都不注入 |
| `hold` | 3 | 日志被持续持有时门仍工作（含"旧写法必失败"的对照） |
| `injectonce` | 6 | 管道存在（哪怕被占）时绝不重复注入 |
| `soak` | 4 | 重启假游戏 40 次，句柄不泄漏 |
| `e2e` | 25 | 真实 exe + 真控制台 + SendInput 热键 + 界面复位 |

### 关于断言计数

`gate` 的 6 条是 6 个**互不重复**的判据。早先它有 7 条，其中 3 条其实在测同一个条件
（`_seenInjectingBeforeReady == 0`），只是措辞不同 —— 那种写法会让覆盖率看起来比
实际高，所以改成了现在这样：每条断言对应一个独立事实。

写测试时请注意同样的陷阱：**多个断言不能共用同一个条件**。若某个条件确实重要到
要从不同角度描述，那是注释的事，不要复制成多条 `Test.Check`。

### 并发约定

`Tick()` 会写 `_logPath` / `_gameStartUtc` / `_injectedPid` 与 `Status`，而
`RuntimeReady()` 读的正是前两个。所以 `gate` / `hold` 两个套件**全程由单个线程调用
`Tick()`**，不启动 `PipeClient` 的巡检线程；采样线程只读 `Status.Connection`。
细节与原因写在 `GateSuite.cs` 的文件头，改那两个套件前请先读它。

### 几条专门钉住历史故障的断言

这些不是泛泛的覆盖率，每一条都对应一个真实踩过的坑，**改坏了会红**：

- **`gate`** —— 在托管运行时装载完之前注入会把游戏打崩（`0xC0000005`）。
  这条门靠"日志写入时间晚于进程启动"+"日志含就绪标记"两个条件判断，
  残留日志（内容含标记但时间是上次运行的）必须被挡住。
- **`hold`** —— 读日志时必须显式用 `FileShare.ReadWrite`。
  `File.ReadAllText` 内部是 `FileShare.Read`，而游戏全程持有写句柄，
  共享检查又是双向的，于是必然抛异常、被 catch 吞掉、界面永远"未连接"。
  套件里那条"旧写法必失败"的对照就是锁这个的。
- **`soak`** —— 句柄泄漏（`Injector` 构造函数抛异常时漏 `CloseHandle`）。
  40 轮重启后句柄增长必须 ≤ 5。
- **`injectonce`** —— 重复注入会让游戏里有两个 `Serve` 各自调 `Shutdown()`，
  表现为"按了没反应"。

## 已知边界

- 假游戏不加载 `mono.dll`，所以**注入必然失败**。这正好用来验证"注入失败不放弃、
  下一轮继续"，但也意味着真实的注入成功路径只能靠 `e2e` + 手动在真游戏上验证。
- `e2e` 会用 `SendInput` 发全局热键，运行期间不要操作键盘。
- E2E/Soak 跑的是 `src\OriTrainerDE\bin\Release\net48\win-x86\OriTrainerDE.exe`，
  所以改动产品代码后要先 `dotnet build`，否则测的还是旧产物。
