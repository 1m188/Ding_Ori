# vendor/SharpMonoInjector

第三方源码，**只取注入部分**，以「源码链接」方式编入 `src\OriTrainer`（exe）。

- 上游：https://github.com/warbler/SharpMonoInjector
- 提交：`73566c1be1e8e1bb25ab60683557958368b2cd47`（2019-03-23）
- 许可：MIT（见 `LICENSE`，版权归 Biney 2017）

## 为什么是源码而不是 ProjectReference

1. 它**不在 NuGet 上**（`dotnet package search SharpMonoInjector` 无结果），无法 `PackageReference`。
2. 上游目标是 `netstandard2.0`。作为独立程序集会让发布目录多出一个 dll；
   直接编进 exe 则单文件即可运行。

## 只编入 exe，绝不编入 DLL

载荷（`OriTrainerDLL`，net35）**不能**引用这里的代码：

```
net48 → netstandard2.0   编译通过
net35 → netstandard2.0   error CS0012: 类型"Object"在未引用的程序集中定义
                          （要求 netstandard.dll，而游戏 Managed 目录里没有）
```

即使能编译，注入后的 Mono 也无法解析外部依赖 —— 载荷必须自包含。

## 与上游的差异

**1. 已移除 `Injector.Eject()` 及其 `CloseAssembly()` 辅助方法。**

上游 `Eject()` 调用 `mono_assembly_close()`。实测证明这条路径会破坏游戏进程：

- `mono_assembly_close()` 只递减引用计数，**不释放** JIT 代码，也不释放托管静态字段；
- 但它会在 `domain->domain_assemblies` 里留下**悬空指针**；
- 之后每次加载程序集，`mono_domain_assembly_search()` 都会遍历该链表。

后果是间歇性的堆损坏崩溃（`STATUS_HEAP_CORRUPTION`，`0xC0000374`），
且往往在 eject 之后一段时间才发作。本项目在实验阶段复现过 3 次。

因此**注入是单向的**：一个游戏进程只注入一次，功能复位靠命名管道断开事件，
不通过卸载任何东西来实现。相关常量 `mono_assembly_close` 也从导出表中一并删除。

**2. 两个构造函数补了失败时的句柄释放（`Injector.cs`）。**

上游在构造函数里 `OpenProcess` 之后直接做后续检查，**中途抛异常时会漏掉
`CloseHandle`**：调用方拿不到对象，`Dispose()` 自然不会执行。

上游一次性注入的用法下这无所谓（失败就退出），但本项目的 exe 会**每 200ms 重试
注入**直到游戏的 `mono.dll` 就绪，于是每次失败泄漏一个句柄、无上限累积
（实测：一个正在启动的游戏，40 次尝试泄漏 40 个句柄）。

改法是把构造函数的后续检查包进 `try/catch`，失败时 `CloseHandle` 后再 rethrow。
改完实测 40 次尝试句柄增长为 0。

## 未做的修改

上游还有一些已知瑕疵，本项目**暂未改动**，因为当前代码路径不触发：

- `Memory.Dispose()` 用 `MEM_DECOMMIT` 释放 `VirtualAllocEx` 分配的内存，
  正确应为 `MEM_RELEASE`（`0x8000`）。会导致内存泄漏，但不致崩溃。

- `ProcessUtils.GetModuleInformation()` 的 `cbSize` 参数传错了（`ProcessUtils.cs`）：

  ```csharp
  Native.GetModuleInformation(handle, ptrs[i], out MODULEINFO info, (uint)(size * ptrs.Length))
  ```

  `cbSize` 按 MSDN 应为 `sizeof(MODULEINFO)`，x86 下是 **12**。这里传的是
  `size * ptrs.Length`（4 × 模块数），x86 下恰好等于前面 `EnumProcessModulesEx`
  算出的 `bytesNeeded`，所以**实测能跑通**（psapi 未严格校验该值），
  纯属数值上的巧合，并非有意为之。

  潜在影响：模块数极少时该值会小于 12，此时 psapi 可能拒绝写入或截断结构体；
  但 `mono.dll` 所在进程必然已加载数十个模块，实际不会落到那个区间。

- `Assembler.Push()` 的立即数编码有三个区间，中间那个是错的：

  | 参数值 | 操作码 | 指令需要 | 实际写入 | 结果 |
  |---|---|---|---|---|
  | `0..127` | `0x6A`（push imm8） | 1 字节 | 1 字节 | 正确 |
  | `128..255` | `0x68`（push imm32） | **4 字节** | **1 字节** | **错位** |
  | `256+` | `0x68`（push imm32） | 4 字节 | 4 字节 | 正确 |

  即操作码已按 `>= 128` 切换成 imm32，但写入长度仍按 `<= 255` 只写 1 字节。
  仅当参数恰好落在 `128..255` 才会出错，目前传入的都是指针，不落在该区间内。

改动这些会偏离上游，等实际用到时再处理并单独记录。
