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

本项目针对上游第三方代码做了一些功能裁剪与缺陷修复。

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

**3. `Memory.Dispose()` 用 `MEM_DECOMMIT` 释放 `VirtualAllocEx` 分配的内存**
（`Memory.cs`）

正确应为 `MEM_RELEASE`（`0x8000`）。上游用 `MEM_DECOMMIT` 只释放物理页，
region 依然挂在该进程的地址空间上（泄漏）。已实测确认：本项目每次
`VirtualAllocEx(MEM_COMMIT)` 都独立新开一个 64KB region，因此改为
`VirtualFreeEx(_handle, key, 0, MEM_RELEASE)` 可精确释放整个 region
（`MEM_RELEASE` 要求 dwSize 为 0，正好不能用上游传的 kvp.Value）。

**4. `ProcessUtils.GetModuleInformation()` 的 `cbSize` 参数传错**
（`ProcessUtils.cs`）

上游传 `(uint)(size * ptrs.Length)`，即 4 × 模块数；x86 下恰好等于前面
`EnumProcessModulesEx` 算出的 `bytesNeeded`，纯属数值巧合才没出错。
已改为 MSDN 规定的 `(uint)Marshal.SizeOf<MODULEINFO>()`（x86 下 12，
x64 下 24，两个平台都正确）。

**5. `Assembler.Push()` 的立即数编码区间错位**
（`Assembler.cs`）

上游用 `(< 128)` 选操作码、用 `(<= 255)` 选写入长度，两个界不一致，
参数落在 128..255 时会写成 `0x68`（push imm32）操作码 + 1 字节操作数，
指令错位。已改为与操作码同界：

  | 参数值 | 操作码 | 指令需要 | 实际写入 | 结果 |
  |---|---|---|---|---|
  | `-128..127` | `0x6A`（push imm8） | 1 字节 | 1 字节 | 正确 |
  | 其余（含 128..255、负数） | `0x68`（push imm32） | 4 字节 | 4 字节 | 正确 |

顺带把负立即数从「一律走 imm32」修正为「-128..127 走 imm8」——原实现里
`(byte)arg` 会把 -1 变成 0xFF，依旧错位。

**6. `Injector.ReadMonoString()` 在 64 位下少解引用一层**
（`Injector.cs`）

`MonoString` 布局：header + length(int) + chars。64 位下 `chars` 是
`char*` 字段（8 字节指针），必须先读指针再解引用才能拿到字符串数据；
32 位下 `chars` 是内联数组，数据就地存放。上游 64 位路径少了一次
解引用，读出来是乱码/越界。本项目游戏为 32 位、走 32 位路径，未触发，
但修复后 64 位目标也正确了：

```csharp
IntPtr chars = Is64Bit
    ? (IntPtr)_memory.ReadLong(monoString + 0x14)
    : monoString + 0xC;
return _memory.ReadUnicodeString(chars, len * 2);
```
