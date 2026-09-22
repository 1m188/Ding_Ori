/*
    管道连接管理
*/

using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Reflection;
using OriTrainerShared;
using SharpMonoInjector;

namespace OriTrainerDE
{
    internal static class PipeClient
    {
        private const string payloadName = "OriTrainerDEDLL"; // 要注入的载荷名称

        // 取得与游戏内载荷相连的管道：先试连接，连不上说明载荷不在，注入后再连。
        // 注入返回即代表 Load() 已跑完、管道已建好，所以直接连即可、无需重试。
        public static NamedPipeClientStream Attach(int pid)
        {
            NamedPipeClientStream pipe = Connect(pid, 1000);
            if (pipe != null) return pipe; // 载荷已在运行，跳过注入

            // 未检测到载荷，开始注入...
            using (Injector injector = new Injector(pid))
            {
                // 载荷由 csproj 内嵌进本 exe，资源名见 OriTrainerDE.csproj 的 LogicalName
                injector.Inject(ReadPayload(payloadName + ".dll"), payloadName, "Loader", "Load");
            }
            // 注入成功

            return Connect(pid, 3000)
                ?? throw new Exception("注入后仍连不上管道 " + Constants.PipeName(pid));
        }

        // 连接失败时返回 null，不抛异常——"没连上"是正常分支，不是错误
        private static NamedPipeClientStream Connect(int pid, int timeout)
        {
            NamedPipeClientStream pipe =
                new NamedPipeClientStream(".", Constants.PipeName(pid), PipeDirection.Out);
            try
            {
                pipe.Connect(timeout);
                return pipe;
            }
            catch
            {
                pipe.Dispose(); // 连接失败也持有句柄，不释放会泄漏
                return null;
            }
        }

        private static byte[] ReadPayload(string logicalName)
        {
            // 获取要注入的载荷的数据
            using (Stream s = Assembly.GetExecutingAssembly().GetManifestResourceStream(logicalName))
            {
                byte[] payload = new byte[s.Length];
                s.Read(payload, 0, payload.Length);
                return payload;
            }
        }
    }
}
