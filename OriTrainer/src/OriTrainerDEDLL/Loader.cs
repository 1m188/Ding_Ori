using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using OriTrainerShared;

namespace OriTrainerDEDLL
{
    public static class Loader
    {
        private const string FeatureNamespace = "OriTrainerDEDLL.Features.";

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
                Native.ConnectNamedPipe(_pipe, IntPtr.Zero);
                ReadCommands();
                // exe 断开后复用同一句柄等待下一次连接，不关闭重建。
                Native.DisconnectNamedPipe(_pipe);
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

            Type type = typeof(Loader).Assembly.GetType(FeatureNamespace + parts[0], false);
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
                // 反射调用会把功能里抛的异常包一层，真实原因在 InnerException
                if (ex is TargetInvocationException && ex.InnerException != null)
                    ex = ex.InnerException;

                // 写日志本身也不能抛：这里是后台线程最后的兜底，再抛出去整个游戏就没了
                try
                {
                    File.AppendAllText(Path.Combine(Path.GetTempPath(), "OriTrainerDEDLL_error.log"),
                        string.Format("[{0:yyyy-MM-dd HH:mm:ss.fff}] {1}.{2}() 执行失败：{3}: {4}{5}{6}{7}{8}",
                            DateTime.Now, type.Name, method.Name, ex.GetType().FullName, ex.Message,
                            Environment.NewLine, ex.StackTrace, Environment.NewLine, Environment.NewLine));
                }
                catch { }
            }
        }
    }
}
