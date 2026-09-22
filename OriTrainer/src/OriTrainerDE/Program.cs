using System;
using System.Diagnostics;
using System.IO.Pipes;
using System.Text;
using OriTrainerShared;

namespace OriTrainerDE
{
    internal static class Program
    {
        // 游戏进程名称
        private static readonly string processName = "oriDE";

        private static void Main(string[] args)
        {
            Process[] games = Process.GetProcessesByName(processName);
            if (games.Length == 0) throw new Exception("没有找到游戏进程 " + processName);

            using (NamedPipeClientStream pipe = PipeClient.Attach(games[0].Id))
            {
                byte[] command = Encoding.UTF8.GetBytes(args[0] + Constants.Terminator);
                pipe.Write(command, 0, command.Length);
                pipe.Flush();
            }
            Console.WriteLine("已发送：{0}", args[0]);
        }
    }
}
