# vendor/SharpMonoInjector

第三方源码，**只取注入部分**，以「源码链接」方式编入 `src\OriTrainerDE`（exe）。

- 上游：https://github.com/warbler/SharpMonoInjector
- 提交：`73566c1be1e8e1bb25ab60683557958368b2cd47`（2019-03-23）
- 许可：MIT（见 `LICENSE`，版权归 Biney 2017）

## 为什么是源码而不是 ProjectReference

1. 它**不在 NuGet 上**（`dotnet package search SharpMonoInjector` 无结果），无法 `PackageReference`。
2. 上游目标是 `netstandard2.0`。作为独立程序集会让发布目录多出一个 dll；
   直接编进 exe 则单文件即可运行。

## 只编入 exe，绝不编入 DLL

载荷（`OriTrainerDEDLL`，net35）**不能**引用这里的代码：

```
net48 → netstandard2.0   编译通过
net35 → netstandard2.0   error CS0012: 类型"Object"在未引用的程序集中定义
                          （要求 netstandard.dll，而游戏 Managed 目录里没有）
```

即使能编译，注入后的 Mono 也无法解析外部依赖 —— 载荷必须自包含。

## 与上游的差异

**已移除 `Injector.Eject()` 及其 `CloseAssembly()` 辅助方法。**

上游 `Eject()` 调用 `mono_assembly_close()`。实测证明这条路径会破坏游戏进程：

- `mono_assembly_close()` 只递减引用计数，**不释放** JIT 代码，也不释放托管静态字段；
- 但它会在 `domain->domain_assemblies` 里留下**悬空指针**；
- 之后每次加载程序集，`mono_domain_assembly_search()` 都会遍历该链表。

后果是间歇性的堆损坏崩溃（`STATUS_HEAP_CORRUPTION`，`0xC0000374`），
且往往在 eject 之后一段时间才发作。本项目在实验阶段复现过 3 次。

因此**注入是单向的**：一个游戏进程只注入一次，功能复位靠命名管道断开事件，
不通过卸载任何东西来实现。相关常量 `mono_assembly_close` 也从导出表中一并删除。

## 未做的修改

上游还有两处已知瑕疵，本项目**暂未改动**，因为当前代码路径不触发：

- `Memory.Dispose()` 用 `MEM_DECOMMIT` 释放 `VirtualAllocEx` 分配的内存，
  正确应为 `MEM_RELEASE`（`0x8000`）。会导致内存泄漏，但不致崩溃。
- `Assembler.Push()` 在参数值 `128..255` 时用 `0x6A`（push imm8）却写入
  `BitConverter.GetBytes(int)` 的 4 字节，指令流会错位。
  仅当参数恰好落在该区间才会出错，目前传入的都是指针，不落在区间内。

改动这些会偏离上游，等实际用到时再处理并单独记录。
