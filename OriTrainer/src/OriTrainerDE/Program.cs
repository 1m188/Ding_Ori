using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Reflection;
using System.Text;
using OriTrainerShared;
using SharpMonoInjector;

namespace OriTrainerDE
{
    internal static class Program
    {
        // 游戏进程名称
        private static readonly string processName = "oriDE";

        private static void Main(string[] args)
        {
            // 载荷由 csproj 内嵌进本 exe，资源名见 OriTrainerDE.csproj 的 LogicalName
            byte[] payload;
            using (Stream s = Assembly.GetExecutingAssembly()
                       .GetManifestResourceStream("OriTrainerDEDLL.dll"))
            {
                payload = new byte[s.Length];
                s.Read(payload, 0, payload.Length); // 读取载荷字节
            }

            Console.WriteLine("载荷 {0} 字节", payload.Length);

            Process[] games = Process.GetProcessesByName(processName);
            if (games.Length == 0) throw new Exception("没有找到游戏进程 " + processName);
            Process game = games[0];

            using (Injector injector = new Injector(game.Id))
            {
                injector.Inject(payload, "OriTrainerDEDLL", "Loader", "Load");
            }
            Console.WriteLine("注入成功");

            // 注入返回即代表 Load() 已跑完、管道已建好，直接连即可
            using (NamedPipeClientStream pipe =
                   new NamedPipeClientStream(".", Constants.PipeName(game.Id), PipeDirection.Out))
            {
                pipe.Connect(3000);
                byte[] command = Encoding.UTF8.GetBytes(args[0] + Constants.Terminator);
                pipe.Write(command, 0, command.Length);
                pipe.Flush();
            }
            Console.WriteLine("已发送：{0}", args[0]);
        }
    }
}
