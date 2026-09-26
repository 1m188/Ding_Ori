// 耐久：反复重启假游戏，观察修改器句柄数是否稳定（不泄漏）。
//
// 每次重启都会走一遍 Drop → 重连，是句柄泄漏最容易暴露的地方。
// 这条套件当初抓出过 Injector 构造函数在抛异常时泄漏 _handle
// （调用方拿不到对象 -> Dispose 永不执行），所以值得长期留着。
//
// 跑的是【真实构建出来的 OriTrainer.exe】，不是把主循环编进来 —— 句柄
// 只在真实进程里才有意义。

using System;
using System.Diagnostics;
using System.IO;
using System.Threading;

namespace OriTrainerTests
{
    internal static class SoakSuite
    {
        private const int Rounds = 40;

        private static string _logPath;

        private static int Handles(Process p)
        {
            p.Refresh();
            return p.HandleCount;
        }

        public static void Run()
        {
            Test.Section("耐久：重启假游戏 " + Rounds + " 次");

            string exe = Test.TrainerExe;
            Test.Check("真实 exe 已构建", File.Exists(exe), exe);
            if (!File.Exists(exe)) return;

            _logPath = Path.Combine(Test.Dir, "soak_" + Process.GetCurrentProcess().Id + ".log");

            Test.KillFakes();
            Thread.Sleep(300);
            if (File.Exists(_logPath)) File.Delete(_logPath);

            Process fake = Test.StartFake(_logPath, 0);
            Thread.Sleep(500);

            // UseShellExecute=true：它需要自己的控制台，否则 Terminal.Enter 会抛
            ProcessStartInfo tpsi = new ProcessStartInfo(exe);
            tpsi.UseShellExecute = true;
            Process trainer = Process.Start(tpsi);

            bool ok = Test.WaitFor(() => !trainer.HasExited && Test.ReadAll(_logPath).Contains("CONNECTED"), 15000);
            Console.WriteLine("  首次连接: " + (ok ? "OK" : "FAILED"));
            Thread.Sleep(1000);
            GC.Collect(); Thread.Sleep(500);

            int start = Handles(trainer);
            Console.WriteLine("  起始句柄数: " + start);

            int reconnects = 0;
            for (int i = 0; i < Rounds; i++)
            {
                Test.Stop(fake);
                Thread.Sleep(350);

                fake = Test.StartFake(_logPath, 0);
                string before = Test.ReadAll(_logPath);
                if (Test.WaitFor(() =>
                    {
                        string now = Test.ReadAll(_logPath);
                        return now != before && now.Contains("CONNECTED");
                    }, 8000)) reconnects++;

                if (trainer.HasExited)
                {
                    Console.WriteLine("  !! 修改器在第 " + i + " 轮退出");
                    break;
                }
            }

            Test.KillFakes();
            Thread.Sleep(1500);
            GC.Collect(); Thread.Sleep(500);

            // 必须在 Dispose 之前读：Disposed 的 Process 一访问就抛
            bool alive = !trainer.HasExited;
            int end = alive ? Handles(trainer) : -1;

            Console.WriteLine("  重连成功次数: " + reconnects + "/" + Rounds);
            Console.WriteLine("  结束句柄数: " + end);
            Console.WriteLine("  句柄增长: " + (end < 0 ? "n/a" : (end - start).ToString()) +
                              "  (每次重启平均 " + (end < 0 ? "n/a" : ((double)(end - start) / Rounds).ToString("0.00")) + ")");
            Console.WriteLine("  修改器存活: " + alive);

            if (alive) { try { trainer.Kill(); trainer.WaitForExit(3000); } catch { } }
            trainer.Dispose();

            Test.Check("全部 " + Rounds + " 次重启都重连成功", reconnects == Rounds,
                "只有 " + reconnects + "/" + Rounds);
            Test.Check("修改器全程存活", alive, "exited");
            // 阈值 5：句柄数的抖动来自线程池与 CLR 自身，不是每次重启恰好 +0；
            // 真泄漏在 40 轮下会明显超过这个数（当初那次是 +40）。
            Test.Check("句柄无泄漏（增长 <= 5）", end >= 0 && (end - start) <= 5,
                "start=" + start + " end=" + end);
        }
    }
}
