using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;

namespace OriTrainerDLL
{
    // Hook 基础设施：把游戏方法的入口改写为跳转到我们 DLL 里的方法。
    //
    // ---- 原理 ----
    // 1. mono_compile_method 强制把游戏方法的 IL 编译出 native 代码，返回其入口地址。
    //    游戏方法在游戏进程中已加载，DLL 也在游戏进程内，直接 P/Invoke mono.dll 即可
    //    （无需像注入那样走远程线程）。
    // 2. 目标入口写 x86 跳转 E9 rel32（5 字节），跳到 replacement 方法的 native 入口。
    //    replacement 的地址同样用 mono_compile_method 拿（replacement 也是 mono 运行时
    //    加载的程序集里的方法，统一走同一 API，避免 RuntimeMethodHandle.GetFunctionPointer
    //    在旧 mono 上的不确定行为）。
    // 3. jmp 不改栈：目标方法被调用时，this + 参数原样出现在 replacement 的栈上，
    //    所以 replacement 的签名必须与目标"兼容"（实例方法第一个参数就是 this）。
    // 4. Unhook 还原目标入口的原始 5 字节。
    //
    // ---- 为什么写代码前要 VirtualProtect ----
    // mono JIT 生成的代码页是 RX（可读可执行，不可写），直接 Marshal.Copy 写入会抛
    // AccessViolation（net35 上可被 catch，功能静默失败）。必须先改成 RWX 写完再恢复。
    //
    // ---- 风险 ----
    // hook 的目标方法若被游戏重新 JIT，改写可能失效——本基础设施只保证"改写当时的
    // 状态"，若游戏在 hook 后重新编译该方法（极少见），需要重新 hook。
    //
    // ---- 线程安全 ----
    // Start()/Stop() 跑在命令线程（注入时已 mono_thread_attach），hook 操作在 attach 线程
    // 上做。改写目标入口的 5 字节与游戏主线程执行该方法存在极小 race 窗口，x86 上
    // E9 rel32 写入不是原子的，但窗口极短，可接受（风灵月影同类做法）。
    //
    // ---- 诊断 ----
    // 每步关键结果写 %TEMP%\OriTrainerDLL_hook.log，真机排查用。
    public static class Hooks
    {
        // ---- mono.dll 导出（DLL 在游戏进程内，直接 P/Invoke）----
        [DllImport("mono.dll", EntryPoint = "mono_compile_method", CallingConvention = CallingConvention.Cdecl)]
        private static extern IntPtr MonoCompileMethod(IntPtr method);

        // ---- kernel32：改写代码页权限 ----
        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool VirtualProtect(IntPtr lpAddress, UIntPtr dwSize, uint flNewProtect, out uint lpflOldProtect);

        private const uint PAGE_EXECUTE_READWRITE = 0x40;

        // ---- x86 机器码 ----
        // E9 rel32：无条件相对跳转，5 字节。目标 = 当前指令下一条 + rel32。
        private const byte JMP_REL32 = 0xE9;

        // hook 记录表：目标方法 → 原始入口字节（Unhook 时还原）
        private static readonly Dictionary<MethodInfo, byte[]> _hooks =
            new Dictionary<MethodInfo, byte[]>();

        private static readonly object _lock = new object();

        private static string LogPath
        {
            get { return Path.Combine(Path.GetTempPath(), "OriTrainerDLL_hook.log"); }
        }

        private static void Log(string msg)
        {
            try
            {
                File.AppendAllText(LogPath,
                    string.Format("[{0:HH:mm:ss.fff}] {1}{2}", DateTime.Now, msg, Environment.NewLine));
            }
            catch { }
        }

        // 目标/替换方法的 JIT 入口（mono_compile_method 强编译，返回 native 地址）
        private static IntPtr Compile(MethodInfo method)
        {
            IntPtr entry = MonoCompileMethod(method.MethodHandle.Value);
            if (entry == IntPtr.Zero)
                throw new Exception("mono_compile_method 返回空: " + method);
            return entry;
        }

