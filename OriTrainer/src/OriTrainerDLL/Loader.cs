using System;
using System.Diagnostics;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using OriTrainerShared;

namespace OriTrainerDLL
{
    public static class Loader
    {
        // 功能类命名空间：两版统一为 OriTrainerDLL.Features。
        // 版本区分在 csproj 按 Edition 选择编译 Features 或 Features-DE 目录，
        // 实际构建只编进一个目录，命名空间无需区分。
        private const string FeatureNamespace = "OriTrainerDLL.Features";

        private static IntPtr _pipe; // 管道句柄

        // exe 侧 Injector 的入口：必须 public static 且无参。
        // 准备工作全部在此同步做完，一返回管道就已存在，exe 可直接连接、无需重试。
        public static void Load()
        {
            // 创建命令管道
            _pipe = Native.CreateNamedPipeW(@"\\.\pipe\" + Constants.PipeName(Process.GetCurrentProcess().Id),
                Native.PIPE_ACCESS_DUPLEX,
                Native.PIPE_TYPE_BYTE | Native.PIPE_READMODE_BYTE | Native.PIPE_WAIT,
                Native.PIPE_UNLIMITED_INSTANCES, 4096, 4096, 0, IntPtr.Zero);

            if (_pipe == new IntPtr(-1))
                throw new Exception("创建命名管道失败：" + Marshal.GetLastWin32Error());

            // 只有收发循环放后台线程（ConnectNamedPipe 会阻塞到客户端连上）。
            new Thread(Serve) { IsBackground = true }.Start();
        }

        private static void Serve()
        {
            while (true)
            {
                // 这个线程一旦退出，管道句柄还在、进程也还在，但再也没人
                // ConnectNamedPipe —— exe 侧此后永远连不上，也永远不会重连
                // （进程扫描发现不了），唯一出路是重启游戏。所以必须兜住。
                //
                // 不需要重建管道：现实里唯一会抛的是 Shutdown() 里的
                // Assembly.GetTypes()，此时管道好得很。就算异常发生在连接之后，
                // 下一轮 ConnectNamedPipe 会立刻返回 ERROR_PIPE_CONNECTED
                // （客户端还连着），流程自然接回 ReadCommands。
                try
                {
                    Native.ConnectNamedPipe(_pipe, IntPtr.Zero);
                    Shutdown(); // 刚连上：清掉上一次会话可能遗留的状态
                    ReadCommands();
                    // exe 断开后复用同一句柄等待下一次连接，不关闭重建。
                    Native.DisconnectNamedPipe(_pipe);
                    Shutdown(); // 刚断开：等价于"修改器已关闭"，停掉一切持续写入
                }
                catch (Exception ex)
                {
                    Log("Serve", ex);
                    Thread.Sleep(100); // 防呆：万一走到了这里，不至于空转烧 CPU
                }
            }
        }

        // 停止全部功能。注入无法卸载（见 vendor/SharpMonoInjector/README.md），
        // "关闭修改器"在 DLL 侧唯一能做的就是停止持续写内存并注销主线程钩子。
        //
        // 命名空间即功能清单：不维护注册表，新增功能类自动被覆盖（约定见 Dispatch）。
        //
        // 注意这只是"停止"，不是"撤销"：按各功能的设计，一些在功能设计上不方便或者语义上不好撤销的东西不会撤销
        public static void Shutdown()
        {
            foreach (Type type in typeof(Loader).Assembly.GetTypes())
            {
                if (type.Namespace != FeatureNamespace) continue;

                MethodInfo stop = type.GetMethod("Stop",
                    BindingFlags.Public | BindingFlags.Static, null, Type.EmptyTypes, null);
                if (stop == null) continue;

                // 和 Dispatch 一样：单个功能出错不能中断其余功能，更不能让异常
                // 从后台线程冒出去终止进程（那就是游戏）。
                try
                {
                    stop.Invoke(null, null);
                }
                catch (Exception ex)
                {
                    Log(type.Name + ".Stop", ex);
                }
            }
        }

        private static void ReadCommands()
        {
            byte[] one = new byte[1];
            StringBuilder line = new StringBuilder();

            // 一个字节一个字节的读取
            while (Native.ReadFile(_pipe, one, 1, out uint read, IntPtr.Zero) && read == 1)
            {
                if (one[0] == Constants.Terminator) // 遇到终止符
                {
                    Dispatch(line.ToString()); // 分发命令
                    line.Length = 0;
                }
                else
                {
                    line.Append((char)one[0]);
                }
            }
        }

        // 约定：命令是 "<功能名> <Start|Stop>"，对应 Features 命名空间下同名静态类的同名静态方法。
        private static void Dispatch(string command)
        {
            string[] parts = command.Split(' ');
            if (parts.Length != 2) return;

            Type type = typeof(Loader).Assembly.GetType(FeatureNamespace + "." + parts[0], false);
            if (type == null) return;

            MethodInfo method = type.GetMethod(parts[1],
                BindingFlags.Public | BindingFlags.Static, null, Type.EmptyTypes, null);
            if (method == null) return;

            // 后台线程上的未捕获异常会终止整个进程，即游戏。功能的 bug 不能杀掉游戏。
            try
            {
                method.Invoke(null, null);
            }
            catch (Exception ex)
            {
                Log(type.Name + "." + method.Name, ex);
            }
        }

        // 记一次执行失败。写日志本身也不能抛：调用方在后台线程上，再抛出去整个游戏就没了。
        private static void Log(string subject, Exception ex)
        {
            // 反射调用会把功能里抛的异常包一层，真实原因在 InnerException。
            // 解包属于反射调用错误处理（DLL 侧业务），留在 Loader；Logs 只负责写。
            if (ex is TargetInvocationException && ex.InnerException != null)
                ex = ex.InnerException;

            // Logs.Log 内部自行吞异常，这里无需再包一层 try/catch。
            Logs.Log(string.Format("{0}() 执行失败：{1}: {2}{3}{4}",
                subject, ex.GetType().FullName, ex.Message,
                Environment.NewLine, ex.StackTrace));
        }
    }
}