        // 把目标方法入口改写为 jmp 到 replacement。
        // 目标：目标方法被调用时，直接执行 replacement（this + 参数原样传递）。
        public static void Replace(MethodInfo target, MethodInfo replacement)
        {
            if (target == null) throw new ArgumentNullException("target");
            if (replacement == null) throw new ArgumentNullException("replacement");

            lock (_lock)
            {
                if (_hooks.ContainsKey(target))
                    throw new InvalidOperationException("目标方法已被 hook: " + target);

                try
                {
                    IntPtr targetEntry = Compile(target);
                    IntPtr replacementEntry = Compile(replacement);
                    Log("target  " + target + "  entry=0x" + targetEntry.ToInt64().ToString("X"));
                    Log("repl    " + replacement + "  entry=0x" + replacementEntry.ToInt64().ToString("X"));

                    // 目标入口下一条指令地址 = 目标入口 + 5（E9 rel32 长度）
                    long next = targetEntry.ToInt64() + 5;
                    long delta = replacementEntry.ToInt64() - next;
                    if (delta < int.MinValue || delta > int.MaxValue)
                        throw new Exception("跳转距离超出 int32，无法用 E9 编码");

                    // 代码页可能 RX，先改成 RWX 才能写
                    if (!VirtualProtect(targetEntry, (UIntPtr)5, PAGE_EXECUTE_READWRITE, out uint oldProtect))
                        throw new Exception("VirtualProtect(RWX) 失败: " + Marshal.GetLastWin32Error());

                    // 保存原始 5 字节（Unhook 还原用）
                    byte[] original = new byte[5];
                    Marshal.Copy(targetEntry, original, 0, 5);

                    // 写 E9 rel32
                    byte[] patch = new byte[5];
                    patch[0] = JMP_REL32;
                    uint rel = unchecked((uint)(int)delta);
                    patch[1] = (byte)(rel & 0xFF);
                    patch[2] = (byte)((rel >> 8) & 0xFF);
                    patch[3] = (byte)((rel >> 16) & 0xFF);
                    patch[4] = (byte)((rel >> 24) & 0xFF);

                    Marshal.Copy(patch, 0, targetEntry, 5);

                    // 恢复原保护
                    VirtualProtect(targetEntry, (UIntPtr)5, oldProtect, out uint restored);

                    _hooks[target] = original;
                    Log("hook OK: " + target);
                }
                catch (Exception ex)
                {
                    Log("hook FAIL: " + ex.GetType().Name + ": " + ex.Message);
                    throw;
                }
            }
        }

        // 还原目标方法的原始字节。
        public static void Unhook(MethodInfo target)
        {
            if (target == null) throw new ArgumentNullException("target");

            lock (_lock)
            {
                if (!_hooks.TryGetValue(target, out byte[] original))
                    return; // 没 hook 过，幂等

                try
                {
                    IntPtr entry = Compile(target);

                    if (!VirtualProtect(entry, (UIntPtr)original.Length, PAGE_EXECUTE_READWRITE, out uint oldProtect))
                        throw new Exception("Unhook VirtualProtect(RWX) 失败: " + Marshal.GetLastWin32Error());

                    Marshal.Copy(original, 0, entry, original.Length);

                    VirtualProtect(entry, (UIntPtr)original.Length, oldProtect, out uint restored);

                    _hooks.Remove(target);
                    Log("unhook OK: " + target);
                }
                catch (Exception ex)
                {
                    Log("unhook FAIL: " + ex.GetType().Name + ": " + ex.Message);
                    throw;
                }
            }
        }

        // 是否已 hook
        public static bool IsHooked(MethodInfo target)
        {
            lock (_lock)
            {
                return target != null && _hooks.ContainsKey(target);
            }
        }
    }
}
